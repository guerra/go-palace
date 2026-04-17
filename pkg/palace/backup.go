package palace

import (
	"fmt"
	"io"
	"os"
)

// createBackup copies palacePath (and .wal if present) to <path>.pre-v0.2.bak,
// avoiding overwrite of prior backup attempts via .bak.N suffix.
// Returns the base backup path (the .db file copy).
//
// palacePath is expected to originate from a trusted source (CLI flag or
// config file read at startup). The backup is written with mode 0o600 and
// O_EXCL so even a hostile path cannot overwrite an existing file or widen
// its permissions; the worst-case impact is writing a backup to an
// attacker-specified directory the process has write access to.
func createBackup(palacePath string) (string, error) {
	base := palacePath + ".pre-v0.2.bak"
	target := base
	for i := 1; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		target = fmt.Sprintf("%s.%d", base, i)
	}
	if err := copyFile(palacePath, target); err != nil {
		return "", err
	}
	// Also copy .wal if present (SQLite WAL mode). .shm is a transient cache and
	// will be regenerated on next open; skipping it is safe.
	walSrc := palacePath + "-wal"
	if _, err := os.Stat(walSrc); err == nil {
		if err := copyFile(walSrc, target+"-wal"); err != nil {
			return "", err
		}
	}
	return target, nil
}

// copyFile copies src to dst using streaming io.Copy. Fails if dst exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
