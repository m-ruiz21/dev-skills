package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type target struct {
	os   string
	arch string
}

type environmentSetting struct {
	key, value string
}

type packageOutputKind uint8

const (
	packageOutputRepository packageOutputKind = iota
	packageOutputTemporary
)

const (
	packageOutputMarker = ".task-loop-package-output"
	packageOutputPrefix = ".task-loop-package-"
	pinnedGoToolchain   = "go1.23.12"
)

type packageOutputDirectory struct {
	path           string
	repositoryRoot string
	kind           packageOutputKind
}

var targets = []target{
	{os: "darwin", arch: "amd64"},
	{os: "darwin", arch: "arm64"},
	{os: "linux", arch: "amd64"},
	{os: "linux", arch: "arm64"},
	{os: "windows", arch: "amd64"},
	{os: "windows", arch: "arm64"},
}

func main() {
	goCommand := flag.String("go", "go", "Go command used to build the binaries")
	output := flag.String("output", "../../bin/task-loop", "package output directory")
	verify := flag.Bool("verify", false, "verify the packaged files and checksums")
	smoke := flag.Bool("smoke", false, "smoke-test the packaged native binary")
	reproducible := flag.Bool("reproducible", false, "build the package twice and verify byte-for-byte reproducibility")
	tracked := flag.Bool("tracked", false, "verify every packaged file is tracked and unchanged")
	flag.Parse()

	outputDirectory, err := parsePackageOutputDirectory(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "package-task-loop:", err)
		os.Exit(1)
	}
	switch {
	case selectedModes(*verify, *smoke, *reproducible, *tracked) > 1:
		err = errors.New("-verify, -smoke, -reproducible, and -tracked are mutually exclusive")
	case *verify:
		err = verifyPackage(outputDirectory)
	case *smoke:
		err = smokePackage(outputDirectory)
	case *reproducible:
		err = verifyReproduciblePackage(*goCommand, outputDirectory)
	case *tracked:
		err = verifyTrackedPackage(outputDirectory)
	default:
		err = buildPackage(*goCommand, outputDirectory)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "package-task-loop:", err)
		os.Exit(1)
	}
}

func selectedModes(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func parsePackageOutputDirectory(value string) (packageOutputDirectory, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return packageOutputDirectory{}, fmt.Errorf("resolve current working directory: %w", err)
	}
	rootText, err := runGitCommand(workingDirectory, "rev-parse", "--show-toplevel")
	if err != nil {
		return packageOutputDirectory{}, err
	}
	return parsePackageOutputDirectoryAt(value, workingDirectory, strings.TrimSpace(rootText))
}

func parsePackageOutputDirectoryAt(value, workingDirectory, repositoryRoot string) (packageOutputDirectory, error) {
	root, err := canonicalExistingDirectory(repositoryRoot)
	if err != nil {
		return packageOutputDirectory{}, fmt.Errorf("resolve repository root: %w", err)
	}
	working, err := canonicalExistingDirectory(workingDirectory)
	if err != nil {
		return packageOutputDirectory{}, fmt.Errorf("resolve current working directory: %w", err)
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(working, candidate)
	}
	lexical, err := filepath.Abs(candidate)
	if err != nil {
		return packageOutputDirectory{}, fmt.Errorf("resolve package output: %w", err)
	}
	lexical = filepath.Clean(lexical)
	resolved, err := canonicalPotentialPath(lexical)
	if err != nil {
		return packageOutputDirectory{}, fmt.Errorf("resolve package output: %w", err)
	}
	switch {
	case filepath.Dir(resolved) == resolved:
		return packageOutputDirectory{}, fmt.Errorf("package output must not be a filesystem root: %s", value)
	case pathsEqual(resolved, working):
		return packageOutputDirectory{}, fmt.Errorf("package output must not be the current working directory: %s", value)
	case pathContains(resolved, root):
		return packageOutputDirectory{}, fmt.Errorf("package output must not contain the repository: %s", value)
	}

	defaultOutput := filepath.Join(root, "bin", "task-loop")
	if pathsEqual(lexical, defaultOutput) {
		if !pathsEqual(resolved, lexical) || !pathContains(root, resolved) {
			return packageOutputDirectory{}, fmt.Errorf("repository package output must not contain filesystem redirects: %s", value)
		}
		return packageOutputDirectory{path: resolved, repositoryRoot: root, kind: packageOutputRepository}, nil
	}
	if err := validateTemporaryPackageOutput(resolved); err != nil {
		return packageOutputDirectory{}, fmt.Errorf("package output is not a dedicated task-loop package directory: %s: %w", value, err)
	}
	return packageOutputDirectory{path: resolved, repositoryRoot: root, kind: packageOutputTemporary}, nil
}

func canonicalExistingDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return filepath.Clean(resolved), nil
}

func canonicalPotentialPath(path string) (string, error) {
	missing := make([]string, 0)
	current := filepath.Clean(path)
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			current = resolved
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	for index := len(missing) - 1; index >= 0; index-- {
		current = filepath.Join(current, missing[index])
	}
	return filepath.Clean(current), nil
}

func validateTemporaryPackageOutput(path string) error {
	if !strings.HasPrefix(filepath.Base(path), packageOutputPrefix) {
		return errors.New("temporary output name is not reserved for the packager")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("temporary output must already exist and carry the packager marker")
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("temporary output must be a physical directory")
	}
	marker := filepath.Join(path, packageOutputMarker)
	markerInfo, err := os.Lstat(marker)
	if err != nil || !markerInfo.Mode().IsRegular() {
		return errors.New("temporary output marker is missing")
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != packageOutputMarker+"\n" {
		return errors.New("temporary output marker is invalid")
	}
	return nil
}

func markTemporaryPackageOutput(path string) error {
	marker := filepath.Join(path, packageOutputMarker)
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(packageOutputMarker + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func pathsEqual(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathContains(base, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(base), filepath.Clean(candidate))
	return err == nil &&
		relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

func (output packageOutputDirectory) clearKnownChildren() error {
	if err := os.MkdirAll(output.path, 0o755); err != nil {
		return err
	}
	resolved, err := canonicalExistingDirectory(output.path)
	if err != nil {
		return err
	}
	if !pathsEqual(resolved, output.path) {
		return errors.New("package output became a filesystem redirect")
	}
	known := map[string]bool{"SHA256SUMS": true}
	if output.kind == packageOutputTemporary {
		known[packageOutputMarker] = true
	}
	for _, target := range targets {
		known[target.os+"-"+target.arch] = true
	}
	entries, err := os.ReadDir(output.path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !known[entry.Name()] {
			return fmt.Errorf("unexpected file or directory %s", entry.Name())
		}
	}
	for _, target := range targets {
		child := filepath.Join(output.path, target.os+"-"+target.arch)
		if err := removeKnownPackageChild(output.path, child); err != nil {
			return err
		}
	}
	checksums := filepath.Join(output.path, "SHA256SUMS")
	if err := os.Remove(checksums); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeKnownPackageChild(output, child string) error {
	if _, err := os.Lstat(child); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(child)
	if err != nil {
		return err
	}
	if !pathContains(output, resolved) {
		return fmt.Errorf("package child is a filesystem redirect outside the output: %s", child)
	}
	return os.RemoveAll(child)
}

func buildPackage(goCommand string, output packageOutputDirectory) error {
	if err := output.clearKnownChildren(); err != nil {
		return fmt.Errorf("clear output: %w", err)
	}
	for _, target := range targets {
		path := binaryPath(output.path, target)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}
		command := exec.Command(
			goCommand,
			"build",
			"-mod=readonly",
			"-trimpath",
			"-buildvcs=false",
			"-ldflags=-s -w -buildid=",
			"-o", path,
			"./cmd/task-loop",
		)
		command.Env = targetEnvironment(target)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("build %s-%s: %w", target.os, target.arch, err)
		}
		if err := verifyBinaryCompilerVersion(path, pinnedGoToolchain); err != nil {
			return fmt.Errorf("verify %s-%s compiler: %w", target.os, target.arch, err)
		}
		if err := os.Chmod(path, packagedFileMode(target)); err != nil {
			return fmt.Errorf("set packaged mode for %s: %w", path, err)
		}
	}
	return writeChecksums(output.path)
}

func targetEnvironment(target target) []string {
	environment := os.Environ()
	settings := []environmentSetting{
		{"CGO_ENABLED", "0"},
		{"GO111MODULE", "on"},
		{"GOARCH", target.arch},
		{"GOENV", "off"},
		{"GOEXPERIMENT", ""},
		{"GOFLAGS", ""},
		{"GOOS", target.os},
		{"GOTOOLCHAIN", pinnedGoToolchain},
		{"GOWORK", "off"},
	}
	environment = removeEnvironment(
		environment,
		"GO386",
		"GOAMD64",
		"GOARM",
		"GOARM64",
		"GOFIPS140",
		"GOMIPS",
		"GOMIPS64",
		"GOPPC64",
		"GORISCV64",
		"GOWASM",
	)
	switch target.arch {
	case "amd64":
		settings = append(settings, environmentSetting{"GOAMD64", "v1"})
	case "arm64":
		settings = append(settings, environmentSetting{"GOARM64", "v8.0"})
	}
	for _, setting := range settings {
		environment = replaceEnvironment(environment, setting.key, setting.value)
	}
	return environment
}

func replaceEnvironment(environment []string, key, value string) []string {
	environment = removeEnvironment(environment, key)
	return append(environment, key+"="+value)
}

func removeEnvironment(environment []string, keys ...string) []string {
	prefixes := make([]string, len(keys))
	for index, key := range keys {
		prefixes[index] = strings.ToUpper(key) + "="
	}
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		upper := strings.ToUpper(entry)
		remove := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(upper, prefix) {
				remove = true
				break
			}
		}
		if !remove {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func packagedFileMode(target target) os.FileMode {
	if target.os == "windows" {
		return 0o644
	}
	return 0o755
}

func binaryPath(output string, target target) string {
	name := "task-loop"
	if target.os == "windows" {
		name += ".exe"
	}
	return filepath.Join(output, target.os+"-"+target.arch, name)
}

func writeChecksums(output string) error {
	var contents strings.Builder
	for _, target := range targets {
		path := binaryPath(output, target)
		checksum, err := fileChecksum(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(output, path)
		if err != nil {
			return fmt.Errorf("make checksum path relative: %w", err)
		}
		fmt.Fprintf(&contents, "%s  %s\n", checksum, filepath.ToSlash(relative))
	}
	path := filepath.Join(output, "SHA256SUMS")
	if err := os.WriteFile(path, []byte(contents.String()), 0o644); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	return nil
}

func fileChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyPackage(output packageOutputDirectory) error {
	expected := make(map[string]bool, len(targets)+1)
	for _, target := range targets {
		relative, err := filepath.Rel(output.path, binaryPath(output.path, target))
		if err != nil {
			return fmt.Errorf("make target path relative: %w", err)
		}

		expected[filepath.Clean(relative)] = true
	}
	expected["SHA256SUMS"] = true
	if output.kind == packageOutputTemporary {
		expected[packageOutputMarker] = true
	}

	var actual []string
	err := filepath.WalkDir(output.path, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(output.path, path)
		if err != nil {
			return err
		}
		actual = append(actual, filepath.Clean(relative))
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect package: %w", err)
	}
	sort.Strings(actual)
	for _, path := range actual {
		if !expected[path] {
			return fmt.Errorf("unexpected packaged file %s", filepath.ToSlash(path))
		}
		delete(expected, path)
	}
	if len(expected) != 0 {
		missing := make([]string, 0, len(expected))
		for path := range expected {
			missing = append(missing, filepath.ToSlash(path))
		}
		sort.Strings(missing)
		return fmt.Errorf("missing packaged files: %s", strings.Join(missing, ", "))
	}
	if err := verifyChecksums(output.path); err != nil {
		return err
	}
	if err := verifyPackageCompilerVersions(output.path); err != nil {
		return err
	}
	return verifyPackageModes(output.path)
}

func verifyPackageCompilerVersions(output string) error {
	for _, target := range targets {
		if err := verifyBinaryCompilerVersion(binaryPath(output, target), pinnedGoToolchain); err != nil {
			return fmt.Errorf("%s-%s: %w", target.os, target.arch, err)
		}
	}
	return nil
}

func verifyBinaryCompilerVersion(path, expected string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Go build info for %s: %w", filepath.ToSlash(path), err)
	}
	if info.GoVersion != expected {
		return fmt.Errorf("compiler mismatch for %s: got %s, want %s", filepath.ToSlash(path), info.GoVersion, expected)
	}
	return nil
}

func verifyReproduciblePackage(goCommand string, output packageOutputDirectory) error {
	parent := filepath.Dir(output.path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create reproducibility directory: %w", err)
	}
	first, err := newTemporaryPackageOutput(output, parent, "repro-first")
	if err != nil {
		return fmt.Errorf("create first reproducibility directory: %w", err)
	}
	defer first.removeTemporary()
	second, err := newTemporaryPackageOutput(output, parent, "repro-second")
	if err != nil {
		return fmt.Errorf("create second reproducibility directory: %w", err)
	}
	defer second.removeTemporary()
	if err := buildPackage(goCommand, first); err != nil {
		return fmt.Errorf("first reproducibility build: %w", err)
	}
	if err := buildPackage(goCommand, second); err != nil {
		return fmt.Errorf("second reproducibility build: %w", err)
	}
	if err := verifyPackage(first); err != nil {
		return fmt.Errorf("verify first reproducibility build: %w", err)
	}
	if err := verifyPackage(second); err != nil {
		return fmt.Errorf("verify second reproducibility build: %w", err)
	}
	for _, relative := range packagedFiles() {
		firstChecksum, err := fileChecksum(filepath.Join(first.path, relative))
		if err != nil {
			return err
		}
		secondChecksum, err := fileChecksum(filepath.Join(second.path, relative))
		if err != nil {
			return err
		}
		if firstChecksum != secondChecksum {
			return fmt.Errorf("non-reproducible packaged file %s", filepath.ToSlash(relative))
		}
	}
	return nil
}

func newTemporaryPackageOutput(base packageOutputDirectory, parent, purpose string) (packageOutputDirectory, error) {
	path, err := os.MkdirTemp(parent, packageOutputPrefix+purpose+"-")
	if err != nil {
		return packageOutputDirectory{}, err
	}
	if err := markTemporaryPackageOutput(path); err != nil {
		_ = os.Remove(path)
		return packageOutputDirectory{}, err
	}
	output, err := parsePackageOutputDirectoryAt(path, parent, base.repositoryRoot)
	if err != nil {
		_ = os.Remove(filepath.Join(path, packageOutputMarker))
		_ = os.Remove(path)
		return packageOutputDirectory{}, err
	}
	return output, nil
}

func (output packageOutputDirectory) removeTemporary() {
	if output.kind != packageOutputTemporary {
		return
	}
	if err := output.clearKnownChildren(); err != nil {
		return
	}
	if err := os.Remove(filepath.Join(output.path, packageOutputMarker)); err != nil {
		return
	}
	_ = os.Remove(output.path)
}

func packagedFiles() []string {
	files := make([]string, 0, len(targets)+1)
	for _, target := range targets {
		files = append(files, filepath.Join(target.os+"-"+target.arch, filepath.Base(binaryPath("", target))))
	}
	return append(files, "SHA256SUMS")
}

func packagedGitMode(relative string) (string, bool) {
	if filepath.Clean(relative) == "SHA256SUMS" {
		return "100644", true
	}
	for _, target := range targets {
		expected := filepath.Join(target.os+"-"+target.arch, filepath.Base(binaryPath("", target)))
		if filepath.Clean(relative) == filepath.Clean(expected) {
			if target.os == "windows" {
				return "100644", true
			}
			return "100755", true
		}
	}
	return "", false
}

func verifyTrackedPackage(output packageOutputDirectory) error {
	outputRelative, err := filepath.Rel(output.repositoryRoot, output.path)
	if err != nil || outputRelative == ".." || strings.HasPrefix(outputRelative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("package output must stay within the Git working tree: %s", output.path)
	}

	problems := make([]string, 0)
	for _, relative := range packagedFiles() {
		repositoryPath := filepath.ToSlash(filepath.Join(outputRelative, relative))
		entry, entryErr := runGitCommand(output.repositoryRoot, "ls-files", "--error-unmatch", "--stage", "--", repositoryPath)
		if entryErr != nil {
			problems = append(problems, "expected package path is not tracked: "+repositoryPath)
			continue
		}
		fields := strings.Fields(entry)
		expectedMode, ok := packagedGitMode(relative)
		if !ok {
			problems = append(problems, "expected package path has no mode contract: "+repositoryPath)
			continue
		}
		if len(fields) == 0 || fields[0] != expectedMode {
			actual := "<missing>"
			if len(fields) > 0 {
				actual = fields[0]
			}
			problems = append(problems, fmt.Sprintf("tracked mode mismatch for %s: got %s, want %s", repositoryPath, actual, expectedMode))
		}
	}
	status, err := runGitCommand(output.repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all", "--", filepath.ToSlash(outputRelative))
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		problems = append(problems, "package has tracked, untracked, or mode changes:\n"+strings.TrimRight(status, "\r\n"))
	}
	if len(problems) != 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func runGitCommand(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	if directory != "" {
		command.Dir = directory
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s failed: %w: %s", arguments[0], err, detail)
	}
	return stdout.String(), nil
}

func verifyPackageModes(output string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for _, target := range targets {
		info, err := os.Stat(binaryPath(output, target))
		if err != nil {
			return fmt.Errorf("inspect packaged mode: %w", err)
		}
		if actual, expected := info.Mode().Perm(), packagedFileMode(target); actual != expected {
			return fmt.Errorf("mode mismatch for %s: got %04o, want %04o", filepath.ToSlash(binaryPath(output, target)), actual, expected)
		}
	}
	checksums, err := os.Stat(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		return fmt.Errorf("inspect checksum mode: %w", err)
	}
	if actual := checksums.Mode().Perm(); actual != 0o644 {
		return fmt.Errorf("mode mismatch for SHA256SUMS: got %04o, want 0644", actual)
	}
	return nil
}

func verifyChecksums(output string) error {
	file, err := os.Open(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		return fmt.Errorf("open checksums: %w", err)
	}
	defer file.Close()

	seen := make(map[string]bool, len(targets))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		relative := filepath.Clean(filepath.FromSlash(fields[1]))
		if seen[relative] {
			return fmt.Errorf("duplicate checksum for %s", fields[1])
		}
		seen[relative] = true
		actual, err := fileChecksum(filepath.Join(output, relative))
		if err != nil {
			return err
		}
		if actual != fields[0] {
			return fmt.Errorf("checksum mismatch for %s", fields[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}
	if len(seen) != len(targets) {
		return fmt.Errorf("checksums contain %d entries, want %d", len(seen), len(targets))
	}
	for _, target := range targets {
		relative, err := filepath.Rel(output, binaryPath(output, target))
		if err != nil {
			return fmt.Errorf("make checksum target relative: %w", err)
		}
		if !seen[filepath.Clean(relative)] {
			return fmt.Errorf("missing checksum for %s", filepath.ToSlash(relative))
		}
	}
	return nil
}

func smokePackage(output packageOutputDirectory) error {
	native := target{os: runtime.GOOS, arch: runtime.GOARCH}
	if !supported(native) {
		return fmt.Errorf("unsupported native platform %s-%s", native.os, native.arch)
	}
	binary, err := filepath.Abs(binaryPath(output.path, native))
	if err != nil {
		return fmt.Errorf("resolve native binary: %w", err)
	}
	help := exec.Command(binary, "--help")
	helpOutput, err := help.CombinedOutput()
	if err != nil {
		return fmt.Errorf("--help failed: %w\n%s", err, helpOutput)
	}
	if !bytes.Contains(helpOutput, []byte("usage: task-loop")) ||
		!bytes.Contains(helpOutput, []byte("create-issue -request-file FILE")) {
		return fmt.Errorf("--help returned unexpected output %q", helpOutput)
	}

	scratchRoot := filepath.Join(output.repositoryRoot, "cmd", "task_loop", "build")
	if err := os.MkdirAll(scratchRoot, 0o755); err != nil {
		return fmt.Errorf("create smoke root: %w", err)
	}
	scratch, err := os.MkdirTemp(scratchRoot, ".task-loop-smoke-")
	if err != nil {
		return fmt.Errorf("create smoke directory: %w", err)
	}
	defer removeSmokeDirectory(scratch)
	feature := filepath.Join(scratch, ".scratch", "smoke")
	if err := os.MkdirAll(feature, 0o755); err != nil {
		return fmt.Errorf("create smoke feature: %w", err)
	}
	if err := os.WriteFile(filepath.Join(feature, "PRD.md"), []byte("# Smoke PRD\n"), 0o600); err != nil {
		return fmt.Errorf("write smoke PRD: %w", err)
	}
	requestPath := filepath.Join(feature, "request.json")
	request := `{"prd":".scratch/smoke/PRD.md","title":"Package smoke issue","description":"Exercise packaged request transport.","acceptanceCriteria":["The packaged binary creates the issue."]}`
	if err := os.WriteFile(requestPath, []byte(request), 0o600); err != nil {
		return fmt.Errorf("write smoke request: %w", err)
	}
	createIssue := exec.Command(binary, "create-issue", "-request-file", requestPath)
	createIssue.Dir = scratch
	createIssueOutput, err := createIssue.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create-issue failed: %w\n%s", err, createIssueOutput)
	}
	if string(createIssueOutput) != "Created issue: .scratch/smoke/issues/01-package-smoke-issue.md\n" {
		return fmt.Errorf("create-issue returned unexpected output %q", createIssueOutput)
	}

	message := exec.Command(binary, "add-message", "-file", "progress.txt", "-message", "Packaging smoke test.", "-from", "developer")
	message.Dir = scratch
	messageOutput, err := message.CombinedOutput()
	if err != nil {
		return fmt.Errorf("add-message failed: %w\n%s", err, messageOutput)
	}
	if string(messageOutput) != "Thread 1\n" {
		return fmt.Errorf("add-message returned unexpected output %q", messageOutput)
	}
	content, err := os.ReadFile(filepath.Join(scratch, "progress.txt"))
	if err != nil {
		return fmt.Errorf("read smoke message: %w", err)
	}
	if !bytes.Contains(content, []byte("Packaging smoke test.")) {
		return errors.New("add-message did not persist its message")
	}
	return nil
}

func removeSmokeDirectory(path string) {
	_ = os.RemoveAll(path)
}

func supported(candidate target) bool {
	for _, target := range targets {
		if candidate == target {
			return true
		}
	}
	return false
}
