package taskloop

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

func testRepo(t *testing.T) string {
	t.Helper()
	root := os.Getenv("TASK_LOOP_TEST_MODULE_ROOT")
	if root == "" {
		_, file, _, _ := runtime.Caller(0)
		root = filepath.Join(filepath.Dir(file), "..", "..")
	}
	root = filepath.Clean(filepath.Join(root, "tests", "tmp"))
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := os.MkdirTemp(root, "go-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	return repo
}

func initializeGitRepo(t *testing.T, repo string) {
	t.Helper()
	gitCommand(t, repo, "init", "--quiet")
	gitCommand(t, repo, "config", "user.name", "Task Loop Tests")
	gitCommand(t, repo, "config", "user.email", "task-loop@example.invalid")
	gitCommand(t, repo, "add", "-A")
	gitCommand(t, repo, "commit", "--quiet", "-m", "baseline")
}

func gitCommand(t *testing.T, repo string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, arguments...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", arguments, err, stderr.String())
	}
	return stdout.String()
}

func writePRD(t *testing.T, repo, feature string) string {
	t.Helper()
	path := filepath.Join(repo, ".scratch", feature, "PRD.md")
	writeFile(t, path, "# PRD\n")
	return path
}

func writeIssue(t *testing.T, repo, feature, name, status string, blockedBy []string, closed bool) string {
	t.Helper()
	dir := filepath.Join(repo, ".scratch", feature, "issues")
	if closed {
		dir = filepath.Join(dir, "closed")
	}
	lines := "---\ntitle: " + name + "\nstatus: " + status + "\nblocked-by:"
	if len(blockedBy) == 0 {
		lines += " []\n"
	} else {
		lines += "\n"
		for _, dependency := range blockedBy {
			lines += "  - " + dependency + "\n"
		}
	}
	path := filepath.Join(dir, name)
	writeFile(t, path, lines+"---\n\n## Work\n")
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testPhaseMessageStore(t *testing.T, repo string) MessageStore {
	t.Helper()
	stateRoot := filepath.Join(repo, ".scratch", "test-state", workspaceStateDirectory)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp(stateRoot, "run-")
	if err != nil {
		t.Fatal(err)
	}
	return MessageStore{Root: repo, StateDirectory: state}
}

func passingReviewJSON(t *testing.T) string {
	t.Helper()
	dimensions := make([]map[string]any, 0, len(RequiredDimensions))
	for _, name := range RequiredDimensions {
		dimensions = append(dimensions, map[string]any{"dimension": name, "grade": 90, "evidence": []string{"ok"}})
	}
	data, err := json.Marshal(map[string]any{"schemaVersion": "1.0", "runId": "test-run", "dimensions": dimensions, "findings": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
