//go:build !windows

package taskloop

import (
	"os"
	"path/filepath"
)

func canonicalExistingFilesystemPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func filesystemPathRedirect(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}
