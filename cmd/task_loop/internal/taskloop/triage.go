package taskloop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Issue struct {
	Path      string
	Status    string
	BlockedBy []string
}

type TriageContext struct {
	PRDPath    string
	Candidates []Issue
	Progress   string
}

func SelectIssue(root, prdPath string, agent TriageAgent) (string, error) {
	canonicalRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return "", err
	}
	runDirectory, canonicalPRD, err := canonicalRunDirectory(canonicalRoot, prdPath)
	if err != nil {
		return "", err
	}
	issuesCandidate := filepath.Join(runDirectory, "issues")
	issuesDir, err := canonicalExistingPathWithin(runDirectory, issuesCandidate)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no actionable issues found under %s", issuesCandidate)
	}
	if err != nil {
		return "", err
	}
	candidates, err := actionableIssues(canonicalRoot, runDirectory, issuesDir)
	if err != nil {
		return "", err
	}
	review := make([]Issue, 0)
	for _, issue := range candidates {
		if issue.Status == "review" {
			review = append(review, issue)
		}
	}
	if len(review) > 0 {
		candidates = review
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no actionable issues found under %s", issuesDir)
	}
	progress, err := readOptionalConfined(runDirectory, filepath.Join(runDirectory, "progress.txt"))
	if err != nil {
		return "", err
	}
	context := TriageContext{PRDPath: canonicalPRD, Candidates: candidates, Progress: progress}
	if agent == nil {
		agent = func(context TriageContext) (string, error) {
			return context.Candidates[0].Path, nil
		}
	}
	response, err := agent(context)
	if err != nil {
		return "", err
	}
	selected, err := parseTriageResponse(response, candidates)
	if err != nil {
		return "", err
	}
	reviewPath, err := canonicalReviewPath(canonicalRoot, selected)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(reviewPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(reviewPath), 0o755); err != nil {
			return "", err
		}
		file, createErr := os.OpenFile(reviewPath, os.O_CREATE|os.O_EXCL, 0o644)
		if createErr != nil && !os.IsExist(createErr) {
			return "", createErr
		}
		if file != nil {
			file.Close()
		}
	}
	return selected, nil
}

func actionableIssues(root, runDirectory, issuesDir string) ([]Issue, error) {
	entries, err := os.ReadDir(issuesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	issues := make([]Issue, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		issuePath, err := canonicalExistingPathWithin(issuesDir, filepath.Join(issuesDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		issue, err := loadIssue(issuePath)
		if err != nil {
			return nil, err
		}
		if issue.Status != "ready-for-agent" && issue.Status != "review" {
			continue
		}
		resolved := true
		for _, dependency := range issue.BlockedBy {
			closed, err := dependencyResolved(root, runDirectory, dependency, issuesDir)
			if err != nil {
				return nil, err
			}
			if !closed {
				resolved = false
				break
			}
		}
		if resolved {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

func loadIssue(path string) (Issue, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Issue{}, err
	}
	fields := parseFrontmatter(string(content))
	return Issue{Path: path, Status: fields["status"], BlockedBy: parseListField(string(content), "blocked-by")}, nil
}

func parseFrontmatter(content string) map[string]string {
	result := make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return result
	}
	end := strings.Index(content[3:], "\n---")
	if end < 0 {
		return result
	}
	for _, line := range strings.Split(content[3:3+end], "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if value != "" && value != "[]" {
			result[key] = unquoteYAMLScalar(value)
		}
	}
	return result
}

func parseListField(content, wanted string) []string {
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	end := strings.Index(content[3:], "\n---")
	if end < 0 {
		return nil
	}
	lines := strings.Split(content[3:3+end], "\n")
	for i, line := range lines {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != wanted {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "[]" {
			return nil
		}
		if value != "" {
			return []string{unquoteYAMLScalar(value)}
		}
		items := make([]string, 0)
		for _, next := range lines[i+1:] {
			next = strings.TrimSpace(next)
			if !strings.HasPrefix(next, "-") {
				break
			}
			items = append(items, unquoteYAMLScalar(strings.TrimSpace(strings.TrimPrefix(next, "-"))))
		}
		return items
	}
	return nil
}

func dependencyResolved(root, runDirectory, reference, issuesDir string) (bool, error) {
	original := reference
	if !filepath.IsAbs(original) {
		original = filepath.Join(root, original)
	}
	original, err := canonicalPotentialPathWithin(runDirectory, original)
	if err != nil {
		return false, err
	}
	closedFallback, err := canonicalPotentialPathWithin(runDirectory, filepath.Join(issuesDir, "closed", filepath.Base(reference)))
	if err != nil {
		return false, err
	}
	return issueClosed(original) || issueClosed(closedFallback), nil
}

func issueClosed(path string) bool {
	content, err := os.ReadFile(path)
	return err == nil && parseFrontmatter(string(content))["status"] == "closed"
}

func parseTriageResponse(response string, candidates []Issue) (string, error) {
	if strings.Contains(strings.TrimSuffix(response, "\n"), "\n") {
		return "", fmt.Errorf("triage response must be exactly one issue reference, not multiple lines")
	}
	reference := strings.TrimSpace(response)
	if reference == "" {
		return "", fmt.Errorf("triage response must not be empty")
	}
	for _, candidate := range candidates {
		if candidate.Path == reference {
			return candidate.Path, nil
		}
	}
	return "", fmt.Errorf("triage response is not an eligible issue reference: %q", reference)
}

func readOptional(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}
