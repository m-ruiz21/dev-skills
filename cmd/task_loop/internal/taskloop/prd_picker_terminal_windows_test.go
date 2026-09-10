//go:build windows

package taskloop

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPreparePRDPickerOutputIgnoresRedirectedOutput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	restore, err := preparePRDPickerOutput(writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparePRDPickerOutputEnablesAndRestoresVirtualTerminalProcessing(t *testing.T) {
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		t.Skip("test process has no controlling Windows console")
	}
	defer output.Close()
	handle := windows.Handle(output.Fd())
	var originalMode uint32
	if err := windows.GetConsoleMode(handle, &originalMode); err != nil {
		t.Fatal(err)
	}

	restore, err := preparePRDPickerOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	restorePending := true
	t.Cleanup(func() {
		if restorePending {
			_ = restore()
		}
	})

	var enabledMode uint32
	if err := windows.GetConsoleMode(handle, &enabledMode); err != nil {
		t.Fatal(err)
	}
	if enabledMode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
		t.Fatal("virtual-terminal processing is not enabled")
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	restorePending = false

	var restoredMode uint32
	if err := windows.GetConsoleMode(handle, &restoredMode); err != nil {
		t.Fatal(err)
	}
	if restoredMode != originalMode {
		t.Fatalf("restored mode %#x, want %#x", restoredMode, originalMode)
	}
}
