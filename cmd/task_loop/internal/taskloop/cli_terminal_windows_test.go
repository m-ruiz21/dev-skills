//go:build windows

package taskloop

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCLIUsesInteractivePRDPickerWhenStdoutIsRedirected(t *testing.T) {
	repo := testRepo(t)
	for index := 1; index <= maximumPRDViewport+2; index++ {
		writePRD(t, repo, fmt.Sprintf("feature-%02d", index))
	}
	t.Setenv("TASK_LOOP_HELPER", "1")
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputReader.Close()
	defer outputWriter.Close()
	if err := windows.SetHandleInformation(
		windows.Handle(outputWriter.Fd()),
		windows.HANDLE_FLAG_INHERIT,
		windows.HANDLE_FLAG_INHERIT,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASK_LOOP_TEST_OUTPUT_HANDLE", strconv.FormatUint(uint64(outputWriter.Fd()), 10))

	result := runInPseudoConsole(
		t,
		repo,
		os.Args[0],
		"-test.run=^TestWindowsCLIPseudoConsoleHelper$ --",
		outputReader,
		outputWriter,
		"q",
	)

	if result.exitCode != 0 {
		t.Fatalf("exit=%d terminal=%q stdout=%q", result.exitCode, result.terminal, result.stdout)
	}
	if !strings.Contains(result.terminal, "Select a PRD") {
		t.Fatalf("interactive CLI did not render the picker on the terminal: %q", result.terminal)
	}
	offscreenPath := fmt.Sprintf(".scratch/feature-%02d/PRD.md", maximumPRDViewport+1)
	if strings.Contains(result.terminal, offscreenPath) {
		t.Fatalf("interactive CLI dumped offscreen path %q instead of limiting the terminal viewport: %q", offscreenPath, result.terminal)
	}
	if result.stdout != "PRD selection cancelled.\n" {
		t.Fatalf("redirected stdout contains picker output instead of only ordinary CLI output: %q", result.stdout)
	}
}

func TestWindowsCLIPseudoConsoleHelper(t *testing.T) {
	if os.Getenv("TASK_LOOP_HELPER") != "1" {
		return
	}
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	outputHandle, err := strconv.ParseUint(os.Getenv("TASK_LOOP_TEST_OUTPUT_HANDLE"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	output := os.NewFile(uintptr(outputHandle), "task-loop-test-output")
	if output == nil {
		t.Fatal("inherited task-loop output handle is invalid")
	}
	defer output.Close()

	if executable := os.Getenv("TASK_LOOP_BINARY_UNDER_TEST"); executable != "" {
		command := exec.Command(executable)
		command.Stdin, command.Stdout, command.Stderr = input, output, output
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
		return
	}
	os.Stdin, os.Stdout, os.Stderr = input, output, output
	os.Exit(NewCLI().Run(nil, output, output))
}

type pseudoConsoleResult struct {
	terminal string
	stdout   string
	exitCode uint32
}

func runInPseudoConsole(
	t *testing.T,
	directory, executable, arguments string,
	redirectedOutputReader, redirectedOutputWriter *os.File,
	input string,
) pseudoConsoleResult {
	t.Helper()
	inputReader, inputWriter := newPseudoConsolePipe(t)
	defer inputWriter.Close()
	consoleOutputReader, consoleOutputWriter := newPseudoConsolePipe(t)
	defer consoleOutputReader.Close()

	var pseudoConsole windows.Handle
	if err := windows.CreatePseudoConsole(
		windows.Coord{X: 80, Y: 24},
		windows.Handle(inputReader.Fd()),
		windows.Handle(consoleOutputWriter.Fd()),
		0,
		&pseudoConsole,
	); err != nil {
		t.Fatal(err)
	}

	pseudoConsoleOpen := true
	defer func() {
		if pseudoConsoleOpen {
			windows.ClosePseudoConsole(pseudoConsole)
		}
	}()
	inputReader.Close()
	consoleOutputWriter.Close()

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}
	defer attributes.Delete()
	if err := attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		*(*unsafe.Pointer)(unsafe.Pointer(&pseudoConsole)),
		unsafe.Sizeof(pseudoConsole),
	); err != nil {
		t.Fatal(err)
	}

	commandLine := `"` + executable + `"`
	if arguments != "" {
		commandLine += " " + arguments
	}
	commandLinePointer, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		t.Fatal(err)
	}
	directoryPointer, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		t.Fatal(err)
	}
	startupInfo := windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attributes.List(),
	}
	var processInfo windows.ProcessInformation
	if err := windows.CreateProcess(
		nil,
		commandLinePointer,
		nil,
		nil,
		true,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		nil,
		directoryPointer,
		&startupInfo.StartupInfo,
		&processInfo,
	); err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(processInfo.Process)
	defer windows.CloseHandle(processInfo.Thread)

	var terminalOutput bytes.Buffer
	var terminalOutputMu sync.Mutex
	var redirectedOutput bytes.Buffer
	pickerReady := make(chan struct{})
	var pickerReadyOnce sync.Once
	var readDone sync.WaitGroup
	readErrors := make(chan error, 2)
	readDone.Add(2)
	go func() {
		defer readDone.Done()
		_, err := io.Copy(&redirectedOutput, redirectedOutputReader)
		readErrors <- err
	}()
	go func() {
		defer readDone.Done()
		buffer := make([]byte, 4096)
		for {
			count, err := consoleOutputReader.Read(buffer)
			if count > 0 {
				terminalOutputMu.Lock()
				_, _ = terminalOutput.Write(buffer[:count])
				rendered := terminalOutput.String()
				terminalOutputMu.Unlock()
				if strings.Contains(rendered, "Select a PRD") {
					pickerReadyOnce.Do(func() { close(pickerReady) })
				}
			}
			if err != nil {
				readErrors <- err
				return
			}
		}
	}()
	processDone := make(chan error, 1)
	go func() {
		_, err := windows.WaitForSingleObject(processInfo.Process, windows.INFINITE)
		processDone <- err
	}()
	select {
	case <-pickerReady:
		if _, err := inputWriter.WriteString(input); err != nil {
			t.Fatal(err)
		}
	case err := <-processDone:
		if err != nil {
			t.Fatal(err)
		}
		processDone = nil
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the PRD picker or process exit")
	}
	if processDone != nil {
		if err := <-processDone; err != nil {
			t.Fatal(err)
		}
	}
	redirectedOutputWriter.Close()
	windows.ClosePseudoConsole(pseudoConsole)
	pseudoConsoleOpen = false
	readDone.Wait()
	close(readErrors)
	for err := range readErrors {
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
	}

	var exitCode uint32
	if err := windows.GetExitCodeProcess(processInfo.Process, &exitCode); err != nil {
		t.Fatal(err)
	}
	terminalOutputMu.Lock()
	defer terminalOutputMu.Unlock()
	return pseudoConsoleResult{
		terminal: filepath.ToSlash(terminalOutput.String()),
		stdout:   filepath.ToSlash(redirectedOutput.String()),
		exitCode: exitCode,
	}
}

func newPseudoConsolePipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	var reader, writer windows.Handle
	if err := windows.CreatePipe(&reader, &writer, nil, 0); err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(reader), "conpty-reader"), os.NewFile(uintptr(writer), "conpty-writer")
}
