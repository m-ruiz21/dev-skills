//go:build !windows

package taskloop

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func openPRDPickerTerminalOutput() (*prdPickerTerminalOutput, error) {
	output, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	return newPRDPickerTerminalOutput(output, nil), nil
}

func waitForPRDPickerInput(input *os.File, timeout time.Duration) (bool, error) {
	descriptor := []unix.PollFd{{Fd: int32(input.Fd()), Events: unix.POLLIN}}
	ready, err := unix.Poll(descriptor, int(timeout.Milliseconds()))
	return ready > 0, err
}
