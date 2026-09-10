package taskloop

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDevelopmentResponseParsing(t *testing.T) {
	tests := []struct {
		response string
		outcome  DevelopmentOutcome
		message  string
		valid    bool
	}{
		{"[completed] Done.", DevelopmentCompleted, "Done.", true},
		{"[partial]\nProgress.", DevelopmentPartial, "Progress.", true},
		{"[needs-clarity] Which one?", DevelopmentNeedsClarity, "Which one?", true},
		{"[completed]", "", "", false},
		{"[completed]Done.", "", "", false},
		{"[completed][partial] Mixed.", "", "", false},
		{"Done [completed] now.", "", "", false},
		{"[bogus] no.", "", "", false},
	}
	for _, test := range tests {
		t.Run(test.response, func(t *testing.T) {
			outcome, message, err := parseDevelopmentResponse(test.response)
			if test.valid && (err != nil || outcome != test.outcome || message != test.message) {
				t.Fatalf("got (%q,%q,%v)", outcome, message, err)
			}
			if !test.valid && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTestingResponseParsing(t *testing.T) {
	for _, valid := range []string{"[success]", " \n[success]\t", "[failure]"} {
		if _, err := parseTestingResponse(valid); err != nil {
			t.Errorf("rejected %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "[success] ok", "[success][failure]", "[skipped]"} {
		if _, err := parseTestingResponse(invalid); err == nil {
			t.Errorf("accepted %q", invalid)
		}
	}
}

func TestPhaseContextsAndTestingFindingRequirement(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	progress := filepath.Join(repo, ".scratch", "feature", "progress.txt")
	writeFile(t, progress, "prior progress\n")
	review := filepath.Join(repo, "review", "01.md")
	writeFile(t, review, "-- Thread 1\n[user] - 2026-01-01T00:00:00Z\n\nPrior review.\n")
	store := testPhaseMessageStore(t, repo)

	var developmentContext DevelopmentContext
	result, err := RunDevelopment(context.Background(), repo, issue, prd, "C:\\tools\\task-loop.exe", store, func(_ context.Context, phase DevelopmentContext) (string, error) {
		developmentContext = phase
		return "[completed] Implemented.", nil
	})
	if err != nil || result.Outcome != DevelopmentCompleted {
		t.Fatalf("development: result=%+v err=%v", result, err)
	}
	if developmentContext.ReviewPath != filepath.Join("review", "01.md") {
		t.Fatalf("review path = %q, want working-directory-relative review path", developmentContext.ReviewPath)
	}
	for _, wanted := range []string{"# PRD", "## Work", "prior progress", "Prior review", TestFirstInstruction, NoDirectReviewEditsInstruction, "add-message"} {
		joined := developmentContext.PRD + developmentContext.Issue + developmentContext.Progress + developmentContext.Review + developmentContext.Instructions
		if !strings.Contains(joined, wanted) {
			t.Errorf("development context missing %q", wanted)
		}
	}

	_, err = RunTesting(context.Background(), repo, issue, prd, "task-loop", store, func(context.Context, TestingContext) (string, error) { return "[failure]", nil })
	if err == nil || !strings.Contains(err.Error(), "must append investigation findings") {
		t.Fatalf("missing finding error: %v", err)
	}
	testing, err := RunTesting(context.Background(), repo, issue, prd, "task-loop", store, func(_ context.Context, phase TestingContext) (string, error) {
		if strings.Count(phase.Instructions, "'-message-file'") != 2 {
			t.Errorf("testing instructions do not supply both exact commands:\n%s", phase.Instructions)
		}
		if strings.Contains(phase.Instructions, "<summary of files") ||
			strings.Contains(phase.Instructions, "<actionable test") ||
			!strings.Contains(phase.Instructions, "targets the review thread, not progress.txt") {
			t.Errorf("testing command semantics are ambiguous:\n%s", phase.Instructions)
		}
		if _, err := store.Add(phase.ReviewPath, "New investigation.", SenderReviewer); err != nil {
			return "", err
		}
		return "[failure]", nil
	})
	if err != nil || testing.Outcome != TestingFailure {
		t.Fatalf("testing: result=%+v err=%v", testing, err)
	}

}

func TestPhaseInputsRejectProgressAndReviewSymlinkEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by Windows-safe traversal tests; symlink creation needs elevated privileges")
	}
	t.Run("progress", func(t *testing.T) {
		repo := testRepo(t)
		prd := writePRD(t, repo, "feature")
		issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
		outside := filepath.Join(testRepo(t), "progress.txt")
		writeFile(t, outside, "restricted\n")
		if err := os.Symlink(outside, filepath.Join(repo, ".scratch", "feature", "progress.txt")); err != nil {
			t.Fatal(err)
		}
		if _, err := buildDevelopmentContext(repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo)); err == nil {
			t.Fatal("accepted progress symlink escape")
		}
	})
	t.Run("review", func(t *testing.T) {
		repo := testRepo(t)
		prd := writePRD(t, repo, "feature")
		issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
		outside := testRepo(t)
		if err := os.Symlink(outside, filepath.Join(repo, "review")); err != nil {
			t.Fatal(err)
		}
		if _, err := buildDevelopmentContext(repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo)); err == nil {
			t.Fatal("accepted review symlink escape")
		}
		if _, err := (MessageStore{Root: repo}).Add(filepath.Join("review", "01.md"), "unsafe", SenderDeveloper); err == nil {
			t.Fatal("appended through external root review symlink")
		}
	})
}

func TestPhaseInputsRejectReviewSymlinkRedirectWithinRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by Windows junction tests")
	}
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	internal := filepath.Join(repo, "internal-review-target")
	if err := os.MkdirAll(internal, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(internal, filepath.Join(repo, "review")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := buildDevelopmentContext(repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo)); err == nil {
		t.Fatal("accepted root review symlink redirected within the repository")
	}
	if _, err := (MessageStore{Root: repo}).Add(filepath.Join("review", "01.md"), "unsafe", SenderDeveloper); err == nil {
		t.Fatal("appended through root review symlink redirected within the repository")
	}
}

func TestReviewFileRejectsFinalSymlinkRedirects(t *testing.T) {
	tests := []struct {
		name   string
		target func(repo string) string
	}{
		{
			name: "internal git config",
			target: func(repo string) string {
				initializeGitRepo(t, repo)
				return filepath.Join(repo, ".git", "config")
			},
		},
		{
			name: "external",
			target: func(string) string {
				target := filepath.Join(testRepo(t), "review.md")
				writeFile(t, target, "external target")
				return target
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepo(t)
			prd := writePRD(t, repo, "feature")
			issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
			target := test.target(repo)
			review := filepath.Join(repo, "review", "01.md")
			if err := os.MkdirAll(filepath.Dir(review), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, review); err != nil {
				t.Skipf("file symlinks unavailable: %v", err)
			}
			before, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := buildDevelopmentContext(repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo)); err == nil {
				t.Fatal("read through final-component review symlink")
			}
			if _, err := (MessageStore{Root: repo}).Add(filepath.Join("review", "01.md"), "unsafe", SenderDeveloper); err == nil {
				t.Fatal("appended through final-component review symlink")
			}
			after, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("target changed: before=%q after=%q err=%v", before, after, err)
			}
		})
	}
}

func TestMalformedDevelopmentDoesNotTouchReview(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	review := filepath.Join(repo, "review", "01.md")
	writeFile(t, review, "")
	_, err := RunDevelopment(context.Background(), repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo), func(context.Context, DevelopmentContext) (string, error) {
		return "[completed] ", nil
	})
	if err == nil {
		t.Fatal("expected malformed response")
	}
	content, _ := os.ReadFile(review)
	if len(content) != 0 {
		t.Fatal("malformed response changed review")
	}
}

func TestAgentFailuresRemainDistinct(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	_, err := RunDevelopment(context.Background(), repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo), func(context.Context, DevelopmentContext) (string, error) {
		return "", errors.New("process failed")
	})
	var processError AgentProcessError
	if !errors.As(err, &processError) {
		t.Fatalf("got %T, want AgentProcessError", err)
	}
}

func TestIssueDeltaPathMustStayInRunStateDirectory(t *testing.T) {
	repo := testRepo(t)
	runDirectory := filepath.Join(repo, ".scratch", "feature")
	writeFile(t, filepath.Join(runDirectory, workspaceStateDirectory, "run-test", issueDeltaFilename), "safe")
	outside := filepath.Join(repo, "outside.diff")
	writeFile(t, outside, "restricted")
	if _, err := canonicalIssueDeltaPath(runDirectory, outside); err == nil {
		t.Fatal("accepted review delta outside run state directory")
	}
}

func TestPhaseInputsRejectWindowsReviewJunctionRedirects(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows reparse-point behavior")
	}
	for _, test := range []struct {
		name       string
		targetPath func(string) string
	}{
		{"external", func(string) string { return testRepo(t) }},
		{"internal", func(repo string) string {
			target := filepath.Join(repo, "internal-review-target")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			return target
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepo(t)
			prd := writePRD(t, repo, "feature")
			issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
			target := test.targetPath(repo)
			command := exec.Command(os.Getenv("ComSpec"), "/d", "/c", "mklink", "/J", filepath.Join(repo, "review"), target)
			if output, err := command.CombinedOutput(); err != nil {
				t.Skipf("junctions unavailable: %v: %s", err, output)
			}
			if _, err := buildDevelopmentContext(repo, issue, prd, "task-loop", testPhaseMessageStore(t, repo)); err == nil {
				t.Fatalf("accepted %s review junction redirect", test.name)
			}
			if _, err := (MessageStore{Root: repo}).Add(filepath.Join("review", "01.md"), "unsafe", SenderDeveloper); err == nil {
				t.Fatalf("appended through %s review junction redirect", test.name)
			}
		})
	}
}
