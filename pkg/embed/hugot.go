package embed

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	hugot "github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

const (
	defaultModelName = "sentence-transformers/all-MiniLM-L6-v2"
	defaultModelDir  = ".mempalace/models"
	pipelineName     = "mempalace-embed"
)

// HugotOptions configures the HugotEmbedder.
type HugotOptions struct {
	// ModelName is the HuggingFace model identifier.
	// Defaults to "sentence-transformers/all-MiniLM-L6-v2".
	ModelName string
	// ModelDir is the parent directory where models are stored.
	// Defaults to ~/.mempalace/models/.
	ModelDir string
	// ModelPath overrides auto-download: use this local model directory directly.
	ModelPath string
}

// HugotEmbedder is a pure-Go embedder backed by knights-analytics/hugot.
// It uses the GoMLX backend (no CGO beyond sqlite-vec) and downloads the
// model on first use.
type HugotEmbedder struct {
	pipeline        *pipelines.FeatureExtractionPipeline
	session         *hugot.Session
	dim             int
	needsTruncation bool
}

// NewHugotEmbedder creates a HugotEmbedder. It initializes a GoSession,
// resolves/downloads the model, and creates a FeatureExtractionPipeline.
func NewHugotEmbedder(opts HugotOptions) (*HugotEmbedder, error) {
	modelName := opts.ModelName
	if modelName == "" {
		modelName = defaultModelName
	}

	modelPath := opts.ModelPath
	if modelPath == "" {
		resolved, err := resolveModelPath(modelName, opts.ModelDir)
		if err != nil {
			return nil, fmt.Errorf("embed: resolve model: %w", err)
		}
		modelPath = resolved
	}

	session, err := newHugotSession()
	if err != nil {
		return nil, fmt.Errorf("embed: create go session: %w", err)
	}

	pipeline, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         pipelineName,
		OnnxFilename: "onnx/model.onnx",
		Options:      []hugot.FeatureExtractionOption{pipelines.WithNormalization()},
	})
	if err != nil {
		_ = session.Destroy()
		return nil, fmt.Errorf("embed: create pipeline: %w", err)
	}

	meta := pipeline.GetMetadata()
	dim := 0
	if len(meta.OutputsInfo) > 0 && len(meta.OutputsInfo[0].Dimensions) > 0 {
		dims := meta.OutputsInfo[0].Dimensions
		dim = int(dims[len(dims)-1])
	}
	if dim <= 0 {
		_ = session.Destroy()
		return nil, fmt.Errorf("embed: could not determine embedding dimension from model")
	}

	return &HugotEmbedder{
		pipeline:        pipeline,
		session:         session,
		dim:             dim,
		needsTruncation: needsGoMLXTruncation(),
	}, nil
}

func (h *HugotEmbedder) Dimension() int { return h.dim }

const maxBatchSize = 32

// maxCharsGoMLX is the safety truncation limit for the pure-Go GoMLX backend,
// which does not truncate internally and crashes on sequences > 512 tokens.
// ORT backend truncates via the Rust tokenizer, so this is not needed there.
const maxCharsGoMLX = 1500

func (h *HugotEmbedder) Embed(texts []string) ([][]float32, error) {
	if texts == nil {
		return nil, fmt.Errorf("embed: nil input")
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	input := texts
	if h.needsTruncation {
		input = make([]string, len(texts))
		for i, t := range texts {
			if len(t) > maxCharsGoMLX {
				input[i] = t[:maxCharsGoMLX]
			} else {
				input[i] = t
			}
		}
	}

	all := make([][]float32, 0, len(input))
	for start := 0; start < len(input); start += maxBatchSize {
		end := start + maxBatchSize
		if end > len(input) {
			end = len(input)
		}
		result, err := h.pipeline.RunPipeline(input[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed: run pipeline: %w", err)
		}
		all = append(all, result.Embeddings...)
	}
	return all, nil
}

// Close destroys the hugot session and frees resources.
func (h *HugotEmbedder) Close() error {
	if h.session != nil {
		return h.session.Destroy()
	}
	return nil
}

// resolveModelPath checks for a local model or downloads it.
func resolveModelPath(modelName, modelDir string) (string, error) {
	if modelDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("user home: %w", err)
		}
		modelDir = filepath.Join(home, defaultModelDir)
	}

	dirName := strings.ReplaceAll(modelName, "/", "_")
	modelPath := filepath.Join(modelDir, dirName)

	if hasONNXModel(modelPath) {
		return modelPath, nil
	}

	slog.Info("downloading model (first use)", "model", modelName, "dest", modelDir)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return "", fmt.Errorf("create model dir: %w", err)
	}

	downloadOpts := hugot.NewDownloadOptions()
	downloadOpts.OnnxFilePath = "onnx/model.onnx"
	downloaded, err := hugot.DownloadModel(modelName, modelDir, downloadOpts)
	if err != nil {
		return "", fmt.Errorf("download model %s: %w", modelName, err)
	}
	slog.Info("model ready", "path", downloaded)
	return downloaded, nil
}

// hasONNXModel checks if the model directory has the expected ONNX file.
func hasONNXModel(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "onnx", "model.onnx"))
	return err == nil
}
