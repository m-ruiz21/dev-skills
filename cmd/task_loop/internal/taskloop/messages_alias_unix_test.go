//go:build !windows

package taskloop

import (
	"os"
	"testing"
)

func TestMessageFileParentSymlinkCannotAliasDestination(t *testing.T) {
	assertParentDirectoryAliasCannotConsumeDestination(t, os.Symlink)
}
