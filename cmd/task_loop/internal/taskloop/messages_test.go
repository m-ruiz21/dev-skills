package taskloop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMessageStoreAppendsThreadsRepliesAndTimestamp(t *testing.T) {
	repo := testRepo(t)
	store := MessageStore{Root: repo, Clock: fixedClock{time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("local", 3600))}}
	target := filepath.Join("review", "01.md")
	first, err := store.Add(target, "Question.", SenderReviewer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Add(target, "Work.", SenderDeveloper)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reply(target, "Answer.", SenderUser, first.ThreadID); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(filepath.Join(repo, target))
	text := string(content)
	for _, wanted := range []string{"-- Thread 1", "-- Thread 2", "-- Reply to Thread 1", "[reviewer] - 2026-01-02T02:04:05Z", "Answer."} {
		if !strings.Contains(text, wanted) {
			t.Errorf("missing %q in %q", wanted, text)
		}
	}
	if first.ThreadID == second.ThreadID || !first.IsNewThread {
		t.Fatal("thread identity was not preserved")
	}
}

func TestMessageStoreRejectsInvalidInputsWithoutMutation(t *testing.T) {
	repo := testRepo(t)
	store := MessageStore{Root: repo}
	target := filepath.Join("review", "01.md")
	tests := []struct {
		name, message, reply string
		sender               Sender
	}{
		{"sender", "hello", "", "admin"},
		{"message", " \t", "", SenderDeveloper},
		{"missing reply", "hello", "99", SenderUser},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.reply == "" {
				_, err = store.Add(target, test.message, test.sender)
			} else {
				_, err = store.Reply(target, test.message, test.sender, test.reply)
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(repo, target)); !os.IsNotExist(err) {
		t.Fatalf("invalid input created target: %v", err)
	}
	if _, err := store.Add(filepath.Join("..", "escaped.md"), "hello", SenderDeveloper); err == nil {
		t.Fatal("escaping path accepted")
	}
	if err := os.MkdirAll(filepath.Join(repo, "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("review", "hello", SenderDeveloper); err == nil {
		t.Fatal("directory target accepted")
	}
}

func TestMessageStoreConcurrentAppendsAreExclusive(t *testing.T) {
	repo := testRepo(t)
	store := MessageStore{Root: repo}
	const workers = 24
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			result, err := store.Add(filepath.Join("review", "concurrent.md"), "Message "+string(rune('A'+index)), SenderDeveloper)
			if err != nil {
				errs <- err
				return
			}
			ids <- result.ThreadID
		}(index)
	}
	wait.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	unique := make(map[string]bool)
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != workers {
		t.Fatalf("got %d unique IDs, want %d", len(unique), workers)
	}
	content, _ := os.ReadFile(filepath.Join(repo, "review", "concurrent.md"))
	if count := len(regexp.MustCompile(`(?m)^-- Thread \S+$`).FindAll(content, -1)); count != workers {
		t.Fatalf("got %d complete threads", count)
	}
}

func TestMessageStoreRejectsSymlinkEscape(t *testing.T) {
	repo := testRepo(t)
	outside := testRepo(t)
	link := filepath.Join(repo, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	store := MessageStore{Root: repo}
	if _, err := store.Add(filepath.Join("linked", "escaped.md"), "no", SenderDeveloper); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestProgressInstructionQuotesBundledAbsoluteCommand(t *testing.T) {
	commandPath := filepath.Join(string(filepath.Separator), "plugin root", "bin", "task-loop")
	if runtime.GOOS == "windows" {
		commandPath = `C:\plugin root\bin\task-loop.exe`
	}
	messagePath := filepath.Join(string(filepath.Separator), "run state", "message.txt")
	if runtime.GOOS == "windows" {
		messagePath = `C:\run state\message.txt`
	}
	instruction, err := BuildProgressUpdateInstruction("feature progress.txt", messagePath, SenderDeveloper, commandPath)
	if err != nil {
		t.Fatal(err)
	}
	var expected string
	if runtime.GOOS == "windows" {
		expected = `& 'C:\plugin root\bin\task-loop.exe' 'add-message' '-file' 'feature progress.txt' '-message-file' 'C:\run state\message.txt' '-from' 'developer'`
	} else {
		expected = `'/plugin root/bin/task-loop' 'add-message' '-file' 'feature progress.txt' '-message-file' '/run state/message.txt' '-from' 'developer'`
	}
	if !strings.Contains(instruction, expected) {
		t.Fatalf("instruction does not preserve the quoted bundled command:\n%s", instruction)
	}
}

func TestCommandFormattingUsesLiteralShellArguments(t *testing.T) {
	arguments := []string{`C:\Program Files\O'Brien\task-loop.exe`, "space value", "semi;colon", "amp&ersand", "pipe|value", "$dollar"}
	posix := formatCommandForShell(arguments, CommandShellPOSIX)
	if expected := `'C:\Program Files\O'"'"'Brien\task-loop.exe' 'space value' 'semi;colon' 'amp&ersand' 'pipe|value' '$dollar'`; posix != expected {
		t.Fatalf("POSIX command:\n got: %s\nwant: %s", posix, expected)
	}
	powerShell := formatCommandForShell(arguments, CommandShellPowerShell)
	if expected := `& 'C:\Program Files\O''Brien\task-loop.exe' 'space value' 'semi;colon' 'amp&ersand' 'pipe|value' '$dollar'`; powerShell != expected {
		t.Fatalf("PowerShell command:\n got: %s\nwant: %s", powerShell, expected)
	}
}

func TestFormattedCommandExecutesWithoutArgumentInjection(t *testing.T) {
	repo := testRepo(t)
	marker := filepath.Join(repo, "shell-injection-marker")
	helper := filepath.Join(repo, "plugin $(touch shell-injection-marker) `ticks` 'quotes' ; &", "task-loop")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	executable, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, executable, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(helper, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	values := []string{
		"space value",
		"apostrophe's and \"double quotes\"",
		"semi;colon & ampersand | pipe",
		"$(touch shell-injection-marker)",
		"`touch shell-injection-marker`",
		"$(New-Item -ItemType File -Path shell-injection-marker)",
		"line one\nline two",
	}
	shells := []struct {
		name       string
		kind       CommandShell
		candidates []string
		prefix     []string
	}{
		{name: "PowerShell", kind: CommandShellPowerShell, candidates: []string{"pwsh", "powershell.exe"}, prefix: []string{"-NoProfile", "-NonInteractive", "-Command"}},
		{name: "Bash", kind: CommandShellPOSIX, candidates: []string{"bash"}, prefix: []string{"-c"}},
	}
	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			shellPath := ""
			for _, candidate := range shell.candidates {
				if found, lookupErr := exec.LookPath(candidate); lookupErr == nil {
					shellPath = found
					break
				}
			}
			if shellPath == "" {
				t.Skipf("%s is unavailable", shell.name)
			}
			commandPath := helper
			if runtime.GOOS == "windows" && shell.kind == CommandShellPOSIX {
				commandPath = filepath.ToSlash(commandPath)
			}
			arguments := append([]string{commandPath, "-test.run=^TestCommandQuotingHelper$", "--"}, values...)
			formatted := formatCommandForShell(arguments, shell.kind)
			commandArguments := append(append([]string{}, shell.prefix...), formatted)
			command := exec.Command(shellPath, commandArguments...)
			command.Dir = repo
			output := filepath.Join(repo, "arguments-"+strings.ToLower(shell.name)+".json")
			command.Env = append(os.Environ(), "TASK_LOOP_QUOTE_OUTPUT="+output)
			if combined, runErr := command.CombinedOutput(); runErr != nil {
				t.Fatalf("execute %s: %v\n%s", formatted, runErr, combined)
			}
			content, readErr := os.ReadFile(output)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var actual []string
			if err := json.Unmarshal(content, &actual); err != nil {
				t.Fatal(err)
			}
			if strings.Join(actual, "\x00") != strings.Join(values, "\x00") {
				t.Fatalf("arguments changed: got %#v, want %#v", actual, values)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("%s command executed dynamic data and created marker: %v", shell.name, err)
			}
		})
	}
}

func TestCommandQuotingHelper(t *testing.T) {
	output := os.Getenv("TASK_LOOP_QUOTE_OUTPUT")
	if output == "" {
		return
	}
	index := 0
	for index < len(os.Args) && os.Args[index] != "--" {
		index++
	}
	if index == len(os.Args) {
		t.Fatal("missing argument separator")
	}
	content, err := json.Marshal(os.Args[index+1:])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedPromptUsesFileTransportWithoutMessageInterpolation(t *testing.T) {
	repo := testRepo(t)
	stateRoot := filepath.Join(repo, ".scratch", "feature", workspaceStateDirectory)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp(stateRoot, "run-")
	if err != nil {
		t.Fatal(err)
	}
	store := MessageStore{Root: repo, StateDirectory: state}
	messagePath, err := store.PrepareMessageFile(MessagePurposeDevelopmentProgress)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(repo, "task loop helper")
	if runtime.GOOS == "windows" {
		wrapper += ".cmd"
		writeFile(t, wrapper, "@echo off\r\n\"%TASK_LOOP_TEST_EXE%\" -test.run=^TestCLIHelperProcess$ -- %*\r\n")
	} else {
		writeFile(t, wrapper, "#!/bin/sh\nexec \"$TASK_LOOP_TEST_EXE\" -test.run=^TestCLIHelperProcess$ -- \"$@\"\n")
		if err := os.Chmod(wrapper, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	instruction, err := BuildProgressUpdateInstruction(filepath.Join("review", "prompt.md"), messagePath, SenderDeveloper, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(instruction, "<summary") || !strings.Contains(instruction, "-message-file") {
		t.Fatalf("instruction contains interpolated message placeholder:\n%s", instruction)
	}
	start := strings.Index(instruction, "`")
	end := strings.Index(instruction[start+1:], "`")
	if start < 0 || end < 0 {
		t.Fatalf("instruction has no exact command: %s", instruction)
	}
	commandText := instruction[start+1 : start+1+end]
	sentinel := filepath.Join(repo, "interpolated-command-ran.txt")
	message := "apostrophe's \"quotes\" ; & | $dollar `tick`\n$(echo injected > " + sentinel + ")\n"
	writeFile(t, messagePath, message)

	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", commandText)
	} else {
		command = exec.Command("sh", "-c", commandText)
	}
	command.Dir = repo
	command.Env = append(os.Environ(), "TASK_LOOP_HELPER=1", "TASK_LOOP_TEST_EXE="+os.Args[0])
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("execute generated command: %v\n%s\ncommand=%s", err, output, commandText)
	}
	content, err := os.ReadFile(filepath.Join(repo, "review", "prompt.md"))
	if err != nil || !strings.Contains(string(content), message) {
		t.Fatalf("message content=%q err=%v", content, err)
	}
	if _, err := os.Stat(messagePath); !os.IsNotExist(err) {
		t.Fatalf("consumed message file survived: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("message content was interpolated by the shell: %v", err)
	}
}

func TestMessageFileInputIsConfinedAndNonEmpty(t *testing.T) {
	repo := testRepo(t)
	outside := filepath.Join(testRepo(t), "message.txt")
	writeFile(t, outside, "outside")
	input, err := ParseMessageInput("", false, outside, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.Resolve(repo); err == nil {
		t.Fatal("accepted message file outside repository")
	}
	empty := filepath.Join(repo, "empty.txt")
	writeFile(t, empty, " \n")
	input, err = ParseMessageInput("", false, empty, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.Resolve(repo); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("accepted empty message file: %v", err)
	}
}

func TestMessageFileInputRejectsFinalSymlinkWithoutReadingOrDeletingTarget(t *testing.T) {
	repo := testRepo(t)
	target := filepath.Join(repo, "target.txt")
	link := filepath.Join(repo, "message.txt")
	writeFile(t, target, "do not consume")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	input, err := ParseMessageInput("", false, link, true)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := input.Resolve(repo)
	if err == nil {
		_ = resolved.Consume(filepath.Join(repo, "review", "destination.md"))
		t.Fatal("accepted final-component message-file symlink")
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "do not consume" {
		t.Fatalf("target was changed or deleted: content=%q err=%v", content, readErr)
	}
	if _, statErr := os.Lstat(link); statErr != nil {
		t.Fatalf("original symlink was removed: %v", statErr)
	}
}

func TestResolvedMessageInputDoesNotDeleteReplacementSymlinkTarget(t *testing.T) {
	repo := testRepo(t)
	source := filepath.Join(repo, "message.txt")
	target := filepath.Join(repo, "target.txt")
	writeFile(t, source, "message")
	writeFile(t, target, "keep target")
	input, err := ParseMessageInput("", false, source, true)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := input.Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, source); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	if err := resolved.Consume(filepath.Join(repo, "review", "destination.md")); err == nil {
		t.Fatal("consumed a replacement symlink")
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "keep target" {
		t.Fatalf("replacement target was changed or deleted: content=%q err=%v", content, readErr)
	}
}

func assertParentDirectoryAliasCannotConsumeDestination(t *testing.T, createAlias func(string, string) error) {
	t.Helper()
	repo := testRepo(t)
	destination := filepath.Join(repo, "review", "target.md")
	writeFile(t, destination, "destination must survive")
	aliasParent := filepath.Join(repo, "review-alias")
	if err := createAlias(aliasParent, filepath.Dir(destination)); err != nil {
		t.Skipf("directory aliases unavailable: %v", err)
	}

	var stdout, stderr strings.Builder
	code := NewCLI().runAddMessage(repo, []string{
		"-file", destination,
		"-message-file", filepath.Join(aliasParent, filepath.Base(destination)),
		"-from", "developer",
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "must differ") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "destination must survive" {
		t.Fatalf("destination changed or was deleted: content=%q err=%v", content, err)
	}
}
