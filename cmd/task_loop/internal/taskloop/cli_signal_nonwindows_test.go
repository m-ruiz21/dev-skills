//go:build !windows

package taskloop

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	signalHelperModeEnvironment = "TASK_LOOP_SIGNAL_HELPER"
	signalHelperPIDEnvironment  = "TASK_LOOP_SIGNAL_DESCENDANT_PID"
	signalHelperTestExecutable  = "TASK_LOOP_SIGNAL_TEST_EXE"
)

func TestCLISIGTERMCancelsProcessGroupAndCleansRunState(t *testing.T) {
	switch os.Getenv(signalHelperModeEnvironment) {
	case "agent":
		runSignalAgentHelper(t)
		return
	case "descendant":
		runSignalDescendantHelper(t)
		return
	}

	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)
	pidPath := filepath.Join(repo, "descendant.pid")
	script := filepath.Join(repo, "copilot")
	writeFile(t, script, "#!/bin/sh\nexec env "+signalHelperModeEnvironment+"=agent \"$"+signalHelperTestExecutable+"\" -test.run=^TestCLISIGTERMCancelsProcessGroupAndCleansRunState$\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(signalHelperModeEnvironment, "")
	t.Setenv(signalHelperPIDEnvironment, pidPath)
	t.Setenv(signalHelperTestExecutable, os.Args[0])

	cli := NewCLI()
	cli.Root = repo
	var stdout, stderr bytes.Buffer
	result := make(chan int, 1)
	go func() {
		result <- cli.RunWithAgents([]string{prd}, &stdout, &stderr, Agents{
			Triage: func(context TriageContext) (string, error) {
				return context.Candidates[0].Path, nil
			},
			CopilotBin: script,
		})
	}()

	waitForSignalTestPath(t, pidPath)
	pidText, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	descendantPID, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(descendantPID, syscall.SIGKILL) })

	stateRoot := filepath.Join(repo, ".scratch", "feature", workspaceStateDirectory)
	entries, err := os.ReadDir(stateRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("active run state entries=%v err=%v", entries, err)
	}
	promptPath := filepath.Join(stateRoot, entries[0].Name(), ".task-loop-prompt-signal-test.txt")
	writeFile(t, promptPath, "prompt")

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-result:
		if code != 1 || !strings.Contains(stderr.String(), "development agent failed: context canceled") {
			t.Fatalf("exit=%d stderr=%q", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SIGTERM did not cancel the task loop")
	}

	waitForSignalTestProcessExit(t, descendantPID)
	entries, err = os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("SIGTERM left run state or prompt files: %v", entries)
	}
}

func runSignalAgentHelper(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestCLISIGTERMCancelsProcessGroupAndCleansRunState$")
	command.Env = replaceProcessEnvironment(os.Environ(), signalHelperModeEnvironment, "descendant")
	if err := command.Run(); err != nil {
		t.Fatalf("descendant helper failed: %v", err)
	}
	t.Fatal("descendant helper exited unexpectedly")
}

func runSignalDescendantHelper(t *testing.T) {
	path := os.Getenv(signalHelperPIDEnvironment)
	if path == "" {
		t.Fatal("descendant PID path is missing")
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func waitForSignalTestPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForSignalTestProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if err != nil {
			t.Fatalf("inspect descendant process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("descendant process %d survived process-group cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
