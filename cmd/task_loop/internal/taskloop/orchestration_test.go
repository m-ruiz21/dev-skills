package taskloop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type scriptedInput struct {
	answer string
	calls  int
}

func (input *scriptedInput) Read(string) (string, bool) {
	input.calls++
	return input.answer, true
}

func TestIssueLoopRetriesSameIssueAcrossAllFailureCauses(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	initializeGitRepo(t, repo)
	baseline, err := CaptureWorkspaceBaseline(context.Background(), repo, filepath.Dir(prd))
	if err != nil {
		t.Fatal(err)
	}
	defer baseline.Cleanup()
	store := MessageStore{Root: repo, StateDirectory: baseline.StateDirectory}
	var stdout, stderr bytes.Buffer
	developmentCalls, testingCalls, reviewCalls := 0, 0, 0
	input := &scriptedInput{answer: "Use the standard library."}
	agents := Agents{
		Develop: func(_ context.Context, phase DevelopmentContext) (string, error) {
			developmentCalls++
			switch developmentCalls {
			case 1:
				return "[partial] First slice.", nil
			case 2:
				return "[needs-clarity] Which library?", nil
			default:
				if !strings.Contains(phase.Review, "Use the standard library.") {
					t.Error("clarity answer absent from retry context")
				}
				return fmt.Sprintf("[completed] Attempt %d.", developmentCalls), nil
			}
		},
		Test: func(_ context.Context, phase TestingContext) (string, error) {
			testingCalls++
			if testingCalls == 1 {
				if _, err := store.Add(phase.ReviewPath, "Broken fixture.", SenderReviewer); err != nil {
					return "", err
				}
				return "[failure]", nil
			}
			return "[success]", nil
		},
		Review: func(_ context.Context, phase ReviewContext) (string, error) {
			reviewCalls++
			if phase.DeltaPath == "" || !strings.Contains(phase.Instructions, "Do not use `git diff --staged`") {
				t.Errorf("review did not receive the explicit issue delta: %+v", phase)
			}
			if content, err := os.ReadFile(phase.DeltaPath); err != nil || !strings.Contains(string(content), "review/01.md") {
				t.Errorf("review delta is unavailable or incomplete: content=%q err=%v", content, err)
			}
			if reviewCalls == 1 {
				return failingReviewJSON(t), nil
			}
			return passingReviewJSON(t), nil
		},
		Stdin: input, Stdout: &stdout, Stderr: &stderr,
	}
	code := RunIssueLoop(context.Background(), repo, Run{RepositoryRoot: repo, RunDirectory: filepath.Dir(prd), PRDPath: prd, MaxIterations: 10, Baseline: baseline}, issue, "task-loop", store, agents)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if developmentCalls != 5 || testingCalls != 3 || reviewCalls != 2 || input.calls != 1 {
		t.Fatalf("calls develop=%d test=%d review=%d input=%d", developmentCalls, testingCalls, reviewCalls, input.calls)
	}
	for _, wanted := range []string{"Developer (partial)", "Developer needs clarity", "Tests: failure", "Overall: FAILED", "Overall: PASSED", "human review"} {
		if !strings.Contains(stdout.String(), wanted) {
			t.Errorf("stdout missing %q: %s", wanted, stdout.String())
		}
	}
	review := readOptional(filepath.Join(repo, "review", "01.md"))
	for _, wanted := range []string{"First slice.", "Use the standard library.", "Broken fixture.", "Critical issue."} {
		if !strings.Contains(review, wanted) {
			t.Errorf("review missing %q", wanted)
		}
	}
}

func TestIssueLoopUsesOneSharedBudgetAndDoesNotPromptAfterExhaustion(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	var stdout, stderr bytes.Buffer
	input := &scriptedInput{answer: "unused"}
	calls := 0
	agents := Agents{
		Develop: func(context.Context, DevelopmentContext) (string, error) {
			calls++
			if calls == 1 {
				return "[partial] First.", nil
			}
			return "[needs-clarity] Last question?", nil
		},
		Stdin: input, Stdout: &stdout, Stderr: &stderr,
	}
	code := RunIssueLoop(context.Background(), repo, Run{PRDPath: prd, MaxIterations: 2}, issue, "task-loop", testPhaseMessageStore(t, repo), agents)
	if code != 1 || calls != 2 || input.calls != 0 {
		t.Fatalf("exit=%d calls=%d prompts=%d", code, calls, input.calls)
	}
	if !strings.Contains(stderr.String(), issue) || !strings.Contains(stderr.String(), "Last question?") {
		t.Fatalf("non-actionable exhaustion: %s", stderr.String())
	}
}

func failingReviewJSON(t *testing.T) string {
	t.Helper()
	base := passingReviewJSON(t)
	return strings.Replace(base, `"findings":[]`, `"findings":[{"id":"SEC-1","dimension":"security","severity":"critical","status":"open","summary":"Critical issue."}]`, 1)
}
