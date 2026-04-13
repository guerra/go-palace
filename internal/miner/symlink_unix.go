//go:build !windows

package miner

import (
	"fmt"
	"syscall"
)

// verifyNotSymlink opens a file with O_NOFOLLOW to ensure it is not a symlink.
// This narrows the TOCTOU window between scanning and reading file contents.
func verifyNotSymlink(path string) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("symlink check: %w", err)
	}
	syscall.Close(fd)
	return nil
}
