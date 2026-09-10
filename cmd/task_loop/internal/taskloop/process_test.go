package taskloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOSProcessRunnerResolvesCopilotCMDOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command shim behavior")
	}
	repo := testRepo(t)
	shim := filepath.Join(repo, "copilot.cmd")
	writeFile(t, shim, "@echo off\r\necho %1^|%2^|%3\r\n")
	t.Setenv("PATH", repo)
	stdout, stderr, code, err := (OSProcessRunner{}).Run(context.Background(), "copilot", "--yolo", "-p", "prompt")
	if err != nil || code != 0 || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q code=%d err=%v", stdout, stderr, code, err)
	}
	if strings.TrimSpace(stdout) != `"--yolo"|"-p"|"prompt"` {
		t.Fatalf("unexpected shim output: %q", stdout)
	}
}

func TestOSProcessRunnerInvokesWindowsBatchShimsWithoutInjection(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command shim behavior")
	}
	for _, extension := range []string{".cmd", ".bat"} {
		t.Run(extension, func(t *testing.T) {
			repo := testRepo(t)
			output := filepath.Join(repo, "arguments.json")
			shim := filepath.Join(repo, "copilot"+extension)
			writeFile(t, shim, "@echo off\r\n\"%TASK_LOOP_TEST_EXE%\" -test.run=^TestProcessRunnerArgumentHelper$ -- %*\r\n")
			t.Setenv("PATH", repo)
			t.Setenv("TASK_LOOP_TEST_EXE", os.Args[0])
			t.Setenv("TASK_LOOP_PROCESS_OUTPUT", output)
			expected := []string{
				"space value",
				"apostrophe's",
				"semi;colon",
				"amp&ersand",
				"pipe|value",
				"$dollar",
			}
			_, stderr, code, err := (OSProcessRunner{}).Run(context.Background(), "copilot", expected...)
			if err != nil || code != 0 {
				t.Fatalf("stderr=%q code=%d err=%v", stderr, code, err)
			}
			content, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var actual []string
			if err := json.Unmarshal(content, &actual); err != nil {
				t.Fatal(err)
			}
			if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
				t.Fatalf("arguments changed: got %#v, want %#v", actual, expected)
			}
		})
	}
}

func TestOSProcessRunnerKeepsWindowsNativeExecutablesDirect(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows native executable behavior")
	}

	t.Setenv("ComSpec", filepath.Join(testRepo(t), "missing-cmd.exe"))
	t.Setenv("TASK_LOOP_NATIVE_HELPER", "1")
	stdout, stderr, code, err := (OSProcessRunner{}).Run(context.Background(), os.Args[0], "-test.run=^TestProcessRunnerNativeHelper$")
	if err != nil || code != 0 || stderr != "" || !strings.Contains(stdout, "native-helper") {
		t.Fatalf("stdout=%q stderr=%q code=%d err=%v", stdout, stderr, code, err)
	}
}

func TestOSProcessRunnerWindowsBatchArgumentsCannotBreakOutOfQuotes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows command shim behavior")
	}
	repo := testRepo(t)
	sentinel := filepath.Join(repo, "injected.txt")
	shim := filepath.Join(repo, "copilot.cmd")
	writeFile(t, shim, "@echo off\r\n\"%TASK_LOOP_TEST_EXE%\" -test.run=^TestProcessRunnerArgumentHelper$ -- %*\r\n")
	t.Setenv("PATH", repo)
	t.Setenv("TASK_LOOP_TEST_EXE", os.Args[0])
	t.Setenv("TASK_LOOP_PROCESS_OUTPUT", filepath.Join(repo, "arguments.json"))
	payload := `safe"& echo injected>"` + sentinel + `" & rem "`
	_, _, _, err := (OSProcessRunner{}).Run(context.Background(), "copilot", payload)
	if err == nil {
		t.Fatal("unsafe non-prompt batch argument was not rejected")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("batch argument executed injected command: %v", err)
	}
}

func TestWindowsBatchPromptUsesSafeTemporaryFileTransport(t *testing.T) {
	repo := testRepo(t)
	state := filepath.Join(testRepo(t), "run-state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)

	prompt := "line one\nline \"two\" with ! and amp&pipe|"
	executable := resolvedExecutable{kind: executableWindowsBatch}
	arguments, cleanup, err := executable.prepareArguments(state, []string{"--yolo", "-p", prompt})
	if err != nil {
		t.Fatal(err)
	}
	reference := strings.TrimPrefix(arguments[2], "Read and follow the complete UTF-8 task prompt in file ")
	if reference == arguments[2] || strings.ContainsAny(arguments[2], "\"\r\n!") {
		t.Fatalf("unsafe prompt reference: %q", arguments[2])
	}
	if !filepath.IsAbs(reference) || filepath.Dir(reference) != state {
		t.Fatalf("prompt file = %q, want absolute path under %q", reference, state)
	}
	content, err := os.ReadFile(reference)
	if err != nil || string(content) != prompt {
		t.Fatalf("prompt file content=%q err=%v", content, err)
	}
	matches, err := filepath.Glob(filepath.Join(repo, ".task-loop-prompt-*.txt"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("repository-root prompt files=%v err=%v", matches, err)
	}
	cleanup()
	if _, err := os.Stat(reference); !os.IsNotExist(err) {
		t.Fatalf("prompt file was not removed: %v", err)
	}
}

func TestProcessRunnerArgumentHelper(t *testing.T) {
	output := os.Getenv("TASK_LOOP_PROCESS_OUTPUT")
	if output == "" {
		return
	}
	index := 0
	for index < len(os.Args) && os.Args[index] != "--" {
		index++
	}
	content, err := json.Marshal(os.Args[index+1:])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProcessRunnerNativeHelper(t *testing.T) {
	if os.Getenv("TASK_LOOP_NATIVE_HELPER") != "1" {
		return
	}
	fmt.Print("native-helper")
}

func TestOSProcessRunnerCancelsActiveProcess(t *testing.T) {
	repo := testRepo(t)
	started := filepath.Join(repo, "started")
	t.Setenv("TASK_LOOP_BLOCKING_HELPER", started)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, _, err := (OSProcessRunner{}).Run(ctx, os.Args[0], "-test.run=^TestProcessRunnerBlockingHelper$")
		result <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("subprocess exited before starting: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("subprocess did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled subprocess did not exit")
	}
}

func TestProcessRunnerBlockingHelper(t *testing.T) {
	started := os.Getenv("TASK_LOOP_BLOCKING_HELPER")
	if started == "" {
		return
	}
	if err := os.WriteFile(started, []byte("started"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestInvokeCopilotPreservesProcessFailures(t *testing.T) {
	runner := fakeRunner{stdout: "out", stderr: "boom", code: 7}
	_, err := invokeCopilot(context.Background(), runner, "copilot", "testing", "prompt")
	if err == nil || !strings.Contains(err.Error(), "testing agent process exited with status 7: boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReaderInputPreservesBufferedAnswersAcrossPrompts(t *testing.T) {
	var output bytes.Buffer
	input := NewReaderInput(strings.NewReader("first\nsecond\n"), &output)
	first, available := input.Read("one?")
	if !available || first != "first" {
		t.Fatalf("first=(%q,%v)", first, available)
	}
	second, available := input.Read("two?")
	if !available || second != "second" {
		t.Fatalf("second=(%q,%v)", second, available)
	}
	if output.String() != "one?\n> two?\n> " {
		t.Fatalf("unexpected prompts %q", output.String())
	}
}

type fakeRunner struct {
	stdout, stderr string
	code           int
	err            error
}

func (runner fakeRunner) Run(context.Context, string, ...string) (string, string, int, error) {
	return runner.stdout, runner.stderr, runner.code, runner.err
}
