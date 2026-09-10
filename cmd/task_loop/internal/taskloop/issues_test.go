package taskloop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestCreateIssueWritesCanonicalActionableIssuesWithDependencies(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "account-recovery")
	description := filepath.Join(repo, ".scratch", "account-recovery", "description.txt")
	criteria := filepath.Join(repo, ".scratch", "account-recovery", "criteria.txt")
	writeFile(t, description, "Let a user request and complete account recovery.\n\nKeep the flow end to end.\n")
	writeFile(t, criteria, "A recovery request can be created\n\nAn expired request is rejected\n")

	first, err := CreateIssue(repo, CreateIssueRequest{
		PRDPath:                filepath.Dir(prd),
		Title:                  "Create recovery request",
		DescriptionFile:        description,
		AcceptanceCriteriaFile: criteria,
		Parent:                 ".scratch/account-recovery/PRD.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "01-create-recovery-request.md" {
		t.Fatalf("unexpected first issue name: %s", first)
	}

	second, err := CreateIssue(repo, CreateIssueRequest{
		PRDPath:                prd,
		Title:                  "Complete recovery flow",
		DescriptionFile:        description,
		AcceptanceCriteriaFile: criteria,
		BlockedBy:              []string{first},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "02-complete-recovery-flow.md" {
		t.Fatalf("unexpected second issue name: %s", second)
	}

	firstContent := readOptional(first)
	for _, wanted := range []string{
		"status: ready-for-agent",
		"blocked-by: []",
		"## Parent",
		"## What to build",
		"## Acceptance criteria",
		"- [ ] A recovery request can be created",
		"## Blocked by",
		"None - can start immediately",
	} {
		if !strings.Contains(firstContent, wanted) {
			t.Errorf("first issue missing %q:\n%s", wanted, firstContent)
		}
	}
	dependencyReference := ".scratch/account-recovery/issues/01-create-recovery-request.md"
	secondContent := readOptional(second)
	if !strings.Contains(secondContent, "blocked-by:\n  - "+strconv.Quote(dependencyReference)) ||
		!strings.Contains(secondContent, "## Blocked by\n\n- "+dependencyReference) {
		t.Fatalf("dependency was not rendered canonically:\n%s", secondContent)
	}

	selected, err := SelectIssue(repo, prd, nil)
	if err != nil || selected != first {
		t.Fatalf("generated issue was not immediately actionable: selected=%q err=%v", selected, err)
	}
	closed := filepath.Join(filepath.Dir(first), "closed", filepath.Base(first))
	writeFile(t, closed, strings.ReplaceAll(firstContent, "status: ready-for-agent", "status: closed"))
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	selected, err = SelectIssue(repo, prd, nil)
	if err != nil || selected != second {
		t.Fatalf("dependent generated issue did not unblock: selected=%q err=%v", selected, err)
	}
}

func TestCreateIssueNumbersAcrossOpenAndClosedAndRefusesUnsafeInput(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "feature")
	description := filepath.Join(repo, "description.txt")
	criteria := filepath.Join(repo, "criteria.txt")
	writeFile(t, description, "Build the complete behavior.")
	writeFile(t, criteria, "The behavior is verified.")
	writeIssue(t, repo, "feature", "01-existing.md", "ready-for-agent", nil, false)
	writeIssue(t, repo, "feature", "09-closed.md", "closed", nil, true)

	path, err := CreateIssue(repo, CreateIssueRequest{
		PRDPath:                prd,
		Title:                  "Next deterministic issue",
		DescriptionFile:        description,
		AcceptanceCriteriaFile: criteria,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "10-next-deterministic-issue.md" {
		t.Fatalf("numbering ignored closed issues: %s", path)
	}

	outside := testRepo(t)
	outsideDescription := filepath.Join(outside, "description.txt")
	writeFile(t, outsideDescription, "secret")
	_, err = CreateIssue(repo, CreateIssueRequest{
		PRDPath:                prd,
		Title:                  "Unsafe input",
		DescriptionFile:        outsideDescription,
		AcceptanceCriteriaFile: criteria,
	})
	if err == nil || !strings.Contains(err.Error(), "must stay within") {
		t.Fatalf("outside input was accepted: %v", err)
	}
}

func TestCreateIssueConcurrentCallsAllocateUniqueNumbers(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "concurrent")
	description := filepath.Join(repo, "description.txt")
	criteria := filepath.Join(repo, "criteria.txt")
	writeFile(t, description, "Build one independently numbered vertical slice.")
	writeFile(t, criteria, "The issue receives a unique numeric prefix.")

	const issueCount = 32
	start := make(chan struct{})
	paths := make(chan string, issueCount)
	errors := make(chan error, issueCount)
	var workers sync.WaitGroup
	for index := 0; index < issueCount; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			path, err := CreateIssue(repo, CreateIssueRequest{
				PRDPath:                prd,
				Title:                  fmt.Sprintf("Concurrent issue %d", index),
				DescriptionFile:        description,
				AcceptanceCriteriaFile: criteria,
			})
			if err != nil {
				errors <- err
				return
			}
			paths <- path
		}(index)
	}
	close(start)
	workers.Wait()
	close(paths)
	close(errors)

	for err := range errors {
		t.Errorf("concurrent create failed: %v", err)
	}
	numbers := make(map[int]string, issueCount)
	for path := range paths {
		prefix, _, ok := strings.Cut(filepath.Base(path), "-")
		if !ok {
			t.Errorf("issue filename has no numeric prefix: %s", path)
			continue
		}
		number, err := strconv.Atoi(prefix)
		if err != nil {
			t.Errorf("issue filename has invalid numeric prefix %q: %v", path, err)
			continue
		}
		if previous, exists := numbers[number]; exists {
			t.Errorf("numeric prefix %d was reused by %s and %s", number, previous, path)
		}
		numbers[number] = path
	}
	if len(numbers) != issueCount {
		t.Fatalf("created %d uniquely numbered issues, want %d", len(numbers), issueCount)
	}
	for number := 1; number <= issueCount; number++ {
		if _, exists := numbers[number]; !exists {
			t.Errorf("numeric prefix %d was not allocated", number)
		}
	}
}

func TestCreateIssueCLIValidatesArgumentsAndPrintsCreatedPath(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "feature")
	writeFile(t, filepath.Join(repo, "title.txt"), "\uFEFFCLI-created issue\r\n")
	writeFile(t, filepath.Join(repo, "description.txt"), "A multiline-safe description.")
	writeFile(t, filepath.Join(repo, "criteria.txt"), "First criterion\nSecond criterion")
	cli := NewCLI()
	cli.Root = repo
	var stdout, stderr bytes.Buffer

	code := cli.Run([]string{
		"create-issue",
		"-prd", ".scratch/feature",
		"-title-file", "title.txt",
		"-description-file", "description.txt",
		"-acceptance-criteria-file", "criteria.txt",
	}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 ||
		stdout.String() != "Created issue: .scratch/feature/issues/01-cli-created-issue.md\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"create-issue", "-prd", ".scratch/feature"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "exactly one of -title or -title-file is required") {
		t.Fatalf("missing arguments: exit=%d stderr=%q", code, stderr.String())
	}

	request, err := parseCreateIssueArguments([]string{
		"-prd", ".scratch/feature",
		"-title", "Dependent",
		"-description-file", "description.txt",
		"-acceptance-criteria-file", "criteria.txt",
		"-blocked-by", "01-first.md",
		"-blocked-by", "02-second.md",
	})
	if err != nil || len(request.BlockedBy) != 2 {
		t.Fatalf("repeated blockers were not parsed: request=%+v err=%v", request, err)
	}

	_, err = parseCreateIssueArguments([]string{
		"-prd", ".scratch/feature",
		"-title", "Direct title",
		"-title-file", "title.txt",
		"-description-file", "description.txt",
		"-acceptance-criteria-file", "criteria.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "-title and -title-file are mutually exclusive") {
		t.Fatalf("conflicting title inputs were accepted: %v", err)
	}

	request, err = parseCreateIssueArguments([]string{"-request-file", "issue.json"})
	if err != nil || request.RequestFile != "issue.json" {
		t.Fatalf("request file was not parsed: request=%+v err=%v", request, err)
	}
	_, err = parseCreateIssueArguments([]string{"-request-file", "issue.json", "-parent", "parent"})
	if err == nil || !strings.Contains(err.Error(), "may not be combined") {
		t.Fatalf("mixed request-file and legacy arguments were accepted: %v", err)
	}
}

func TestCreateIssueRequestFileIsStrictAndRepositoryConfined(t *testing.T) {
	repo := testRepo(t)
	writePRD(t, repo, "feature")
	valid := `{"prd":".scratch/feature/PRD.md","title":"Strict request","description":"Build it.","acceptanceCriteria":["It works."]}`
	tests := []struct {
		name, content, want string
	}{
		{name: "unknown field", content: strings.TrimSuffix(valid, "}") + `,"titleFile":"title.txt"}`, want: "unknown field"},
		{name: "trailing document", content: valid + `{}`, want: "exactly one JSON object"},
		{name: "missing description", content: `{"prd":".scratch/feature/PRD.md","title":"Missing","acceptanceCriteria":["It works."]}`, want: "description is required"},
		{name: "multiline criterion", content: `{"prd":".scratch/feature/PRD.md","title":"Multiline","description":"Build it.","acceptanceCriteria":["first\nsecond"]}`, want: "must be a single line"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestPath := filepath.Join(repo, strings.ReplaceAll(test.name, " ", "-")+".json")
			writeFile(t, requestPath, test.content)
			_, err := CreateIssue(repo, CreateIssueRequest{RequestFile: requestPath})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}

	outside := testRepo(t)
	outsideRequest := filepath.Join(outside, "issue.json")
	writeFile(t, outsideRequest, valid)
	if _, err := CreateIssue(repo, CreateIssueRequest{RequestFile: outsideRequest}); err == nil || !strings.Contains(err.Error(), "must stay within") {
		t.Fatalf("outside request file was accepted: %v", err)
	}
}

func TestCreateIssueRequestFilePreservesDescriptionMarkdownWhitespace(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "markdown-description")
	description := "    package main  \r\n    \tprintln(\"preserved\")\r\n\r\nParagraph with trailing spaces.  \rBare CR above stays a line break.\t \r\n\r\n  "
	normalized := strings.ReplaceAll(strings.ReplaceAll(description, "\r\n", "\n"), "\r", "\n")
	request := CreateIssueRequest{
		PRDPath:            prd,
		Title:              "Preserve Markdown description",
		Description:        description,
		AcceptanceCriteria: []string{"The description round-trips."},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(repo, "issue.json")
	if err := os.WriteFile(requestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	created, err := CreateIssue(repo, CreateIssueRequest{RequestFile: requestPath})
	if err != nil {
		t.Fatal(err)
	}
	content := readOptional(created)
	const prefix = "## What to build\n\n"
	const suffix = "## Acceptance criteria\n\n"
	_, after, found := strings.Cut(content, prefix)
	if !found {
		t.Fatalf("issue has no description section:\n%s", content)
	}
	got, _, found := strings.Cut(after, suffix)
	if !found {
		t.Fatalf("issue has no acceptance criteria section:\n%s", content)
	}
	if want := normalized + "\n\n"; got != want {
		t.Fatalf("rendered description = %q, want normalized input plus section boundary %q", got, want)
	}
}

func TestCreateIssueRequestFilePreservesEveryArgumentWithoutExecution(t *testing.T) {
	base := testRepo(t)
	repo := filepath.Join(base, "repository $(touch path-executed) `ticks` 'quotes' ; &")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	contentMarker := filepath.Join(repo, "content-executed")
	pathMarker := filepath.Join(repo, "path-executed")
	feature := "feature $(touch path-executed) `ticks` 'quotes' ; &"
	prd := writePRD(t, repo, feature)
	blocker := writeIssue(t, repo, feature, "01-blocker $() `ticks` 'quotes' ; &.md", "closed", nil, false)
	requestPath := filepath.Join(repo, "request $(touch path-executed) `ticks` 'quotes' ; &.json")
	title := `Literal "$(touch content-executed)" and $(New-Item content-executed) with 'quotes' and ` + "`touch content-executed`"
	description := strings.Join([]string{
		"Keep apostrophe's and \"quotes\" literal.",
		"$(touch " + contentMarker + ")",
		"`touch " + contentMarker + "`",
		"$(New-Item -ItemType File -Path '" + contentMarker + "')",
		"'@",
		"\"@",
		"EOF",
		"Unicode stays intact: café, 東京, 🚀.",
	}, "\n")
	criteria := []string{
		"An apostrophe's value is unchanged",
		"Shell substitutions remain literal: $(touch " + contentMarker + ") and `touch " + contentMarker + "`",
		"Here-string terminators remain literal: '@ and \"@",
		"Multiline Unicode is preserved: naïve → 完了 ✓",
	}
	request := CreateIssueRequest{
		PRDPath:            prd,
		Title:              title,
		Description:        description,
		AcceptanceCriteria: criteria,
		Parent:             prd,
		BlockedBy:          []string{blocker},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	helper := filepath.Join(repo, "plugin $(touch path-executed) `ticks` 'quotes' ; &", "task-loop")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, executable, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(helper, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	command := exec.Command(helper,
		"-test.run=^TestCLIHelperProcess$", "--",
		"create-issue", "-request-file", requestPath,
	)
	command.Dir = repo
	command.Env = append(os.Environ(), "TASK_LOOP_HELPER=1", "TASK_LOOP_TEST_EXE="+os.Args[0])
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("execute structured-argv create-issue command: %v\n%s", err, output)
	}

	entries, err := os.ReadDir(filepath.Join(filepath.Dir(prd), "issues"))
	if err != nil {
		t.Fatal(err)
	}
	var issueName string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") && entry.Name() != filepath.Base(blocker) {
			if issueName != "" {
				t.Fatalf("created multiple issue files: %q and %q", issueName, entry.Name())
			}
			issueName = entry.Name()
		}
	}
	if issueName == "" {
		t.Fatalf("created issue entries = %#v, want one issue file", entries)
	}
	issuePath := filepath.Join(filepath.Dir(prd), "issues", issueName)
	issue := readOptional(issuePath)
	if got := parseFrontmatter(issue)["title"]; got != title {
		t.Fatalf("title round trip = %q, want %q\n%s", got, title, issue)
	}
	if !strings.Contains(issue, "## What to build\n\n"+description+"\n\n## Acceptance criteria") {
		t.Fatalf("description was not preserved literally:\n%s", issue)
	}
	for _, criterion := range criteria {
		if !strings.Contains(issue, "- [ ] "+criterion+"\n") {
			t.Errorf("criterion was not preserved literally: %q\n%s", criterion, issue)
		}
	}
	if !strings.Contains(issue, "## Parent\n\n"+prd+"\n") {
		t.Fatalf("parent was not preserved literally:\n%s", issue)
	}
	blockerRelative, err := filepath.Rel(repo, blocker)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseListField(issue, "blocked-by"); len(got) != 1 || got[0] != filepath.ToSlash(blockerRelative) {
		t.Fatalf("blocker did not round trip: %#v", got)
	}
	for _, marker := range []string{contentMarker, pathMarker} {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("dynamic request data executed and created marker %s: %v", marker, err)
		}
	}
}

func TestCreateIssueTitleFileRejectsUnsafeOrInvalidContent(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "invalid-title")
	description := filepath.Join(repo, "description.txt")
	criteria := filepath.Join(repo, "criteria.txt")
	writeFile(t, description, "Build it.")
	writeFile(t, criteria, "It works.")

	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{name: "empty", content: []byte(" \t\r\n"), want: "title is required"},
		{name: "multiple lines", content: []byte("first\nsecond\n"), want: "title must be a single line"},
		{name: "extra trailing line", content: []byte("title\n\n"), want: "title must be a single line"},
		{name: "bare carriage return", content: []byte("first\rsecond"), want: "title must be a single line"},
		{name: "invalid UTF-8", content: []byte{0xff, 'a'}, want: "title file must contain valid UTF-8"},
		{name: "no filename characters", content: []byte("東京 🚀\n"), want: "ASCII letter or number"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			titleFile := filepath.Join(repo, "title-"+strings.ReplaceAll(test.name, " ", "-")+".txt")
			if err := os.WriteFile(titleFile, test.content, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := CreateIssue(repo, CreateIssueRequest{
				PRDPath:                prd,
				TitleFile:              titleFile,
				DescriptionFile:        description,
				AcceptanceCriteriaFile: criteria,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	outside := testRepo(t)
	outsideTitle := filepath.Join(outside, "title.txt")
	writeFile(t, outsideTitle, "Outside title\n")
	_, err := CreateIssue(repo, CreateIssueRequest{
		PRDPath:                prd,
		TitleFile:              outsideTitle,
		DescriptionFile:        description,
		AcceptanceCriteriaFile: criteria,
	})
	if err == nil || !strings.Contains(err.Error(), "must stay within") {
		t.Fatalf("outside title file was accepted: %v", err)
	}
}

func TestIssueFrontmatterScalarRoundTrip(t *testing.T) {
	title := `Metadata: #1 "quoted" path\segment`
	dependencies := []string{
		".scratch/feature/issues/01-colon: value.md",
		".scratch/feature/issues/02-hash # value.md",
		`.scratch/feature/issues/03-"quoted".md`,
		`.scratch/feature/issues/04-back\slash.md`,
	}
	content, err := renderIssue(title, "", "Build it.", []string{"It works."}, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseFrontmatter(content)["title"]; got != title {
		t.Fatalf("title round trip = %q, want %q\n%s", got, title, content)
	}
	got := parseListField(content, "blocked-by")
	if strings.Join(got, "\x00") != strings.Join(dependencies, "\x00") {
		t.Fatalf("dependencies round trip = %#v, want %#v\n%s", got, dependencies, content)
	}
	for _, dependency := range dependencies {
		quoted, err := quoteYAMLScalar(dependency)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(content, "  - "+quoted+"\n") {
			t.Errorf("dependency was not emitted as a quoted YAML scalar: %q\n%s", dependency, content)
		}
	}
}

func TestIssueFrontmatterScalarRoundTripWithYAMLLineBreaksAndControls(t *testing.T) {
	var special strings.Builder
	special.WriteString("controls:")
	for character := rune(0); character <= '\x1f'; character++ {
		special.WriteRune(character)
	}
	for character := rune(0x7f); character <= '\u009f'; character++ {
		special.WriteRune(character)
	}
	special.WriteString("; lines:\u2028\u2029")
	value := special.String()
	quoted, err := quoteYAMLScalar(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, character := range quoted {
		if character <= '\x1f' || (character >= '\x7f' && character <= '\x9f') ||
			character == '\u2028' || character == '\u2029' {
			t.Fatalf("quoted scalar contains literal YAML line break/control U+%04X: %q", character, quoted)
		}
	}

	content, err := renderIssue(value, "", "Build it.", []string{"It works."}, []string{value})
	if err != nil {
		t.Fatal(err)
	}
	if got := parseFrontmatter(content)["title"]; got != value {
		t.Fatalf("title round trip = %q, want %q\n%s", got, value, content)
	}
	if got := parseListField(content, "blocked-by"); len(got) != 1 || got[0] != value {
		t.Fatalf("dependency round trip = %#v, want %q\n%s", got, value, content)
	}
}

func TestCreateIssueAcceptsHashInQuotedBlockerPath(t *testing.T) {
	repo := testRepo(t)
	prd := writePRD(t, repo, "hash-blocker")
	description := filepath.Join(repo, "description.txt")
	criteria := filepath.Join(repo, "criteria.txt")
	writeFile(t, description, "Build the dependent behavior.")
	writeFile(t, criteria, "The dependency remains literal.")
	blocker := writeIssue(t, repo, "hash-blocker", "01-blocker #1.md", "closed", nil, false)

	created, err := CreateIssue(repo, CreateIssueRequest{
		PRDPath:                prd,
		Title:                  "Dependent issue",
		DescriptionFile:        description,
		AcceptanceCriteriaFile: criteria,
		BlockedBy:              []string{blocker},
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := loadIssue(created)
	if err != nil {
		t.Fatal(err)
	}
	expected := ".scratch/hash-blocker/issues/01-blocker #1.md"
	if len(issue.BlockedBy) != 1 || issue.BlockedBy[0] != expected {
		t.Fatalf("parsed blockers = %#v, want %q\n%s", issue.BlockedBy, expected, readOptional(created))
	}
}
