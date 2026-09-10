package taskloop

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
)

func NewCLI() *CLI {
	return &CLI{
		Root: ".",
		Deps: Dependencies{Clock: systemClock{}},
	}
}

func (cli *CLI) Run(arguments []string, stdout, stderr io.Writer) int {
	return cli.RunWithAgents(arguments, stdout, stderr, Agents{})
}

func (cli *CLI) RunWithAgents(arguments []string, stdout, stderr io.Writer, injected Agents) int {
	root := cli.Root
	if root == "" {
		root = "."
	}
	canonicalRoot, err := canonicalRepositoryRoot(root)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	if len(arguments) > 0 && arguments[0] == "add-message" {
		return cli.runAddMessage(canonicalRoot, arguments[1:], stdout, stderr)
	}
	if len(arguments) > 0 && arguments[0] == "create-issue" {
		return cli.runCreateIssue(canonicalRoot, arguments[1:], stdout, stderr)
	}
	prdPath, maxValue, maxSupplied, err := parseRunArguments(arguments)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 2
	}
	if containsHelp(arguments) {
		printUsage(stdout)
		return 0
	}
	ctx, stop := interruptContext(injected.Interrupts)
	defer stop()
	if prdPath == "" {
		prdPath, err = cli.selectDiscoveredPRD(ctx, canonicalRoot, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
			return 1
		}
		if prdPath == "" {
			return 0
		}
	}
	run, err := StartRun(canonicalRoot, prdPath, maxValue, maxSupplied)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Selected PRD: %s\nMax iterations: %d\n", run.PRDPath, run.MaxIterations)

	issuePath, err := SelectIssue(canonicalRoot, run.PRDPath, injected.Triage)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Selected issue: %s\n", issuePath)
	captureBaseline := cli.Deps.CaptureBaseline
	if captureBaseline == nil {
		captureBaseline = CaptureWorkspaceBaseline
	}
	run.Baseline, err = captureBaseline(ctx, canonicalRoot, run.RunDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: capture issue workspace baseline: %v\n", err)
		return 1
	}
	defer run.Baseline.Cleanup()
	agents, commandPath := cli.completeAgents(stdout, stderr, run.Baseline.StateDirectory, injected)
	store := MessageStore{Root: canonicalRoot, StateDirectory: run.Baseline.StateDirectory, Clock: cli.Deps.Clock}
	return RunIssueLoop(ctx, canonicalRoot, run, issuePath, commandPath, store, agents)
}

func (cli *CLI) selectDiscoveredPRD(ctx context.Context, root string, stdout io.Writer) (string, error) {
	prds, err := DiscoverPRDs(root)
	if err != nil {
		return "", err
	}
	if len(prds) == 0 {
		fmt.Fprintln(stdout, "No PRDs found under .scratch/<feature>/PRD.md")
		return "", nil
	}

	options := make([]PRDOption, len(prds))
	for index, path := range prds {
		options[index] = PRDOption{
			Path:  path,
			Label: sanitizePRDPickerLabel(displayPRDPath(root, path)),
		}
	}
	selector := cli.Deps.PRDSelector
	if selector == nil {
		var available bool
		selector, available = systemPRDSelector()
		if !available {
			for _, option := range options {
				fmt.Fprintln(stdout, option.Label)
			}
			return "", nil
		}
	}

	selected, ok, err := selector.Select(ctx, options)
	if err != nil {
		return "", err
	}
	if !ok {
		fmt.Fprintln(stdout, "PRD selection cancelled.")
		return "", nil
	}
	for _, option := range options {
		if selected.Path == option.Path {
			return option.Path, nil
		}
	}
	return "", fmt.Errorf("PRD selector returned an unknown path %q", selected.Path)
}

func displayPRDPath(root, path string) string {
	display := path
	if relative, err := filepath.Rel(root, path); err == nil {
		display = relative
	}
	return filepath.ToSlash(display)
}

func (cli *CLI) completeAgents(stdout, stderr io.Writer, temporaryDirectory string, injected Agents) (Agents, string) {
	runner := cli.Deps.Runner
	if runner == nil {
		runner = OSProcessRunner{TemporaryDirectory: temporaryDirectory}
	} else if osRunner, ok := runner.(OSProcessRunner); ok && osRunner.TemporaryDirectory == "" {
		osRunner.TemporaryDirectory = temporaryDirectory
		runner = osRunner
	}
	binary := injected.CopilotBin
	if binary == "" {
		binary = "copilot"
	}
	if injected.Develop == nil {
		injected.Develop = func(ctx context.Context, phase DevelopmentContext) (string, error) {
			return invokeCopilot(ctx, runner, binary, "development", buildDevelopmentPrompt(phase))
		}
	}
	if injected.Test == nil {
		injected.Test = func(ctx context.Context, phase TestingContext) (string, error) {
			return invokeCopilot(ctx, runner, binary, "testing", buildTestingPrompt(phase))
		}
	}
	if injected.Review == nil {
		injected.Review = func(ctx context.Context, phase ReviewContext) (string, error) {
			return invokeCopilot(ctx, runner, binary, "review", buildReviewPrompt(phase))
		}
	}
	if injected.Stdin == nil {
		if cli.Deps.Input != nil {
			injected.Stdin = cli.Deps.Input
		} else {
			injected.Stdin = NewReaderInput(os.Stdin, stdout)
		}
	}
	injected.Stdout, injected.Stderr = stdout, stderr
	injected.UseColor = outputIsTerminal(stdout)
	commandPath, err := os.Executable()
	if err != nil {
		commandPath = "task-loop"
	}
	if absolute, err := filepath.Abs(commandPath); err == nil {
		commandPath = absolute
	}
	return injected, commandPath
}

func invokeCopilot(ctx context.Context, runner ProcessRunner, binary, phase, prompt string) (string, error) {
	stdout, stderr, exitCode, err := runner.Run(ctx, binary, "--yolo", "-p", prompt)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", fmt.Errorf("failed to start %s agent process: %v", phase, err)
	}
	if exitCode != 0 {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		return "", fmt.Errorf("%s agent process exited with status %d: %s", phase, exitCode, detail)
	}
	return stdout, nil
}

func interruptContext(interrupts <-chan os.Signal) (context.Context, func()) {
	if interrupts == nil {
		return signal.NotifyContext(context.Background(), wholeLoopSignals()...)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		select {
		case <-interrupts:
			cancel()
		case <-stopped:
		}
	}()
	return ctx, func() {
		close(stopped)
		cancel()
	}
}

func (cli *CLI) runAddMessage(root string, arguments []string, stdout, stderr io.Writer) int {
	values, err := parseNamedArguments(arguments, map[string]bool{
		"-file": true, "-message": false, "-message-file": false, "-from": true, "-to": false,
	})
	if err != nil {
		fmt.Fprintf(stderr, "task-loop add-message: error: %v\n", err)
		return 2
	}

	if containsHelp(arguments) {
		fmt.Fprintln(stdout, "usage: task-loop add-message -file FILE (-message MESSAGE | -message-file FILE) -from SENDER [-to THREAD]")
		return 0
	}
	for _, required := range []string{"-file", "-from"} {
		if _, ok := values[required]; !ok {
			fmt.Fprintf(stderr, "task-loop add-message: error: the following arguments are required: %s\n", required)
			return 2
		}
	}
	messageInput, err := ParseMessageInput(
		values["-message"], hasNamedArgument(values, "-message"),
		values["-message-file"], hasNamedArgument(values, "-message-file"),
	)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop add-message: error: %v\n", err)
		return 2
	}
	sender, err := ParseSender(values["-from"])
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	message, err := messageInput.Resolve(root)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	store := MessageStore{Root: root, Clock: cli.Deps.Clock}
	target, targetErr := store.confinedPath(values["-file"])
	if targetErr != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", targetErr)
		return 1
	}
	if message.consumedPath != "" {
		if err := message.ensureDistinctDestination(target); err != nil {
			fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
			return 1
		}
	}
	var result ThreadMessage
	if threadID, replying := values["-to"]; replying {
		result, err = store.append(target, message.content, sender, threadID, true, message.consumedIdentity)
	} else {
		result, err = store.append(target, message.content, sender, "", false, message.consumedIdentity)
	}
	if err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	if err := message.Consume(target); err != nil {
		fmt.Fprintf(stderr, "task-loop: error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Thread %s\n", result.ThreadID)
	return 0
}

func (cli *CLI) runCreateIssue(root string, arguments []string, stdout, stderr io.Writer) int {
	request, err := parseCreateIssueArguments(arguments)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop create-issue: error: %v\n", err)
		return 2
	}
	if containsHelp(arguments) {
		printCreateIssueUsage(stdout)
		return 0
	}
	path, err := CreateIssue(root, request)
	if err != nil {
		fmt.Fprintf(stderr, "task-loop create-issue: error: %v\n", err)
		return 1
	}
	relative, err := filepath.Rel(root, path)
	if err == nil {
		path = relative
	}
	fmt.Fprintf(stdout, "Created issue: %s\n", filepath.ToSlash(path))
	return 0
}

func parseCreateIssueArguments(arguments []string) (CreateIssueRequest, error) {
	var request CreateIssueRequest
	singular := map[string]*string{
		"-request-file":             &request.RequestFile,
		"-prd":                      &request.PRDPath,
		"-title":                    &request.Title,
		"-title-file":               &request.TitleFile,
		"-description-file":         &request.DescriptionFile,
		"-acceptance-criteria-file": &request.AcceptanceCriteriaFile,
		"-parent":                   &request.Parent,
	}
	seen := make(map[string]bool)
	for index := 0; index < len(arguments); index++ {
		name := arguments[index]
		if name == "-h" || name == "--help" {
			continue
		}
		target, known := singular[name]
		if !known && name != "-blocked-by" {
			return CreateIssueRequest{}, fmt.Errorf("unrecognized arguments: %s", name)
		}
		if index+1 >= len(arguments) || isCreateIssueOption(arguments[index+1]) {
			return CreateIssueRequest{}, fmt.Errorf("argument %s: expected one argument", name)
		}
		index++
		if name == "-blocked-by" {
			request.BlockedBy = append(request.BlockedBy, arguments[index])
			continue
		}
		if seen[name] {
			return CreateIssueRequest{}, fmt.Errorf("argument %s: may only be provided once", name)
		}
		seen[name] = true
		*target = arguments[index]
	}
	if containsHelp(arguments) {
		return request, nil
	}
	if seen["-request-file"] {
		if len(seen) != 1 || len(request.BlockedBy) != 0 {
			return CreateIssueRequest{}, fmt.Errorf("-request-file may not be combined with other create-issue arguments")
		}
		return request, nil
	}
	if seen["-title"] == seen["-title-file"] {
		if seen["-title"] {
			return CreateIssueRequest{}, fmt.Errorf("-title and -title-file are mutually exclusive")
		}
		return CreateIssueRequest{}, fmt.Errorf("exactly one of -title or -title-file is required")
	}
	for _, required := range []string{"-prd", "-description-file", "-acceptance-criteria-file"} {
		if !seen[required] {
			return CreateIssueRequest{}, fmt.Errorf("the following argument is required: %s", required)
		}
	}
	return request, nil
}

func isCreateIssueOption(argument string) bool {
	switch argument {
	case "-request-file", "-prd", "-title", "-title-file", "-description-file", "-acceptance-criteria-file", "-parent", "-blocked-by", "-h", "--help":
		return true
	default:
		return false
	}
}

func hasNamedArgument(values map[string]string, name string) bool {
	_, ok := values[name]
	return ok
}

func parseRunArguments(arguments []string) (string, string, bool, error) {
	prdPath, maxValue := "", ""
	maxSupplied := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "-h" || argument == "--help" {
			continue
		}
		if argument == "--max-iterations" {
			if index+1 >= len(arguments) {
				return "", "", false, fmt.Errorf("argument --max-iterations: expected one argument")
			}
			if isRunOption(arguments[index+1]) {
				return "", "", false, fmt.Errorf("argument --max-iterations: expected one argument")
			}
			index++
			maxValue, maxSupplied = arguments[index], true
			continue
		}
		if strings.HasPrefix(argument, "--max-iterations=") {
			maxValue, maxSupplied = strings.TrimPrefix(argument, "--max-iterations="), true
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return "", "", false, fmt.Errorf("unrecognized arguments: %s", argument)
		}
		if prdPath != "" {
			return "", "", false, fmt.Errorf("unrecognized arguments: %s", argument)
		}
		prdPath = argument
	}
	return prdPath, maxValue, maxSupplied, nil
}

func isRunOption(argument string) bool {
	return argument == "--max-iterations" ||
		strings.HasPrefix(argument, "--max-iterations=") ||
		argument == "-h" ||
		argument == "--help"
}

func parseNamedArguments(arguments []string, allowed map[string]bool) (map[string]string, error) {
	values := make(map[string]string)
	for index := 0; index < len(arguments); index++ {
		name := arguments[index]
		if name == "-h" || name == "--help" {
			continue
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unrecognized arguments: %s", name)
		}
		if index+1 >= len(arguments) {
			return nil, fmt.Errorf("argument %s: expected one argument", name)
		}
		next := arguments[index+1]
		if _, recognized := allowed[next]; recognized || next == "-h" || next == "--help" {
			return nil, fmt.Errorf("argument %s: expected one argument", name)
		}
		index++
		values[name] = arguments[index]
	}
	return values, nil
}

func containsHelp(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "-h" || argument == "--help" {
			return true
		}
	}
	return false
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: task-loop [-h] [--max-iterations MAX_ITERATIONS] [prd_path]")
	fmt.Fprintln(writer, "       task-loop create-issue -request-file FILE")
	fmt.Fprintln(writer, "       task-loop create-issue -prd PATH (-title TITLE | -title-file FILE) -description-file FILE -acceptance-criteria-file FILE [-parent REF] [-blocked-by ISSUE]...")
	fmt.Fprintln(writer, "       task-loop add-message -file FILE (-message MESSAGE | -message-file FILE) -from SENDER [-to THREAD]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Coordinate one PRD issue at a time through triage, TDD development, testing, and review.")
}

func printCreateIssueUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: task-loop create-issue -request-file FILE")
	fmt.Fprintln(writer, "       task-loop create-issue -prd PATH (-title TITLE | -title-file FILE) -description-file FILE -acceptance-criteria-file FILE [-parent REF] [-blocked-by ISSUE]...")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Create the next numbered canonical issue for a local .scratch/<feature>/PRD.md.")
	fmt.Fprintln(writer, "Automated callers should use one repository-confined UTF-8 JSON -request-file.")
	fmt.Fprintln(writer, "Legacy flags remain available for direct human use.")
}

func outputIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
