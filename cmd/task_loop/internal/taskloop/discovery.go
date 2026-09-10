package taskloop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

func DiscoverPRDs(root string) ([]string, error) {
	canonicalRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return nil, err
	}
	scratch := filepath.Join(canonicalRoot, ".scratch")
	if _, err := os.Stat(scratch); os.IsNotExist(err) {
		return nil, nil
	}
	canonicalScratch, err := canonicalExistingPathWithin(canonicalRoot, scratch)
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(canonicalScratch, "*", "PRD.md"))
	if err != nil {
		return nil, err
	}
	for index, match := range matches {
		runDirectory, prd, err := canonicalRunDirectory(canonicalRoot, match)
		if err != nil {
			return nil, err
		}
		if filepath.Dir(runDirectory) != canonicalScratch {
			return nil, fmt.Errorf("PRD path escaped .scratch: %s", match)
		}
		matches[index] = prd
	}
	sort.Strings(matches)
	if root == "." || root == "" {
		for i := range matches {
			relative, err := filepath.Rel(canonicalRoot, matches[i])
			if err != nil {
				return nil, err
			}
			matches[i] = filepath.Clean(relative)
		}
	}
	return matches, nil
}

func ParseMaxIterations(value string, supplied bool) (int, error) {
	if !supplied {
		return DefaultMaxIterations, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("--max-iterations must be an integer, got %q", value)
	}
	if parsed < 1 {
		return 0, fmt.Errorf("--max-iterations must be a positive integer, got %d", parsed)
	}
	return parsed, nil
}

func StartRun(root, prdPath, maxValue string, maxSupplied bool) (Run, error) {
	budget, err := ParseMaxIterations(maxValue, maxSupplied)
	if err != nil {
		return Run{}, err
	}
	canonicalRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return Run{}, err
	}
	candidate := prdPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(canonicalRoot, candidate)
	}
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return Run{}, fmt.Errorf("PRD path does not exist: %s", prdPath)
	}
	runDirectory, canonicalPRD, err := canonicalRunDirectory(canonicalRoot, prdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Run{}, fmt.Errorf("PRD path does not exist: %s", prdPath)
		}
		return Run{}, fmt.Errorf("invalid PRD path %s: %w", prdPath, err)
	}
	info, err := os.Stat(canonicalPRD)
	if os.IsNotExist(err) {
		return Run{}, fmt.Errorf("PRD path does not exist: %s", prdPath)
	}
	if err != nil {
		return Run{}, fmt.Errorf("PRD path is not readable: %s (%v)", prdPath, err)
	}
	if !info.Mode().IsRegular() {
		return Run{}, fmt.Errorf("PRD path is not a file: %s", prdPath)
	}
	if _, err := os.ReadFile(canonicalPRD); err != nil {
		return Run{}, fmt.Errorf("PRD path is not readable: %s (%v)", prdPath, err)
	}
	return Run{RepositoryRoot: canonicalRoot, RunDirectory: runDirectory, PRDPath: canonicalPRD, MaxIterations: budget}, nil
}
