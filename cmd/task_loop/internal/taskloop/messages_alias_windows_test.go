//go:build windows

package taskloop

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestMessageFileParentJunctionCannotAliasDestination(t *testing.T) {
	assertParentDirectoryAliasCannotConsumeDestination(t, func(link, target string) error {
		output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			return fmt.Errorf("mklink /J failed: %w: %s", err, output)
		}
		return nil
	})
}
