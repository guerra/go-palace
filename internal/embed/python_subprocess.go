package embed

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// MempalaceDirEnv is the environment variable that points at the Python
// reference implementation checkout. It is consulted by
// NewPythonSubprocessEmbedder when PythonSubprocessOptions.MempalaceDir is
// empty. There is intentionally no hardcoded fallback — shipping a
// developer-specific absolute path in source is a portability and
// information-disclosure hazard.
const MempalaceDirEnv = "MEMPALACE_PY_DIR"

// ErrMempalaceDirUnset is returned when neither PythonSubprocessOptions.MempalaceDir
// nor the MEMPALACE_PY_DIR env var was supplied. Callers should treat this
// as a signal to fall back to FakeEmbedder.
var ErrMempalaceDirUnset = errors.New("embed: MEMPALACE_PY_DIR unset and no MempalaceDir override provided")

// ErrClosed indicates the embedder's subprocess has been shut down.
var ErrClosed = errors.New("embed: python subprocess closed")

// ErrProbeFailed indicates the subprocess started but failed its readiness
// handshake (e.g. Python exception, wrong script, missing chromadb).
var ErrProbeFailed = errors.New("embed: python subprocess probe failed")

// PythonSubprocessOptions controls how NewPythonSubprocessEmbedder spawns the
// helper process. Zero values fall back to production defaults. Tests should
// set PythonArgs to swap in a fake script.
type PythonSubprocessOptions struct {
	// UVBinary is the path to the uv launcher. Defaults to "uv".
	UVBinary string
	// MempalaceDir is the working directory of the Python oracle.
	// Defaults to DefaultMempalaceDir.
	MempalaceDir string
	// PythonArgs, when non-empty, fully overrides the default command —
	// the first element is the executable and the rest are its args.
	// Intended for unit tests driving testdata/fake_embedder.py.
	PythonArgs []string
	// ProbeTimeout bounds the initial readiness handshake. Defaults to 60s.
	ProbeTimeout time.Duration
}

// pythonScript is the real production helper. It imports Chroma's bundled
// ONNXMiniLM_L6_V2 (NOT sentence-transformers — that package is not in
// mempalace's pyproject.toml) and emits one readiness JSON line followed by
// one JSON response per input line.
const pythonScript = `
import json, sys
from chromadb.utils.embedding_functions import ONNXMiniLM_L6_V2
ef = ONNXMiniLM_L6_V2()
sys.stdout.write(json.dumps({"ready": True, "dim": 384}) + "\n")
sys.stdout.flush()
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    texts = json.loads(line)
    vecs = ef(texts)
    sys.stdout.write(json.dumps([[float(x) for x in v] for v in vecs]) + "\n")
    sys.stdout.flush()
`

// PythonSubprocessEmbedder is an Embedder that delegates to a long-lived
// Python helper process over newline-delimited JSON. It is safe for use from
// multiple goroutines: all pipe access is serialized with a mutex.
type PythonSubprocessEmbedder struct {
	dim    int
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer

	mu     sync.Mutex
	closed bool
}

// NewPythonSubprocessEmbedder spawns the helper and runs the readiness probe.
// On any failure it kills the subprocess and returns a non-nil error — the
// caller is expected to fall back to FakeEmbedder with a loud log line.
func NewPythonSubprocessEmbedder(opts PythonSubprocessOptions) (*PythonSubprocessEmbedder, error) {
	if opts.ProbeTimeout == 0 {
		opts.ProbeTimeout = 60 * time.Second
	}

	cmd, err := buildCmd(opts)
	if err != nil {
		return nil, err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("embed: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("embed: stdout pipe: %w", err)
	}
	// Bounded stderr buffer so a noisy subprocess does not flood memory.
	stderr := &bytes.Buffer{}
	cmd.Stderr = boundedWriter{buf: stderr, max: 4 * 1024}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("embed: start python subprocess: %w", err)
	}

	reader := bufio.NewReaderSize(stdout, 1<<20) // 1 MiB line buffer
	dim, probeErr := probeReady(reader, opts.ProbeTimeout)
	if probeErr != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("%w: %v (stderr: %s)",
			ErrProbeFailed, probeErr, stderr.String())
	}

	return &PythonSubprocessEmbedder{
		dim:    dim,
		cmd:    cmd,
		stdin:  stdin,
		stdout: reader,
		stderr: stderr,
	}, nil
}

func buildCmd(opts PythonSubprocessOptions) (*exec.Cmd, error) {
	if len(opts.PythonArgs) > 0 {
		return exec.Command(opts.PythonArgs[0], opts.PythonArgs[1:]...), nil //nolint:gosec
	}
	uv := opts.UVBinary
	if uv == "" {
		uv = "uv"
	}
	dir := opts.MempalaceDir
	if dir == "" {
		dir = os.Getenv(MempalaceDirEnv)
	}
	if dir == "" {
		return nil, ErrMempalaceDirUnset
	}
	return exec.Command(uv, "run", "--directory", dir, "python", "-c", pythonScript), nil //nolint:gosec
}

// readyMessage is the one-shot JSON handshake emitted by the helper.
type readyMessage struct {
	Ready bool `json:"ready"`
	Dim   int  `json:"dim"`
}

// probeReady drains stdout looking for a parseable ready handshake. Non-JSON
// lines (e.g. tqdm progress bars printed to stdout on first model download)
// are logged at debug and skipped. The read loop runs on a side goroutine so
// we can enforce the ProbeTimeout with a select.
func probeReady(r *bufio.Reader, timeout time.Duration) (int, error) {
	type result struct {
		dim int
		err error
	}
	ch := make(chan result, 1)

	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{err: fmt.Errorf("read probe: %w", err)}
				return
			}
			trimmed := bytes.TrimSpace([]byte(line))
			if len(trimmed) == 0 {
				continue
			}
			var msg readyMessage
			if jerr := json.Unmarshal(trimmed, &msg); jerr != nil {
				slog.Debug("embed: skipping non-json probe line",
					"line", string(trimmed))
				continue
			}
			if !msg.Ready || msg.Dim <= 0 {
				ch <- result{err: fmt.Errorf("bad handshake: %s", trimmed)}
				return
			}
			ch <- result{dim: msg.Dim}
			return
		}
	}()

	select {
	case res := <-ch:
		return res.dim, res.err
	case <-time.After(timeout):
		return 0, fmt.Errorf("probe timeout after %s", timeout)
	}
}

// Dimension returns the vector dimension advertised by the helper at probe
// time. For ONNXMiniLM_L6_V2 this is 384.
func (e *PythonSubprocessEmbedder) Dimension() int { return e.dim }

// Embed sends one batch to the helper and returns the decoded vectors.
// Calls are serialized across goroutines via an internal mutex.
func (e *PythonSubprocessEmbedder) Embed(texts []string) ([][]float32, error) {
	if texts == nil {
		return nil, fmt.Errorf("embed: nil input")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrClosed
	}

	payload, err := json.Marshal(texts)
	if err != nil {
		return nil, fmt.Errorf("embed: marshal texts: %w", err)
	}
	if _, err := e.stdin.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("embed: write stdin: %w", err)
	}

	line, err := e.stdout.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("embed: read stdout: %w (stderr: %s)",
			err, e.stderr.String())
	}

	var raw [][]float32
	if err := json.Unmarshal(bytes.TrimSpace([]byte(line)), &raw); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(raw) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d vectors, got %d",
			len(texts), len(raw))
	}
	return raw, nil
}

// Close shuts the helper down. It closes stdin so the Python loop exits, then
// waits up to 5 seconds before force-killing. Safe to call more than once.
//
// Close returns an error only for unexpected termination (signal, non-zero
// exit other than the benign EOF-exit, or wait failures). A clean exit (code
// 0) after stdin close is the expected path.
func (e *PythonSubprocessEmbedder) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	_ = e.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- e.cmd.Wait() }()
	select {
	case err := <-done:
		return e.classifyWaitErr(err)
	case <-time.After(5 * time.Second):
		_ = e.cmd.Process.Kill()
		<-done
		// Force-kill after a hung shutdown is an incident worth surfacing.
		return fmt.Errorf("embed: python subprocess did not exit within 5s, killed (stderr: %s)",
			e.stderr.String())
	}
}

// classifyWaitErr returns nil for benign shutdown exits (code 0 from clean
// stdin-EOF) and a wrapped error otherwise, including the bounded stderr
// tail so the caller can see why Python died.
func (e *PythonSubprocessEmbedder) classifyWaitErr(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// The helper's `for line in sys.stdin:` loop exits with code 0 on
		// EOF. Any non-zero code or kill-by-signal is unexpected and gets
		// reported so zombie/crashed subprocesses surface instead of being
		// silently masked by Close.
		return fmt.Errorf("embed: python subprocess exited with %s (stderr: %s)",
			exitErr.String(), e.stderr.String())
	}
	return fmt.Errorf("embed: python subprocess wait: %w (stderr: %s)",
		err, e.stderr.String())
}

// boundedWriter captures at most `max` bytes into an in-memory buffer,
// dropping subsequent writes. Used for subprocess stderr.
type boundedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w boundedWriter) Write(p []byte) (int, error) {
	remaining := w.max - w.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
	} else {
		w.buf.Write(p)
	}
	return len(p), nil
}
