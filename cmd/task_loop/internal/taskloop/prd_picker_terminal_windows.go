//go:build windows

package taskloop

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func openPRDPickerTerminalOutput() (*prdPickerTerminalOutput, error) {
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	restore, err := preparePRDPickerOutput(output)
	if err != nil {
		return nil, errors.Join(err, output.Close())
	}
	return newPRDPickerTerminalOutput(output, restore), nil
}

func preparePRDPickerOutput(output *os.File) (func() error, error) {
	handle := windows.Handle(output.Fd())
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return nil, err
	}
	if fileType != windows.FILE_TYPE_CHAR {
		return func() error { return nil }, nil
	}

	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	enabledMode := mode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if enabledMode == mode {
		return func() error { return nil }, nil
	}
	if err := windows.SetConsoleMode(handle, enabledMode); err != nil {
		return nil, err
	}
	return func() error {
		return windows.SetConsoleMode(handle, mode)
	}, nil
}

func waitForPRDPickerInput(input *os.File, timeout time.Duration) (bool, error) {
	event, err := windows.WaitForSingleObject(windows.Handle(input.Fd()), uint32(timeout.Milliseconds()))
	if err != nil {
		return false, err
	}
	return event == windows.WAIT_OBJECT_0, nil
}
