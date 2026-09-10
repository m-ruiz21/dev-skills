package taskloop

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCLIListsSortedPRDsAndValidatesArguments(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "zeta")
	writePRD(t, repo, "alpha")
	cli := NewCLI()
	cli.Root = repo
	var stdout, stderr bytes.Buffer
	if code := cli.Run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != ".scratch/alpha/PRD.md\n.scratch/zeta/PRD.md\n" {
		t.Fatalf("unexpected listing %q", got)
	}
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run([]string{"missing/PRD.md"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "does not exist") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestCLISelectsDiscoveredPRDFromKeyboardPicker(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "zeta")
	writePRD(t, repo, "alpha")
	writeIssue(t, repo, "zeta", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)

	cli := NewCLI()
	cli.Root = repo
	cli.Deps.PRDSelector = PRDSelectorFunc(func(_ context.Context, options []PRDOption) (PRDOption, bool, error) {
		if len(options) != 2 ||
			options[0].Label != ".scratch/alpha/PRD.md" ||
			options[1].Label != ".scratch/zeta/PRD.md" {
			t.Fatalf("unexpected picker options: %+v", options)
		}
		return options[1], true, nil
	})
	agents := Agents{
		Triage: func(context TriageContext) (string, error) {
			return context.Candidates[0].Path, nil
		},
		Develop: func(context.Context, DevelopmentContext) (string, error) { return "[completed] Done.", nil },
		Test:    func(context.Context, TestingContext) (string, error) { return "[success]", nil },
		Review:  func(context.Context, ReviewContext) (string, error) { return passingReviewJSON(t), nil },
	}
	var stdout, stderr bytes.Buffer

	if code := cli.RunWithAgents(nil, &stdout, &stderr, agents); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(filepath.ToSlash(stdout.String()), "Selected PRD: "+filepath.ToSlash(filepath.Join(repo, ".scratch", "zeta", "PRD.md"))) {
		t.Fatalf("selected PRD was not used: %s", stdout.String())
	}
}

func TestCLIHandlesCancelledPRDSelectionWithoutStartingWork(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "feature")
	cli := NewCLI()
	cli.Root = repo
	cli.Deps.PRDSelector = PRDSelectorFunc(func(context.Context, []PRDOption) (PRDOption, bool, error) {
		return PRDOption{}, false, nil
	})
	var stdout, stderr bytes.Buffer

	if code := cli.Run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "PRD selection cancelled.\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCLIInstallsCancellationBeforePRDSelection(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "feature")
	interrupts := make(chan os.Signal, 1)
	interrupts <- os.Interrupt
	cli := NewCLI()
	cli.Root = repo
	cli.Deps.PRDSelector = PRDSelectorFunc(func(ctx context.Context, _ []PRDOption) (PRDOption, bool, error) {
		select {
		case <-ctx.Done():
			return PRDOption{}, false, ctx.Err()
		case <-time.After(time.Second):
			t.Fatal("PRD selector did not receive cancellation")
			return PRDOption{}, false, nil
		}
	})
	var stdout, stderr bytes.Buffer

	code := cli.RunWithAgents(nil, &stdout, &stderr, Agents{Interrupts: interrupts})

	if code != 1 || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLISelectsOnceAndUsesDefaultBudget(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)
	cli := NewCLI()
	cli.Root = repo
	var stdout, stderr bytes.Buffer
	triageCalls := 0
	agents := Agents{
		Triage: func(context TriageContext) (string, error) {
			triageCalls++
			return context.Candidates[0].Path, nil
		},
		Develop: func(context.Context, DevelopmentContext) (string, error) { return "[completed] Done.", nil },
		Test:    func(context.Context, TestingContext) (string, error) { return "[success]", nil },
		Review:  func(context.Context, ReviewContext) (string, error) { return passingReviewJSON(t), nil },
	}

	code := cli.RunWithAgents([]string{prd}, &stdout, &stderr, agents)
	if code != 0 || triageCalls != 1 {
		t.Fatalf("exit=%d triage=%d stderr=%s", code, triageCalls, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Max iterations: 10") {
		t.Fatal(stdout.String())
	}
}

func TestCLIProvidesAgentsTheAbsoluteRunningExecutable(t *testing.T) {
	cli := NewCLI()
	var stdout, stderr bytes.Buffer
	_, commandPath := cli.completeAgents(&stdout, &stderr, "", Agents{})
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	if commandPath != expected || !filepath.IsAbs(commandPath) {
		t.Fatalf("command path = %q, want absolute running executable %q", commandPath, expected)
	}
}

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("TASK_LOOP_HELPER") != "1" {
		return
	}
	arguments := os.Args
	for len(arguments) > 0 && arguments[0] != "--" {
		arguments = arguments[1:]
	}
	if len(arguments) > 0 {
		arguments = arguments[1:]
	}
	os.Exit(NewCLI().Run(arguments, os.Stdout, os.Stderr))
}

func TestCLISubprocessAddMessageAndDiscovery(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "zeta")
	writePRD(t, repo, "alpha")
	run := func(arguments ...string) (string, string, int) {
		command := exec.Command(os.Args[0], append([]string{"-test.run=TestCLIHelperProcess", "--"}, arguments...)...)
		command.Dir = repo
		command.Env = append(os.Environ(), "TASK_LOOP_HELPER=1")
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return stdout.String(), stderr.String(), code
	}
	stdout, stderr, code := run()
	if code != 0 || stderr != "" || stdout != ".scratch/alpha/PRD.md\n.scratch/zeta/PRD.md\n" {
		t.Fatalf("listing: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	target := filepath.Join("review", "01.md")
	stdout, stderr, code = run("add-message", "-file", target, "-message", "Starting.", "-from", "developer")
	if code != 0 || stdout != "Thread 1\n" || stderr != "" {
		t.Fatalf("message: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	content, _ := os.ReadFile(filepath.Join(repo, target))
	if !strings.Contains(string(content), "Starting.") {
		t.Fatal("subprocess did not persist message")
	}
	messagePath := filepath.Join(repo, "message input.txt")
	special := "quotes ' \" ; & | $() `n\nsecond line"
	writeFile(t, messagePath, special)
	stdout, stderr, code = run("add-message", "-file", target, "-message-file", messagePath, "-from", "developer")
	if code != 0 || stdout != "Thread 2\n" || stderr != "" {
		t.Fatalf("file message: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	content, _ = os.ReadFile(filepath.Join(repo, target))
	if !strings.Contains(string(content), special) {
		t.Fatal("subprocess did not preserve file-based message content")
	}
	if _, err := os.Stat(messagePath); !os.IsNotExist(err) {
		t.Fatalf("consumed message file was not removed: %v", err)
	}
	_, stderr, code = run("add-message", "-file", target, "-message", "Bad.", "-from", "admin")
	if code != 1 || !strings.Contains(stderr, "task-loop: error:") {
		t.Fatalf("validation: exit=%d stderr=%q", code, stderr)
	}
}

func TestCLIParsersRejectOptionTokensAsValues(t *testing.T) {
	runTests := []struct {
		name      string
		arguments []string
		errorText string
	}{
		{"next option", []string{"--max-iterations", "--max-iterations", "3"}, "argument --max-iterations: expected one argument"},
		{"equals option", []string{"--max-iterations", "--max-iterations=3"}, "argument --max-iterations: expected one argument"},
		{"help option", []string{"--max-iterations", "--help"}, "argument --max-iterations: expected one argument"},
	}
	for _, test := range runTests {
		t.Run("run "+test.name, func(t *testing.T) {
			_, _, _, err := parseRunArguments(test.arguments)
			if err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("error=%v, want %q", err, test.errorText)
			}
		})
	}

	addMessageTests := []struct {
		name      string
		arguments []string
		errorText string
	}{
		{"file", []string{"-file", "-message", "body", "-from", "developer"}, "argument -file: expected one argument"},
		{"message", []string{"-file", "review/01.md", "-message", "-from", "developer"}, "argument -message: expected one argument"},
		{"message file", []string{"-file", "review/01.md", "-message-file", "-from", "developer"}, "argument -message-file: expected one argument"},
		{"sender", []string{"-file", "review/01.md", "-message", "body", "-from", "-to", "1"}, "argument -from: expected one argument"},
		{"thread", []string{"-file", "review/01.md", "-message", "body", "-from", "developer", "-to", "-file"}, "argument -to: expected one argument"},
		{"help", []string{"-file", "--help"}, "argument -file: expected one argument"},
	}
	for _, test := range addMessageTests {
		t.Run("add-message "+test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := NewCLI().runAddMessage(testRepo(t), test.arguments, &stdout, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), test.errorText) {
				t.Fatalf("exit=%d stderr=%q, want argparse-style failure %q", code, stderr.String(), test.errorText)
			}
		})
	}
}

func TestCLIAddMessageRequiresExactlyOneMessageInput(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		errorText string
	}{
		{
			name:      "missing",
			arguments: []string{"-file", "review/01.md", "-from", "developer"},
			errorText: "one of the arguments -message -message-file is required",
		},
		{
			name:      "both",
			arguments: []string{"-file", "review/01.md", "-message", "inline", "-message-file", "message.txt", "-from", "developer"},
			errorText: "argument -message-file: not allowed with argument -message",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := NewCLI().runAddMessage(testRepo(t), test.arguments, &stdout, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), test.errorText) {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
		})
	}
}

func TestCLIAddMessageRejectsUsingTargetAsMessageFile(t *testing.T) {
	repo := testRepo(t)
	target := filepath.Join(repo, "review", "01.md")
	writeFile(t, target, "message source")
	var stdout, stderr bytes.Buffer
	code := NewCLI().runAddMessage(repo, []string{
		"-file", target,
		"-message-file", target,
		"-from", "developer",
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "must differ") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != "message source" {
		t.Fatalf("target changed: content=%q err=%v", content, err)
	}
}

type blockingInput struct {
	started chan struct{}
	release chan struct{}
}

func (input *blockingInput) Read(string) (string, bool) {
	close(input.started)
	<-input.release
	return "", false
}

func TestCLIInterruptsClarityAndCleansRunState(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)
	interrupts := make(chan os.Signal, 1)
	input := &blockingInput{started: make(chan struct{}), release: make(chan struct{})}
	defer close(input.release)
	agents := Agents{
		Triage: func(context TriageContext) (string, error) {
			return context.Candidates[0].Path, nil
		},
		Develop: func(context.Context, DevelopmentContext) (string, error) {
			go func() {
				<-input.started
				interrupts <- os.Interrupt
			}()
			return "[needs-clarity] Which implementation?", nil
		},
		Stdin:      input,
		Interrupts: interrupts,
	}
	cli := NewCLI()
	cli.Root = repo
	var stdout, stderr bytes.Buffer
	if code := cli.RunWithAgents([]string{prd}, &stdout, &stderr, agents); code != 1 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "clarity request was cancelled") {
		t.Fatalf("interrupt was not converted to cancellation: %s", stderr.String())
	}
	select {
	case <-input.started:
	default:
		t.Fatal("clarity input was not started")
	}
	stateRoot := filepath.Join(repo, ".scratch", "feature", workspaceStateDirectory)
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("deferred workspace cleanup left run state: %v", entries)
	}
}

func TestCLIInterruptsBaselineGitAndCleansOnlyItsRunState(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)
	stateRoot := filepath.Join(repo, ".scratch", "feature", workspaceStateDirectory)
	peerState := filepath.Join(stateRoot, "run-peer")
	writeFile(t, filepath.Join(peerState, "keep.txt"), "peer")

	interrupts := make(chan os.Signal, 1)
	started := make(chan struct{})
	var once sync.Once
	blockingGit := func(ctx context.Context, environment []string, root string, arguments ...string) (string, error) {
		if len(arguments) >= 3 && arguments[0] == "rev-parse" && arguments[1] == "--git-path" && arguments[2] == "objects" {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return "", ctx.Err()
		}
		return runGit(ctx, environment, root, arguments...)
	}
	cli := NewCLI()
	cli.Root = repo
	cli.Deps.CaptureBaseline = func(ctx context.Context, root, runDirectory string) (WorkspaceBaseline, error) {
		return captureWorkspaceBaseline(ctx, root, runDirectory, blockingGit)
	}
	go func() {
		<-started
		interrupts <- os.Interrupt
	}()

	var stdout, stderr bytes.Buffer
	code := cli.RunWithAgents([]string{prd}, &stdout, &stderr, Agents{
		Triage: func(context TriageContext) (string, error) {
			return context.Candidates[0].Path, nil
		},
		Interrupts: interrupts,
	})
	if code != 1 || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "run-peer" {
		t.Fatalf("baseline cancellation damaged peer state or leaked its unique state: %v", entries)
	}
	if content, err := os.ReadFile(filepath.Join(peerState, "keep.txt")); err != nil || string(content) != "peer" {
		t.Fatalf("peer state changed: content=%q err=%v", content, err)
	}
}

func TestCLIInterruptsDeltaGitAndRunsDeferredStateCleanup(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)
	stateRoot := filepath.Join(repo, ".scratch", "feature", workspaceStateDirectory)
	peerState := filepath.Join(stateRoot, "run-peer")
	writeFile(t, filepath.Join(peerState, "keep.txt"), "peer")

	interrupts := make(chan os.Signal, 1)
	started := make(chan struct{})
	blockDelta := false
	blockingGit := func(ctx context.Context, environment []string, root string, arguments ...string) (string, error) {
		if blockDelta && len(arguments) > 0 && arguments[0] == "write-tree" {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		}
		return runGit(ctx, environment, root, arguments...)
	}
	cli := NewCLI()
	cli.Root = repo
	cli.Deps.CaptureBaseline = func(ctx context.Context, root, runDirectory string) (WorkspaceBaseline, error) {
		baseline, err := captureWorkspaceBaseline(ctx, root, runDirectory, blockingGit)
		blockDelta = err == nil
		return baseline, err
	}
	go func() {
		<-started
		interrupts <- os.Interrupt
	}()

	var stdout, stderr bytes.Buffer
	code := cli.RunWithAgents([]string{prd}, &stdout, &stderr, Agents{
		Triage: func(context TriageContext) (string, error) {
			return context.Candidates[0].Path, nil
		},
		Develop:    func(context.Context, DevelopmentContext) (string, error) { return "[completed] Done.", nil },
		Test:       func(context.Context, TestingContext) (string, error) { return "[success]", nil },
		Review:     func(context.Context, ReviewContext) (string, error) { return passingReviewJSON(t), nil },
		Interrupts: interrupts,
	})
	if code != 1 || !strings.Contains(stderr.String(), "review:") || !strings.Contains(stderr.String(), "context canceled") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "run-peer" {
		t.Fatalf("deferred cleanup damaged peer state or leaked its unique state: %v", entries)
	}
}

func TestCLIInterruptsActiveAgentsAndCleansRunState(t *testing.T) {
	for _, phase := range []string{"development", "testing", "review"} {
		t.Run(phase, func(t *testing.T) {
			repo := testRepo(t)
			prd := writePRD(t, repo, "feature")
			writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
			initializeGitRepo(t, repo)
			interrupts := make(chan os.Signal, 1)
			started := make(chan struct{})
			agents := Agents{
				Triage: func(context TriageContext) (string, error) {
					return context.Candidates[0].Path, nil
				},
				Develop: func(context.Context, DevelopmentContext) (string, error) {
					return "[completed] Done.", nil
				},
				Test: func(context.Context, TestingContext) (string, error) {
					return "[success]", nil
				},
				Review: func(context.Context, ReviewContext) (string, error) {
					return passingReviewJSON(t), nil
				},
				Interrupts: interrupts,
			}
			block := func(ctx context.Context) (string, error) {
				close(started)
				<-ctx.Done()
				return "", ctx.Err()
			}
			switch phase {
			case "development":
				agents.Develop = func(ctx context.Context, _ DevelopmentContext) (string, error) {
					return block(ctx)
				}
			case "testing":
				agents.Test = func(ctx context.Context, _ TestingContext) (string, error) {
					return block(ctx)
				}
			case "review":
				agents.Review = func(ctx context.Context, _ ReviewContext) (string, error) {
					return block(ctx)
				}
			}
			go func() {
				<-started
				interrupts <- os.Interrupt
			}()
			cli := NewCLI()
			cli.Root = repo
			var stdout, stderr bytes.Buffer
			if code := cli.RunWithAgents([]string{prd}, &stdout, &stderr, agents); code != 1 {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), phase+" agent failed: context canceled") {
				t.Fatalf("interrupt was not reported as %s cancellation: %s", phase, stderr.String())
			}
			stateRoot := filepath.Join(repo, ".scratch", "feature", workspaceStateDirectory)
			entries, err := os.ReadDir(stateRoot)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("deferred workspace cleanup left run state: %v", entries)
			}
		})
	}
}
