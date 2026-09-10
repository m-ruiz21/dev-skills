package taskloop

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSelectIssueStatusPriorityDependenciesAndReviewDoc(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	open := writeIssue(t, repo, "feature", "01-open.md", "ready-for-agent", nil, false)
	blocked := writeIssue(t, repo, "feature", "02-blocked.md", "review", []string{open}, false)
	selected, err := SelectIssue(repo, prd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if selected != open {
		t.Fatalf("selected %s, want %s", selected, open)
	}

	writeFile(t, open, strings.ReplaceAll(readOptional(open), "status: ready-for-agent", "status: closed"))
	selected, err = SelectIssue(repo, prd, nil)
	if err != nil {
		t.Fatal(err)
	}

	if selected != blocked {
		t.Fatalf("review status not prioritized: %s", selected)
	}
	if _, err := os.Stat(filepath.Join(repo, "review", "02-blocked.md")); err != nil {
		t.Fatal("review document not created")
	}
}

func TestSelectIssueResolvesMovedClosedDependency(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	original := filepath.Join(repo, ".scratch", "feature", "issues", "01-dep.md")
	writeIssue(t, repo, "feature", "01-dep.md", "closed", nil, true)
	dependent := writeIssue(t, repo, "feature", "02-dependent.md", "ready-for-agent", []string{original}, false)
	selected, err := SelectIssue(repo, prd, nil)
	if err != nil || selected != dependent {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestSelectIssueValidatesAgentResponse(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	issue := writeIssue(t, repo, "feature", "01.md", "ready-for-agent", nil, false)
	tests := []string{"", issue + "\n" + issue, filepath.Join(repo, "other.md")}
	for _, response := range tests {
		_, err := SelectIssue(repo, prd, func(TriageContext) (string, error) { return response, nil })
		if err == nil {
			t.Errorf("accepted %q", response)
		}
	}
}

func TestSelectIssueNoActionableIssue(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	writeIssue(t, repo, "feature", "01.md", "blocked", nil, false)
	if _, err := SelectIssue(repo, prd, nil); err == nil || !strings.Contains(err.Error(), "no actionable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelectIssueRejectsEscapingIssueAndDependencyPaths(t *testing.T) {
	t.Run("dependency traversal", func(t *testing.T) {
		repo := testRepo(t)
		prd := writePRD(t, repo, "feature")
		outside := filepath.Join(testRepo(t), "closed.md")
		writeFile(t, outside, "---\nstatus: closed\n---\n")
		writeIssue(t, repo, "feature", "01.md", "ready-for-agent", []string{outside}, false)
		if _, err := SelectIssue(repo, prd, nil); err == nil {
			t.Fatal("accepted dependency outside the run directory")
		}
	})
	t.Run("issue symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs elevated privileges on Windows")
		}
		repo := testRepo(t)
		prd := writePRD(t, repo, "feature")
		outside := filepath.Join(testRepo(t), "outside.md")
		writeFile(t, outside, "---\nstatus: ready-for-agent\n---\n")
		link := filepath.Join(repo, ".scratch", "feature", "issues", "01.md")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, err := SelectIssue(repo, prd, nil); err == nil {
			t.Fatal("accepted issue symlink escape")
		}
	})
}
