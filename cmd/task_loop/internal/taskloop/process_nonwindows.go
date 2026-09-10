//go:build !windows

package taskloop

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

func newWindowsBatchCommand(ctx context.Context, comspec, batchCommand string) *exec.Cmd {
	return exec.CommandContext(ctx, comspec, "/d", "/v:on", "/s", "/c", batchCommand)
}

func configureProcessCancellation(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 5 * time.Second
}
