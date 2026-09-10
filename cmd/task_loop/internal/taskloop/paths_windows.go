//go:build windows

package taskloop

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func canonicalExistingFilesystemPath(path string) (string, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, 32768)
	length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	resolved := windows.UTF16ToString(buffer[:length])
	switch {
	case strings.HasPrefix(resolved, `\\?\UNC\`):
		resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
	case strings.HasPrefix(resolved, `\\?\`):
		resolved = strings.TrimPrefix(resolved, `\\?\`)
	}
	return filepath.Clean(resolved), nil
}

func filesystemPathRedirect(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	}
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := windows.GetFileAttributes(pathPointer)
	if err != nil {
		return false, err
	}
	return attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}
