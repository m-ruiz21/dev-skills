package taskloop

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func canonicalRepositoryRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := canonicalExistingFilesystemPath(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory: %s", root)
	}
	return filepath.Clean(resolved), nil
}

func canonicalExistingPathWithin(base, candidate string) (string, error) {
	resolvedBase, err := canonicalExistingFilesystemPath(base)
	if err != nil {
		return "", fmt.Errorf("resolve confined directory %s: %w", base, err)
	}
	resolvedCandidate, err := canonicalExistingFilesystemPath(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve confined path %s: %w", candidate, err)
	}
	if !isPathWithin(resolvedBase, resolvedCandidate) {
		return "", fmt.Errorf("path must stay within %s: %s", resolvedBase, candidate)
	}
	return filepath.Clean(resolvedCandidate), nil
}

func canonicalPotentialPathWithin(base, candidate string) (string, error) {
	resolvedBase, err := canonicalExistingFilesystemPath(base)
	if err != nil {
		return "", fmt.Errorf("resolve confined directory %s: %w", base, err)
	}
	resolvedCandidate, err := resolvePathThroughExistingAncestor(candidate)
	if err != nil {
		return "", err
	}
	if !isPathWithin(resolvedBase, resolvedCandidate) {
		return "", fmt.Errorf("path must stay within %s: %s", resolvedBase, candidate)
	}
	return filepath.Clean(resolvedCandidate), nil
}

func resolvePathThroughExistingAncestor(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", path, err)
	}
	missing := make([]string, 0)
	current := filepath.Clean(absolute)
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, resolveErr := canonicalExistingFilesystemPath(current)
			if resolveErr != nil {
				return "", fmt.Errorf("resolve path %s: %w", path, resolveErr)
			}
			current = resolved
			break
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve path %s: %w", path, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve path %s: no existing ancestor", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
	for index := len(missing) - 1; index >= 0; index-- {
		current = filepath.Join(current, missing[index])
	}
	return filepath.Clean(current), nil
}

func isPathWithin(base, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(base), filepath.Clean(candidate))
	return err == nil &&
		relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

func pathsEqual(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func canonicalRunDirectory(root, prdPath string) (string, string, error) {
	scratchPath := filepath.Join(root, ".scratch")
	scratch, err := canonicalExistingPathWithin(root, scratchPath)
	if err != nil {
		return "", "", err
	}
	candidate := prdPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	prd, err := canonicalExistingPathWithin(scratch, candidate)
	if err != nil {
		return "", "", err
	}
	runDirectory := filepath.Dir(prd)
	if filepath.Dir(runDirectory) != scratch || filepath.Base(prd) != "PRD.md" {
		return "", "", fmt.Errorf("PRD path must match .scratch/<feature>/PRD.md, got: %s", prdPath)
	}
	return runDirectory, prd, nil
}

func reviewReference(issuePath string) string {
	return filepath.Join("review", issueStem(issuePath)+".md")
}

func canonicalReviewPath(root, issuePath string) (string, error) {
	if err := ensurePhysicalReviewDirectory(root); err != nil {
		return "", err
	}
	candidate := filepath.Join(root, reviewReference(issuePath))
	if err := rejectFinalPathRedirect(candidate, "review file"); err != nil {
		return "", err
	}
	return canonicalPotentialPathWithin(root, candidate)
}

func readOptionalConfined(base, path string) (string, error) {
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate = filepath.Clean(candidate)
	if err := rejectFinalPathRedirect(candidate, "file"); err != nil {
		return "", err
	}
	canonical, err := canonicalPotentialPathWithin(base, candidate)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(canonical)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(content), nil
}

func rejectFinalPathRedirect(path, kind string) error {
	redirect, err := filesystemPathRedirect(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s %s: %w", kind, path, err)
	}
	if redirect {
		return fmt.Errorf("%s must not be a filesystem redirect: %s", kind, path)
	}
	return nil
}

type phasePaths struct {
	PRD             string
	Issue           string
	RunDirectory    string
	Progress        string
	ReviewReference string
	Review          string
}

func resolvePhasePaths(root, prdPath, issuePath string) (phasePaths, error) {
	canonicalRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return phasePaths{}, err
	}
	runDirectory, prd, err := canonicalRunDirectory(canonicalRoot, prdPath)
	if err != nil {
		return phasePaths{}, err
	}
	issuesDirectory, err := canonicalExistingPathWithin(runDirectory, filepath.Join(runDirectory, "issues"))
	if err != nil {
		return phasePaths{}, err
	}
	issue, err := canonicalExistingPathWithin(issuesDirectory, issuePath)
	if err != nil {
		return phasePaths{}, err
	}
	if filepath.Dir(issue) != issuesDirectory {
		return phasePaths{}, fmt.Errorf("selected issue must be directly under %s: %s", issuesDirectory, issuePath)
	}
	review, err := canonicalReviewPath(canonicalRoot, issue)
	if err != nil {
		return phasePaths{}, err
	}
	return phasePaths{
		PRD:             prd,
		Issue:           issue,
		RunDirectory:    runDirectory,
		Progress:        filepath.Join(runDirectory, "progress.txt"),
		ReviewReference: reviewReference(issue),
		Review:          review,
	}, nil
}

func canonicalIssueDeltaPath(runDirectory, deltaPath string) (string, error) {
	stateRoot, err := canonicalExistingPathWithin(runDirectory, filepath.Join(runDirectory, workspaceStateDirectory))
	if err != nil {
		return "", err
	}
	delta, err := canonicalExistingPathWithin(stateRoot, deltaPath)
	if err != nil {
		return "", err
	}
	stateDirectory := filepath.Dir(delta)
	if filepath.Dir(stateDirectory) != stateRoot ||
		!strings.HasPrefix(filepath.Base(stateDirectory), "run-") ||
		filepath.Base(delta) != issueDeltaFilename {
		return "", fmt.Errorf("invalid issue-owned review delta path: %s", deltaPath)
	}
	return delta, nil
}

func ensurePhysicalReviewDirectory(root string) error {
	reviewDirectory := filepath.Join(root, "review")
	for _, path := range []string{root, reviewDirectory} {
		redirect, err := filesystemPathRedirect(path)
		if os.IsNotExist(err) && path == reviewDirectory {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect physical review directory %s: %w", path, err)
		}
		if redirect {
			return fmt.Errorf("root review directory and its parent chain must not contain filesystem redirects: %s", path)
		}
	}
	return nil
}
