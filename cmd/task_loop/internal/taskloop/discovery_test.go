package taskloop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMaxIterations(t *testing.T) {
	tests := []struct {
		value    string
		supplied bool
		want     int
		valid    bool
	}{
		{"", false, 10, true},
		{"5", true, 5, true},
		{"0", true, 0, false},
		{"-1", true, 0, false},
		{"1.5", true, 0, false},
		{"ten", true, 0, false},
	}
	for _, test := range tests {
		got, err := ParseMaxIterations(test.value, test.supplied)
		if test.valid && (err != nil || got != test.want) {
			t.Errorf("ParseMaxIterations(%q,%v)=(%d,%v)", test.value, test.supplied, got, err)
		}
		if !test.valid && err == nil {
			t.Errorf("accepted %q", test.value)
		}
	}
}

func TestStartRunValidatesPRDContract(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	run, err := StartRun(repo, prd, "", false)
	if err != nil || run.PRDPath != prd || run.MaxIterations != 10 {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	notPRD := filepath.Join(repo, "notes.md")
	writeFile(t, notPRD, "# notes")
	for _, path := range []string{filepath.Join(repo, "missing", "PRD.md"), notPRD, repo} {
		if _, err := StartRun(repo, path, "", false); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
}

func TestRunPathsRejectTraversalAndSymlinkEscapes(t *testing.T) {
	repo := testRepo(t)
	outside := testRepo(t)
	outsidePRD := writePRD(t, outside, "other")
	if _, err := StartRun(repo, outsidePRD, "", false); err == nil {
		t.Fatal("accepted PRD outside repository .scratch")
	}

	link := filepath.Join(repo, ".scratch", "feature", "PRD.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePRD, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := StartRun(repo, link, "", false); err == nil {
		t.Fatal("accepted PRD symlink escape")
	}
}

func TestDiscoverPRDsOnlyMatchesOneFeatureLevel(t *testing.T) {
	repo := testRepo(t)
	alpha := writePRD(t, repo, "alpha")
	zeta := writePRD(t, repo, "zeta")
	writeFile(t, filepath.Join(repo, ".scratch", "nested", "child", "PRD.md"), "# ignored")
	got, err := DiscoverPRDs(repo)
	if err != nil || len(got) != 2 || got[0] != alpha || got[1] != zeta {
		t.Fatalf("got=%v err=%v", got, err)
	}
}
