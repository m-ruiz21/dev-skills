package taskloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type CreateIssueRequest struct {
	PRDPath                string   `json:"prd"`
	Title                  string   `json:"title"`
	Description            string   `json:"description"`
	AcceptanceCriteria     []string `json:"acceptanceCriteria"`
	Parent                 string   `json:"parent,omitempty"`
	BlockedBy              []string `json:"blockedBy,omitempty"`
	RequestFile            string   `json:"-"`
	TitleFile              string   `json:"-"`
	DescriptionFile        string   `json:"-"`
	AcceptanceCriteriaFile string   `json:"-"`
}

var numberedIssuePattern = regexp.MustCompile(`^([0-9]+)-.+\.md$`)

const issueAllocationLockFilename = ".create-issue.lock"

func CreateIssue(root string, request CreateIssueRequest) (string, error) {
	canonicalRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		return "", err
	}
	request, err = resolveCreateIssueRequest(canonicalRoot, request)
	if err != nil {
		return "", err
	}
	runDirectory, err := issueRunDirectory(canonicalRoot, request.PRDPath)
	if err != nil {
		return "", err
	}
	title, err := resolveIssueTitle(canonicalRoot, request)
	if err != nil {
		return "", err
	}
	parent, err := validateSingleLine("parent", request.Parent, false)
	if err != nil {
		return "", err
	}
	description, err := resolveIssueDescription(canonicalRoot, request)
	if err != nil {
		return "", err
	}
	criteria, err := resolveAcceptanceCriteria(canonicalRoot, request)
	if err != nil {
		return "", err
	}

	issuesDirectory, err := ensureIssuesDirectory(runDirectory)
	if err != nil {
		return "", err
	}
	dependencies := make([]string, 0, len(request.BlockedBy))
	for _, reference := range request.BlockedBy {
		dependency, err := canonicalDependencyReference(canonicalRoot, runDirectory, issuesDirectory, reference)
		if err != nil {
			return "", err
		}
		dependencies = append(dependencies, dependency)
	}

	releaseAllocationLock, err := acquireIssueAllocationLock(issuesDirectory)
	if err != nil {
		return "", err
	}
	defer releaseAllocationLock()

	number, err := nextIssueNumber(issuesDirectory)
	if err != nil {
		return "", err
	}
	slug := issueSlug(title)
	if slug == "" {
		return "", fmt.Errorf("title must contain at least one ASCII letter or number")
	}
	filename := fmt.Sprintf("%02d-%s.md", number, slug)
	target, err := canonicalPotentialPathWithin(issuesDirectory, filepath.Join(issuesDirectory, filename))
	if err != nil {
		return "", err
	}
	if filepath.Dir(target) != issuesDirectory {
		return "", fmt.Errorf("generated issue path escaped issues directory: %s", target)
	}
	if err := rejectFinalPathRedirect(target, "issue file"); err != nil {
		return "", err
	}

	content, err := renderIssue(title, parent, description, criteria, dependencies)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return "", fmt.Errorf("refusing to overwrite existing issue: %s", target)
	}
	if err != nil {
		return "", fmt.Errorf("create issue %s: %w", target, err)
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(target)
		}
	}()
	if _, err := file.WriteString(content); err != nil {
		return "", fmt.Errorf("write issue %s: %w", target, err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close issue %s: %w", target, err)
	}
	complete = true
	return target, nil
}

func resolveCreateIssueRequest(root string, request CreateIssueRequest) (CreateIssueRequest, error) {
	if request.RequestFile == "" {
		return request, nil
	}
	if request.PRDPath != "" ||
		request.Title != "" ||
		request.Description != "" ||
		request.AcceptanceCriteria != nil ||
		request.Parent != "" ||
		request.BlockedBy != nil ||
		request.TitleFile != "" ||
		request.DescriptionFile != "" ||
		request.AcceptanceCriteriaFile != "" {
		return CreateIssueRequest{}, fmt.Errorf("request file may not be combined with other create-issue inputs")
	}
	content, err := readIssueInputFile(root, request.RequestFile, "request")
	if err != nil {
		return CreateIssueRequest{}, err
	}
	if !utf8.Valid(content) {
		return CreateIssueRequest{}, fmt.Errorf("request file must contain valid UTF-8: %s", request.RequestFile)
	}
	content = bytes.TrimPrefix(content, []byte("\xEF\xBB\xBF"))
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var resolved CreateIssueRequest
	if err := decoder.Decode(&resolved); err != nil {
		return CreateIssueRequest{}, fmt.Errorf("decode request file %s: %w", request.RequestFile, err)
	}
	if err := ensureJSONDocumentComplete(decoder); err != nil {
		return CreateIssueRequest{}, fmt.Errorf("decode request file %s: %w", request.RequestFile, err)
	}
	return resolved, nil
}

func ensureJSONDocumentComplete(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("request file must contain exactly one JSON object")
}

func resolveIssueDescription(root string, request CreateIssueRequest) (string, error) {
	if request.DescriptionFile != "" {
		if request.Description != "" {
			return "", fmt.Errorf("description and description file are mutually exclusive")
		}
		return readIssueInput(root, request.DescriptionFile, "description")
	}
	description := strings.ReplaceAll(strings.ReplaceAll(request.Description, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(description) == "" {
		return "", fmt.Errorf("description is required")
	}
	return description, nil
}

func resolveAcceptanceCriteria(root string, request CreateIssueRequest) ([]string, error) {
	if request.AcceptanceCriteriaFile != "" {
		if request.AcceptanceCriteria != nil {
			return nil, fmt.Errorf("acceptance criteria and acceptance criteria file are mutually exclusive")
		}
		criteriaInput, err := readIssueInput(root, request.AcceptanceCriteriaFile, "acceptance criteria")
		if err != nil {
			return nil, err
		}
		criteria := nonemptyLines(criteriaInput)
		if len(criteria) == 0 {
			return nil, fmt.Errorf("acceptance criteria file must contain at least one non-empty line")
		}
		return criteria, nil
	}
	if len(request.AcceptanceCriteria) == 0 {
		return nil, fmt.Errorf("acceptance criteria must contain at least one item")
	}
	criteria := make([]string, len(request.AcceptanceCriteria))
	for index, criterion := range request.AcceptanceCriteria {
		value, err := validateSingleLine(fmt.Sprintf("acceptance criterion %d", index+1), criterion, true)
		if err != nil {
			return nil, err
		}
		criteria[index] = value
	}
	return criteria, nil
}

func acquireIssueAllocationLock(issuesDirectory string) (func(), error) {
	lockPath, err := canonicalPotentialPathWithin(
		issuesDirectory,
		filepath.Join(issuesDirectory, issueAllocationLockFilename),
	)
	if err != nil {
		return nil, fmt.Errorf("resolve issue allocation lock: %w", err)
	}
	if err := rejectFinalPathRedirect(lockPath, "issue allocation lock"); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open issue allocation lock %s: %w", lockPath, err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock issue allocation in %s: %w", issuesDirectory, err)
	}
	return func() {
		_ = unlockFile(file)
		_ = file.Close()
	}, nil
}

func issueRunDirectory(root, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("PRD path is required")
	}
	candidate := input
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		candidate = filepath.Join(candidate, "PRD.md")
	}
	runDirectory, prd, err := canonicalRunDirectory(root, candidate)
	if err != nil {
		return "", fmt.Errorf("invalid PRD path or directory %s: %w", input, err)
	}
	info, err := os.Stat(prd)
	if err != nil {
		return "", fmt.Errorf("read PRD %s: %w", input, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("PRD path is not a file: %s", input)
	}
	return runDirectory, nil
}

func validateSingleLine(name, value string, required bool) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%s must be a single line", name)
	}
	value = strings.TrimSpace(value)
	if required && value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func resolveIssueTitle(root string, request CreateIssueRequest) (string, error) {
	if request.TitleFile == "" {
		return validateSingleLine("title", request.Title, true)
	}
	if request.Title != "" {
		return "", fmt.Errorf("title and title file are mutually exclusive")
	}
	content, err := readIssueInputFile(root, request.TitleFile, "title")
	if err != nil {
		return "", err
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("title file must contain valid UTF-8: %s", request.TitleFile)
	}
	value := strings.TrimPrefix(string(content), "\uFEFF")
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else {
		value = strings.TrimSuffix(value, "\n")
	}
	return validateSingleLine("title", value, true)
}

func readIssueInputFile(root, path, name string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("%s file is required", name)
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	canonical, err := canonicalExistingPathWithin(root, candidate)
	if err != nil {
		return nil, fmt.Errorf("invalid %s file %s: %w", name, path, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return nil, fmt.Errorf("read %s file %s: %w", name, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s file is not a regular file: %s", name, path)
	}
	content, err := os.ReadFile(canonical)
	if err != nil {
		return nil, fmt.Errorf("read %s file %s: %w", name, path, err)
	}
	return content, nil
}

func readIssueInput(root, path, name string) (string, error) {
	content, err := readIssueInputFile(root, path, name)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(strings.TrimPrefix(strings.ReplaceAll(string(content), "\r\n", "\n"), "\uFEFF"))
	if value == "" {
		return "", fmt.Errorf("%s file must not be empty: %s", name, path)
	}
	return value, nil
}

func ensureIssuesDirectory(runDirectory string) (string, error) {
	candidate := filepath.Join(runDirectory, "issues")
	if err := os.Mkdir(candidate, 0o755); err != nil && !os.IsExist(err) {
		return "", fmt.Errorf("create issues directory: %w", err)
	}
	issuesDirectory, err := canonicalExistingPathWithin(runDirectory, candidate)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(issuesDirectory)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("issues path is not a directory: %s", candidate)
	}
	return issuesDirectory, nil
}

func canonicalDependencyReference(root, runDirectory, issuesDirectory, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" || strings.ContainsAny(reference, "\r\n") {
		return "", fmt.Errorf("blocked-by reference must be a non-empty issue path without newlines: %q", reference)
	}
	candidate := reference
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	canonical, err := canonicalExistingPathWithin(issuesDirectory, candidate)
	if err != nil {
		return "", fmt.Errorf("invalid blocked-by issue %s: %w", reference, err)
	}
	relativeToIssues, err := filepath.Rel(issuesDirectory, canonical)
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(relativeToIssues)
	if filepath.Ext(canonical) != ".md" || (directory != "." && directory != "closed") {
		return "", fmt.Errorf("blocked-by issue must be a Markdown file directly under %s or its closed directory: %s", issuesDirectory, reference)
	}
	issue, err := loadIssue(canonical)
	if err != nil {
		return "", fmt.Errorf("read blocked-by issue %s: %w", reference, err)
	}
	if issue.Status == "" {
		return "", fmt.Errorf("blocked-by issue must have YAML frontmatter status: %s", reference)
	}
	if !isPathWithin(runDirectory, canonical) {
		return "", fmt.Errorf("blocked-by issue must belong to the same PRD: %s", reference)
	}
	repositoryRelative, err := filepath.Rel(root, canonical)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(repositoryRelative), nil
}

func nextIssueNumber(issuesDirectory string) (int, error) {
	maximum := 0
	directories := []string{issuesDirectory}
	closedCandidate := filepath.Join(issuesDirectory, "closed")
	if _, err := os.Stat(closedCandidate); err == nil {
		closedDirectory, err := canonicalExistingPathWithin(issuesDirectory, closedCandidate)
		if err != nil {
			return 0, err
		}
		directories = append(directories, closedDirectory)
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("inspect closed issues directory: %w", err)
	}
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("scan issue numbers in %s: %w", directory, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			match := numberedIssuePattern.FindStringSubmatch(entry.Name())
			if match == nil {
				continue
			}
			value, err := strconv.Atoi(match[1])
			if err != nil {
				return 0, fmt.Errorf("invalid issue number in %s: %w", entry.Name(), err)
			}
			if value > maximum {
				maximum = value
			}
		}
	}
	if maximum == int(^uint(0)>>1) {
		return 0, fmt.Errorf("issue number exceeds supported range")
	}
	return maximum + 1, nil
}

func issueSlug(title string) string {
	var slug strings.Builder
	separator := false
	for _, character := range strings.ToLower(title) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			if separator && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(character)
			separator = false
			if slug.Len() >= 80 {
				break
			}
			continue
		}
		separator = slug.Len() > 0
	}
	return strings.Trim(slug.String(), "-")
}

func nonemptyLines(content string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func renderIssue(title, parent, description string, criteria, dependencies []string) (string, error) {
	quotedTitle, err := quoteYAMLScalar(title)
	if err != nil {
		return "", fmt.Errorf("encode issue title: %w", err)
	}
	quotedDependencies := make([]string, len(dependencies))
	for index, dependency := range dependencies {
		quotedDependencies[index], err = quoteYAMLScalar(dependency)
		if err != nil {
			return "", fmt.Errorf("encode blocked-by reference %q: %w", dependency, err)
		}
	}
	var content strings.Builder
	fmt.Fprintf(&content, "---\ntitle: %s\nstatus: ready-for-agent\nblocked-by:", quotedTitle)
	if len(dependencies) == 0 {
		content.WriteString(" []\n")
	} else {
		content.WriteByte('\n')
		for _, dependency := range quotedDependencies {
			fmt.Fprintf(&content, "  - %s\n", dependency)
		}
	}
	content.WriteString("---\n\n")
	if parent != "" {
		fmt.Fprintf(&content, "## Parent\n\n%s\n\n", parent)
	}
	fmt.Fprintf(&content, "## What to build\n\n%s\n\n## Acceptance criteria\n\n", description)
	for _, criterion := range criteria {
		fmt.Fprintf(&content, "- [ ] %s\n", criterion)
	}
	content.WriteString("\n## Blocked by\n\n")
	if len(dependencies) == 0 {
		content.WriteString("None - can start immediately\n")
	} else {
		for _, dependency := range dependencies {
			fmt.Fprintf(&content, "- %s\n", dependency)
		}
	}
	return content.String(), nil
}
