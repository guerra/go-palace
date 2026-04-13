//go:build windows

package miner

import "os"

// verifyNotSymlink checks that path is not a symlink using Lstat.
// Windows lacks O_NOFOLLOW so the TOCTOU window is wider than on Unix,
// but this still catches the common case.
func verifyNotSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.ErrPermission
	}
	return nil
}
