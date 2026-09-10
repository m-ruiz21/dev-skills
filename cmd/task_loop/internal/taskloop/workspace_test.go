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
)

func TestWorkspaceDeltaIncludesOnlyChangesSinceRunStartWithoutMutatingIndex(t *testing.T) {
	repo := testRepo(t)
	runDirectory := filepath.Join(repo, ".scratch", "feature")
	writeFile(t, filepath.Join(runDirectory, "PRD.md"), "# PRD\n")
	for _, name := range []string{"issue-staged.txt", "issue-unstaged.txt", "unrelated-staged.txt", "preexisting-unstaged.txt"} {
		writeFile(t, filepath.Join(repo, name), "base\n")
	}
	initializeGitRepo(t, repo)

	writeFile(t, filepath.Join(repo, "unrelated-staged.txt"), "unrelated before run\n")
	gitCommand(t, repo, "add", "unrelated-staged.txt")
	writeFile(t, filepath.Join(repo, "preexisting-unstaged.txt"), "unstaged before run\n")
	writeFile(t, filepath.Join(repo, "preexisting-untracked.txt"), "untracked before run\n")
	indexBeforeBaseline := gitCommand(t, repo, "diff", "--cached", "--binary")

	baseline, err := CaptureWorkspaceBaseline(context.Background(), repo, runDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.Cleanup()
	if after := gitCommand(t, repo, "diff", "--cached", "--binary"); after != indexBeforeBaseline {
		t.Fatal("capturing the baseline mutated the user's index")
	}

	writeFile(t, filepath.Join(repo, "issue-staged.txt"), "staged issue change\n")
	gitCommand(t, repo, "add", "issue-staged.txt")
	writeFile(t, filepath.Join(repo, "issue-unstaged.txt"), "unstaged issue change\n")
	writeFile(t, filepath.Join(repo, "issue-untracked.txt"), "untracked issue change\n")
	indexBeforeDelta := gitCommand(t, repo, "diff", "--cached", "--binary")

	delta, err := baseline.PrepareDelta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(delta.Path)
	if err != nil {
		t.Fatal(err)
	}
	patch := string(content)
	for _, wanted := range []string{"issue-staged.txt", "issue-unstaged.txt", "issue-untracked.txt"} {
		if !strings.Contains(patch, wanted) {
			t.Errorf("issue delta missing %s:\n%s", wanted, patch)
		}
	}
	for _, excluded := range []string{"unrelated-staged.txt", "preexisting-unstaged.txt", "preexisting-untracked.txt", workspaceStateDirectory} {
		if strings.Contains(patch, excluded) {
			t.Errorf("issue delta included pre-run path %s:\n%s", excluded, patch)
		}
	}
	if after := gitCommand(t, repo, "diff", "--cached", "--binary"); after != indexBeforeDelta {
		t.Fatal("preparing the issue delta mutated the user's index")
	}
}

func TestWorkspaceBaselineRejectsRunDirectoryOutsideScratch(t *testing.T) {
	repo := testRepo(t)
	writeFile(t, filepath.Join(repo, "outside", "PRD.md"), "# PRD\n")
	initializeGitRepo(t, repo)
	if _, err := CaptureWorkspaceBaseline(context.Background(), repo, filepath.Join(repo, "outside")); err == nil {
		t.Fatal("accepted workspace state outside .scratch")
	}
}

func TestWorkspaceBaselineSupportsRepositoryWithoutCommits(t *testing.T) {
	repo := testRepo(t)
	runDirectory := filepath.Join(repo, ".scratch", "feature")
	writeFile(t, filepath.Join(runDirectory, "PRD.md"), "# PRD\n")
	gitCommand(t, repo, "init", "--quiet")
	baseline, err := CaptureWorkspaceBaseline(context.Background(), repo, runDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.Cleanup()
	writeFile(t, filepath.Join(repo, "new.txt"), "issue work\n")
	delta, err := baseline.PrepareDelta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(delta.Path)
	if err != nil || !strings.Contains(string(content), "new.txt") {
		t.Fatalf("unborn repository delta missing new file: %q err=%v", content, err)
	}
}

func TestWorkspaceDeltaPreservesIndexAndEffectiveWorktreeLayers(t *testing.T) {
	tests := []struct {
		name       string
		change     func(*testing.T, string)
		layerLabel string
		wantText   string
	}{
		{
			name: "staged deletion with retained worktree",
			change: func(t *testing.T, repo string) {
				gitCommand(t, repo, "rm", "--cached", "--quiet", "layer.txt")
			},
			layerLabel: "=== INDEX (staged state) ===",
			wantText:   "deleted file mode",
		},
		{
			name: "staged content followed by unstaged restoration",
			change: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "layer.txt"), "staged\n")
				gitCommand(t, repo, "add", "layer.txt")
				writeFile(t, filepath.Join(repo, "layer.txt"), "base\n")
			},
			layerLabel: "=== INDEX (staged state) ===",
			wantText:   "+staged",
		},
		{
			name: "ordinary staged change is deduplicated",
			change: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "layer.txt"), "staged\n")
				gitCommand(t, repo, "add", "layer.txt")
			},
			layerLabel: "=== INDEX + EFFECTIVE WORKTREE (identical) ===",
			wantText:   "+staged",
		},
		{
			name: "unstaged change",
			change: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "layer.txt"), "unstaged\n")
			},
			layerLabel: "=== EFFECTIVE WORKTREE (tracked and untracked content) ===",
			wantText:   "+unstaged",
		},
		{
			name: "untracked file",
			change: func(t *testing.T, repo string) {
				writeFile(t, filepath.Join(repo, "untracked-layer.txt"), "untracked\n")
			},
			layerLabel: "=== EFFECTIVE WORKTREE (tracked and untracked content) ===",
			wantText:   "untracked-layer.txt",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepo(t)
			runDirectory := filepath.Join(repo, ".scratch", "feature")
			writeFile(t, filepath.Join(runDirectory, "PRD.md"), "# PRD\n")
			writeFile(t, filepath.Join(repo, "layer.txt"), "base\n")
			initializeGitRepo(t, repo)
			baseline, err := CaptureWorkspaceBaseline(context.Background(), repo, runDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer baseline.Cleanup()
			if baseline.IndexTree == "" || baseline.WorktreeTree == "" {
				t.Fatal("run start did not capture both workspace layers")
			}

			test.change(t, repo)
			delta, err := baseline.PrepareDelta(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(delta.Path)
			if err != nil {
				t.Fatal(err)
			}
			patch := string(content)
			if !strings.Contains(patch, test.layerLabel) || !strings.Contains(patch, test.wantText) {
				t.Fatalf("layered delta missing %q or %q:\n%s", test.layerLabel, test.wantText, patch)
			}
			if count := strings.Count(patch, "diff --git "); count != 1 {
				t.Fatalf("delta contains %d file patches, want one without duplicate ambiguity:\n%s", count, patch)
			}
		})
	}
}

func TestWorkspaceSnapshotsKeepUntrackedObjectsPrivate(t *testing.T) {
	repo := testRepo(t)
	runDirectory := filepath.Join(repo, ".scratch", "feature")
	writeFile(t, filepath.Join(runDirectory, "PRD.md"), "# PRD\n")
	initializeGitRepo(t, repo)
	preexisting := filepath.Join(repo, "private-preexisting.txt")
	writeFile(t, preexisting, "private baseline content\n")
	preexistingHash := strings.TrimSpace(gitCommand(t, repo, "hash-object", preexisting))

	baseline, err := CaptureWorkspaceBaseline(context.Background(), repo, runDirectory)
	if err != nil {
		t.Fatal(err)
	}
	state := baseline.StateDirectory
	current := filepath.Join(repo, "private-current.txt")
	writeFile(t, current, "private current content\n")
	currentHash := strings.TrimSpace(gitCommand(t, repo, "hash-object", current))
	if _, err := baseline.PrepareDelta(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{preexistingHash, currentHash} {
		command := exec.Command("git", "-C", repo, "cat-file", "-e", hash)
		if err := command.Run(); err == nil {
			t.Fatalf("untracked blob %s leaked into the repository object database", hash)
		}
	}
	if _, err := os.Stat(baseline.ObjectDirectory); err != nil {
		t.Fatalf("private object directory missing before cleanup: %v", err)
	}
	if err := baseline.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("isolated state survived cleanup: %v", err)
	}
}

func TestConcurrentWorkspaceBaselinesUseIsolatedState(t *testing.T) {
	repo := testRepo(t)
	runDirectory := filepath.Join(repo, ".scratch", "feature")
	writeFile(t, filepath.Join(runDirectory, "PRD.md"), "# PRD\n")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "base\n")
	initializeGitRepo(t, repo)

	results := make(chan WorkspaceBaseline, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			baseline, err := CaptureWorkspaceBaseline(context.Background(), repo, runDirectory)
			if err != nil {
				errs <- err
				return
			}
			results <- baseline
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var baselines []WorkspaceBaseline
	for baseline := range results {
		baselines = append(baselines, baseline)
	}
	if len(baselines) != 2 || pathsEqual(baselines[0].StateDirectory, baselines[1].StateDirectory) {
		t.Fatalf("workspace states are not isolated: %+v", baselines)
	}
	if pathsEqual(baselines[0].ObjectDirectory, baselines[1].ObjectDirectory) {
		t.Fatal("concurrent runs share a private object directory")
	}
	firstMessage, err := (MessageStore{Root: repo, StateDirectory: baselines[0].StateDirectory}).PrepareMessageFile(MessagePurposeDevelopmentProgress)
	if err != nil {
		t.Fatal(err)
	}
	secondMessage, err := (MessageStore{Root: repo, StateDirectory: baselines[1].StateDirectory}).PrepareMessageFile(MessagePurposeDevelopmentProgress)
	if err != nil {
		t.Fatal(err)
	}
	if pathsEqual(firstMessage, secondMessage) {
		t.Fatal("concurrent runs share a message transport file")
	}
	writeFile(t, firstMessage, "first")
	writeFile(t, secondMessage, "second")
	writeFile(t, filepath.Join(repo, "tracked.txt"), "changed\n")
	firstDelta, err := baselines[0].PrepareDelta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondDelta, err := baselines[1].PrepareDelta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pathsEqual(firstDelta.Path, secondDelta.Path) {
		t.Fatal("concurrent runs share a review patch")
	}
	firstContent, _ := os.ReadFile(firstDelta.Path)
	secondContent, _ := os.ReadFile(secondDelta.Path)
	if !bytes.Equal(firstContent, secondContent) {
		t.Fatalf("isolated runs observed different workspace changes")
	}
	if err := baselines[0].Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secondDelta.Path); err != nil {
		t.Fatalf("one run cleaned another run's state: %v", err)
	}
	if content, err := os.ReadFile(secondMessage); err != nil || string(content) != "second" {
		t.Fatalf("one run changed another run's message transport: content=%q err=%v", content, err)
	}
	if _, err := baselines[1].PrepareDelta(context.Background()); err != nil {
		t.Fatalf("remaining run became unusable after peer cleanup: %v", err)
	}
	if err := baselines[1].Cleanup(); err != nil {
		t.Fatal(err)
	}
}
