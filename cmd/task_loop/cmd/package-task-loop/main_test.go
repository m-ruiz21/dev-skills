package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTargetEnvironmentClearsHostBuildTuningAndPinsArchitectureBaseline(t *testing.T) {
	for _, key := range []string{"GOFLAGS", "GOEXPERIMENT", "GO386", "GOAMD64", "GOARM", "GOARM64", "GOFIPS140", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM", "GOENV", "GOTOOLCHAIN", "GOWORK"} {
		t.Setenv(key, "host-specific-value")
	}
	tests := []struct {
		target target
		pinned map[string]string
		absent []string
	}{
		{
			target: target{os: "linux", arch: "amd64"},
			pinned: map[string]string{"CGO_ENABLED": "0", "GO111MODULE": "on", "GOOS": "linux", "GOARCH": "amd64", "GOAMD64": "v1", "GOFLAGS": "", "GOEXPERIMENT": "", "GOENV": "off", "GOTOOLCHAIN": pinnedGoToolchain, "GOWORK": "off"},
			absent: []string{"GO386", "GOARM", "GOARM64", "GOFIPS140", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM"},
		},
		{
			target: target{os: "windows", arch: "arm64"},
			pinned: map[string]string{"CGO_ENABLED": "0", "GO111MODULE": "on", "GOOS": "windows", "GOARCH": "arm64", "GOARM64": "v8.0", "GOFLAGS": "", "GOEXPERIMENT": "", "GOENV": "off", "GOTOOLCHAIN": pinnedGoToolchain, "GOWORK": "off"},
			absent: []string{"GO386", "GOAMD64", "GOARM", "GOFIPS140", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM"},
		},
	}
	for _, test := range tests {
		t.Run(test.target.os+"-"+test.target.arch, func(t *testing.T) {
			actual := environmentMap(targetEnvironment(test.target))
			for key, expected := range test.pinned {
				if actual[key] != expected {
					t.Errorf("%s=%q, want %q", key, actual[key], expected)
				}
			}
			for _, key := range test.absent {
				if _, ok := actual[key]; ok {
					t.Errorf("%s was inherited", key)
				}
			}
		})
	}
}

func TestBinaryCompilerVersionVerification(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyBinaryCompilerVersion(executable, runtime.Version()); err != nil {
		t.Fatalf("current test binary rejected: %v", err)
	}
	if err := verifyBinaryCompilerVersion(executable, "go0.0.0"); err == nil || !strings.Contains(err.Error(), "compiler mismatch") {
		t.Fatalf("compiler mismatch accepted: %v", err)
	}
}

func TestPackagedFileModes(t *testing.T) {
	if packagedFileMode(target{os: "linux", arch: "amd64"}) != 0o755 {
		t.Fatal("Unix package mode is not executable")
	}
	if packagedFileMode(target{os: "windows", arch: "amd64"}) != 0o644 {
		t.Fatal("Windows package mode is not 0644")
	}
	if runtime.GOOS == "windows" {
		return
	}
	output := packageTestDirectory(t)
	for _, target := range targets {
		path := binaryPath(output.path, target)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("binary"), packagedFileMode(target)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(output.path, "SHA256SUMS"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackageModes(output.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binaryPath(output.path, target{os: "windows", arch: "amd64"}), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackageModes(output.path); err == nil {
		t.Fatal("mode verification accepted executable Windows binary")
	}
}

func TestPluginVersionsMatchMigrationRelease(t *testing.T) {
	root := repositoryRoot(t)
	var plugin struct {
		Version string `json:"version"`
	}
	readJSON(t, filepath.Join(root, ".claude-plugin", "plugin.json"), &plugin)
	var marketplace struct {
		Metadata struct {
			Version string `json:"version"`
		} `json:"metadata"`
		Plugins []struct {
			Version string `json:"version"`
		} `json:"plugins"`
	}
	readJSON(t, filepath.Join(root, ".github", "plugin", "marketplace.json"), &marketplace)
	if plugin.Version != "0.5.0" || marketplace.Metadata.Version != plugin.Version || len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Version != plugin.Version {
		t.Fatalf("plugin versions are not synchronized at 0.5.0: plugin=%q metadata=%q entries=%v", plugin.Version, marketplace.Metadata.Version, marketplace.Plugins)
	}
}

func TestAutomationSkillsRequireStructuredOrLiteralArgv(t *testing.T) {
	root := repositoryRoot(t)
	tests := []struct {
		path     string
		required []string
	}{
		{
			path: filepath.Join(root, "skills", "develop-task", "SKILL.md"),
			required: []string{
				"executable:",
				"arguments:",
				"workingDirectory:",
				"structured process data",
				`Replace("'", "''")`,
				`Replace("'", "'\"'\"'")`,
			},
		},
		{
			path: filepath.Join(root, "skills", "to-issues", "SKILL.md"),
			required: []string{
				"until the user explicitly",
				"Establish the canonical local PRD",
				"reuse it without rewriting it",
				"normalized Markdown is exactly the document that would be materialized",
				"Never overwrite, truncate, or silently repurpose",
				"`<slug>-2`, `<slug>-3`",
				"create-new/refuse-overwrite",
				"`## Source` section",
				"same reference as each generated issue's",
				"create an empty `progress.txt` only",
				"Before continuing, verify that the selected PRD now exists",
				"dependency order",
				"Created issue: <path>",
				`"prd":`,
				`"title":`,
				`"description":`,
				`"acceptanceCriteria":`,
				`"parent":`,
				`"blockedBy":`,
				`["create-issue", "-request-file"`,
				"structured process data",
				`Replace("'", "''")`,
				`Replace("'", "'\"'\"'")`,
			},
		},
		{
			path: filepath.Join(root, "skills", "setup-repo", "SKILL.md"),
			required: []string{
				"windows-amd64",
				"windows-arm64",
				"darwin-amd64",
				"darwin-arm64",
				"linux-amd64",
				"linux-arm64",
				`arguments:  ["--help"]`,
				"usage: task-loop",
				"structured process data",
				"Reinstall or update",
				"`to-issues` uses the tracker only to resolve an explicitly supplied source",
				"reuses or materializes `.scratch/<feature>/PRD.md`",
				"always creates local `.scratch/<feature>/issues/` through",
			},
		},
	}
	for _, test := range tests {
		t.Run(filepath.Base(filepath.Dir(test.path)), func(t *testing.T) {
			content, err := os.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(content)
			for _, forbidden := range []string{"```powershell", "```bash", "$taskLoop =", "task_loop="} {
				if strings.Contains(text, forbidden) {
					t.Errorf("automation skill still contains generated shell source marker %q", forbidden)
				}
			}
			for _, required := range test.required {
				if !strings.Contains(text, required) {
					t.Errorf("automation skill is missing safe invocation contract %q", required)
				}
			}
		})
	}
}

func TestToIssuesEstablishesPRDBeforeRequestFilePublication(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "skills", "to-issues", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	approval := strings.Index(text, "After approval, and before creating any request file")
	reuse := strings.Index(text, "reuse it without rewriting it")
	materialize := strings.Index(text, "Materialize every accepted source that was not reused in step 1")
	verify := strings.Index(text, "Before continuing, verify that the selected PRD now exists")
	request := strings.Index(text, "For every issue, choose one unique")
	if approval < 0 || reuse < approval || materialize < reuse || verify < materialize || request < verify {
		t.Fatalf("canonical PRD workflow must precede request-file publication: approval=%d reuse=%d materialize=%d verify=%d request=%d", approval, reuse, materialize, verify, request)
	}
	for _, required := range []string{
		"existing regular file",
		"matches exactly `.scratch/<feature>/PRD.md`",
		"first absent directory",
		"Materialize every accepted source that was not reused in step 1",
		"noncanonical local source",
		"local spec, issue, or plan file",
		"remote source issue",
		"Do not publish, close, label, or otherwise modify a remote source issue",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("canonical PRD workflow is missing %q", required)
		}
	}

	for _, convention := range []struct {
		path     string
		required []string
	}{
		{
			path: filepath.Join(root, "skills", "to-prd", "SKILL.md"),
			required: []string{
				"lowercase ASCII letters/numbers separated by single hyphens",
				"not a Windows reserved device name",
				"create-new/refuse-overwrite",
				"Never overwrite an unrelated existing PRD",
			},
		},
		{
			path: filepath.Join(root, "skills", "setup-repo", "issue-tracker-local.md"),
			required: []string{
				"Feature slugs contain lowercase ASCII letters and numbers",
				"create-new/refuse-overwrite",
				"reuse a PRD only when it is the same source",
				"status: ready-for-agent",
				"blocked-by: []",
				"## What to build",
				"## Acceptance criteria",
				"## Blocked by",
			},
		},
	} {
		content, err := os.ReadFile(convention.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range convention.required {
			if !strings.Contains(string(content), required) {
				t.Errorf("%s is missing canonical PRD convention %q", convention.path, required)
			}
		}
	}
}

func TestLocalIssueConventionsRequireCanonicalFrontmatter(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		filepath.Join("skills", "setup-repo", "issue-tracker-local.md"),
		filepath.Join("skills", "setup-repo", "issue-conventions.md"),
	} {
		path := filepath.Join(root, relative)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		for _, required := range []string{"status: ready-for-agent", "blocked-by:"} {
			if !strings.Contains(text, required) {
				t.Errorf("%s is missing canonical frontmatter field %q", relative, required)
			}
		}
		if strings.Contains(text, "Status: <value>") ||
			strings.Contains(text, "status field (or `Status:` line)") {
			t.Errorf("%s still permits incompatible body Status guidance", relative)
		}
	}
}

func TestTaskLoopWorkflowIncludesPackageContractPaths(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "task-loop.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, path := range []string{
		`      - "skills/setup-repo/**"`,
		`      - "skills/to-prd/SKILL.md"`,
	} {
		if count := strings.Count(text, path); count != 2 {
			t.Errorf("workflow path %q appears %d times, want once under pull_request and once under push", path, count)
		}
	}
}

func TestVerifyTrackedPackageRequiresExactTrackedSetAndModes(t *testing.T) {
	repo := plainTestDirectory(t)
	runGitTest(t, repo, "init", "--quiet")
	runGitTest(t, repo, "config", "user.name", "Task Loop Tests")
	runGitTest(t, repo, "config", "user.email", "task-loop@example.invalid")
	output := filepath.Join(repo, "bin", "task-loop")
	for _, relative := range packagedFiles() {
		path := filepath.Join(output, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitTest(t, repo, "add", "--", "bin/task-loop")
	for _, target := range targets {
		if target.os != "windows" {
			runGitTest(t, repo, "update-index", "--chmod=+x", "--", filepath.ToSlash(filepath.Join("bin", "task-loop", target.os+"-"+target.arch, "task-loop")))
		}
	}
	runGitTest(t, repo, "commit", "--quiet", "-m", "package")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	parsedOutput, err := parsePackageOutputDirectoryAt(output, repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyTrackedPackage(parsedOutput); err != nil {
		t.Fatalf("clean tracked package rejected: %v", err)
	}

	extra := filepath.Join(output, "extra.bin")
	if err := os.WriteFile(extra, []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyTrackedPackage(parsedOutput); err == nil || !strings.Contains(err.Error(), "?? bin/task-loop/extra.bin") {
		t.Fatalf("untracked package file accepted: %v", err)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}

	runGitTest(t, repo, "update-index", "--chmod=+x", "--", "bin/task-loop/SHA256SUMS")
	if err := verifyTrackedPackage(parsedOutput); err == nil || !strings.Contains(err.Error(), "mode mismatch") {
		t.Fatalf("wrong tracked mode accepted: %v", err)
	}
	runGitTest(t, repo, "reset", "--quiet", "--hard", "HEAD")
	runGitTest(t, repo, "rm", "--quiet", "--cached", "--", "bin/task-loop/SHA256SUMS")
	if err := verifyTrackedPackage(parsedOutput); err == nil || !strings.Contains(err.Error(), "is not tracked") {
		t.Fatalf("missing tracked package path accepted: %v", err)
	}
}

func TestPackageOutputRejectsDangerousAndUnmarkedDirectoriesWithoutDeletingThem(t *testing.T) {
	sandbox := plainTestDirectory(t)
	repo := filepath.Join(sandbox, "repository")
	working := filepath.Join(repo, "cmd", "task_loop")
	for _, directory := range []string{working, filepath.Join(repo, "cmd"), repo} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	unrelated := filepath.Join(sandbox, "unrelated")
	looksDedicated := filepath.Join(sandbox, packageOutputPrefix+"unmarked")
	for _, directory := range []string{working, filepath.Join(repo, "cmd"), repo, sandbox, unrelated, looksDedicated} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "must-survive"), []byte(directory), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	volumeRoot := filepath.VolumeName(repo) + string(filepath.Separator)
	tests := []struct {
		name, value string
	}{
		{name: "current directory", value: "."},
		{name: "parent", value: ".."},
		{name: "repository root", value: "../.."},
		{name: "repository ancestor", value: "../../.."},
		{name: "filesystem root", value: volumeRoot},
		{name: "unrelated populated directory", value: unrelated},
		{name: "unmarked reserved directory", value: looksDedicated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parsePackageOutputDirectoryAt(test.value, working, repo); err == nil {
				t.Fatalf("accepted dangerous output %q", test.value)
			}
		})
	}
	for _, directory := range []string{working, filepath.Join(repo, "cmd"), repo, sandbox, unrelated, looksDedicated} {
		if _, err := os.Stat(filepath.Join(directory, "must-survive")); err != nil {
			t.Fatalf("rejected output changed %s: %v", directory, err)
		}
	}
}

func TestPackageOutputAllowsOnlyDefaultOrMarkedTemporaryDirectories(t *testing.T) {
	sandbox := plainTestDirectory(t)
	repo := filepath.Join(sandbox, "repository")
	working := filepath.Join(repo, "cmd", "task_loop")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	defaultOutput, err := parsePackageOutputDirectoryAt(filepath.Join(repo, "bin", "task-loop"), working, repo)
	if err != nil || defaultOutput.kind != packageOutputRepository {
		t.Fatalf("default output rejected: output=%+v err=%v", defaultOutput, err)
	}

	temporaryPath, err := os.MkdirTemp(sandbox, packageOutputPrefix+"test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := markTemporaryPackageOutput(temporaryPath); err != nil {
		t.Fatal(err)
	}
	temporary, err := parsePackageOutputDirectoryAt(temporaryPath, working, repo)
	if err != nil || temporary.kind != packageOutputTemporary {
		t.Fatalf("marked temporary output rejected: output=%+v err=%v", temporary, err)
	}
	t.Cleanup(temporary.removeTemporary)
}

func TestPackageOutputRefusesUnknownContentsBeforeTargetedCleanup(t *testing.T) {
	sandbox := plainTestDirectory(t)
	repo := filepath.Join(sandbox, "repository")
	working := filepath.Join(repo, "cmd", "task_loop")
	outputPath := filepath.Join(repo, "bin", "task-loop")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(outputPath, "linux-amd64", "task-loop")
	unknown := filepath.Join(outputPath, "unrelated.txt")
	for path, content := range map[string]string{known: "known", unknown: "unrelated"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := parsePackageOutputDirectoryAt(outputPath, working, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := output.clearKnownChildren(); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("cleanup accepted unrelated contents: %v", err)
	}
	for _, path := range []string{known, unknown} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cleanup partially deleted %s: %v", path, err)
		}
	}
}

func TestRemoveSmokeDirectoryRemovesNestedSmokeRootOnly(t *testing.T) {
	parent := plainTestDirectory(t)
	smokeRoot, err := os.MkdirTemp(parent, ".task-loop-smoke-")
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(smokeRoot, ".scratch", "smoke", "issues")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "01-smoke.md"), []byte("smoke"), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(parent, "must-survive")
	if err := os.WriteFile(sibling, []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}

	removeSmokeDirectory(smokeRoot)

	if _, err := os.Stat(smokeRoot); !os.IsNotExist(err) {
		t.Fatalf("smoke root still exists: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("cleanup changed sibling: %v", err)
	}
}

func environmentMap(environment []string) map[string]string {
	result := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			result[strings.ToUpper(key)] = value
		}
	}
	return result
}

func packageTestDirectory(t *testing.T) packageOutputDirectory {
	t.Helper()
	root := filepath.Join(repositoryRoot(t), "cmd", "task_loop", "tests", "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, packageOutputPrefix+"test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := markTemporaryPackageOutput(directory); err != nil {
		t.Fatal(err)
	}
	output, err := parsePackageOutputDirectoryAt(directory, root, repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(output.removeTemporary)
	return output
}

func plainTestDirectory(t *testing.T) string {
	t.Helper()
	root := filepath.Join(repositoryRoot(t), "cmd", "task_loop", "tests", "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, "repository-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatal(err)
	}
}

func runGitTest(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", arguments, err, output)
	}
}
