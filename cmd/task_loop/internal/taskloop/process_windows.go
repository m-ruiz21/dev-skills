//go:build windows

package taskloop

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func newWindowsBatchCommand(ctx context.Context, comspec, batchCommand string) *exec.Cmd {
	command := exec.CommandContext(ctx, comspec)
	executable := `"` + strings.ReplaceAll(comspec, `"`, `""`) + `"`
	command.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       executable + ` /d /v:on /s /c "` + batchCommand + `"`,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	return command
}

func configureProcessCancellation(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		killer := exec.Command("taskkill.exe", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
		if err := killer.Run(); err == nil {
			return nil
		}
		return command.Process.Kill()
	}
	command.WaitDelay = 5 * time.Second
}
