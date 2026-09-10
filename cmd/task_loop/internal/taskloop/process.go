package taskloop

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type OSProcessRunner struct {
	TemporaryDirectory string
}

func (runner OSProcessRunner) Run(ctx context.Context, binary string, args ...string) (string, string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	executable, err := resolveExecutable(binary)
	if err != nil {
		return "", "", -1, err
	}
	preparedArguments, cleanup, err := executable.prepareArguments(runner.TemporaryDirectory, args)
	if err != nil {
		return "", "", -1, err
	}
	defer cleanup()
	command, err := executable.command(ctx, preparedArguments)
	if err != nil {
		return "", "", -1, err
	}

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.String(), stderr.String(), -1, ctxErr
	}
	if err == nil {
		return stdout.String(), stderr.String(), 0, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), exitError.ExitCode(), nil
	}
	return stdout.String(), stderr.String(), -1, err
}

func (executable resolvedExecutable) prepareArguments(temporaryDirectory string, arguments []string) ([]string, func(), error) {
	if executable.kind != executableWindowsBatch {
		return arguments, func() {}, nil
	}
	if strings.ContainsAny(executable.path, "\"\r\n!") {
		return nil, func() {}, fmt.Errorf("Windows batch shim path contains characters that cannot be passed safely")
	}
	prepared := append([]string(nil), arguments...)
	temporaryFiles := make([]string, 0)
	cleanup := func() {
		for _, path := range temporaryFiles {
			_ = os.Remove(path)
		}
	}
	for index, argument := range prepared {
		if !strings.ContainsAny(argument, "\"\r\n!") {
			continue
		}
		if index == 0 || (prepared[index-1] != "-p" && prepared[index-1] != "--prompt") {
			cleanup()
			return nil, func() {}, fmt.Errorf("Windows batch shim argument %d contains characters that cannot be passed safely", index)
		}
		directory, err := absoluteTemporaryDirectory(temporaryDirectory)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		file, err := os.CreateTemp(directory, ".task-loop-prompt-*.txt")
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("create Windows batch prompt file: %w", err)
		}
		path := file.Name()
		if _, err := file.WriteString(argument); err != nil {
			file.Close()
			_ = os.Remove(path)
			cleanup()
			return nil, func() {}, fmt.Errorf("write Windows batch prompt file: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			cleanup()
			return nil, func() {}, fmt.Errorf("close Windows batch prompt file: %w", err)
		}
		temporaryFiles = append(temporaryFiles, path)
		prepared[index] = "Read and follow the complete UTF-8 task prompt in file " + path
		if strings.ContainsAny(prepared[index], "\"\r\n!") {
			cleanup()
			return nil, func() {}, fmt.Errorf("Windows batch prompt path contains characters that cannot be passed safely")
		}
	}
	return prepared, cleanup, nil
}

func absoluteTemporaryDirectory(configured string) (string, error) {
	if configured == "" {
		configured = os.TempDir()
	}
	directory, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("resolve Windows batch prompt directory: %w", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return "", fmt.Errorf("inspect Windows batch prompt directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Windows batch prompt directory is not a directory: %s", directory)
	}
	return filepath.Clean(directory), nil
}

type executableKind uint8

const (
	executableNative executableKind = iota
	executableWindowsBatch
)

type resolvedExecutable struct {
	path string
	kind executableKind
}

func resolveExecutable(binary string) (resolvedExecutable, error) {
	resolved := ""
	var err error
	if runtime.GOOS == "windows" && filepath.Ext(binary) == "" {
		for _, extension := range []string{".exe", ".com"} {
			if candidate, candidateErr := exec.LookPath(binary + extension); candidateErr == nil {
				resolved = candidate
				break
			}
		}
	}
	if resolved == "" {
		resolved, err = exec.LookPath(binary)
	}
	if err != nil {
		return resolvedExecutable{}, err
	}
	kind := executableNative
	extension := strings.ToLower(filepath.Ext(resolved))
	if runtime.GOOS == "windows" && (extension == ".cmd" || extension == ".bat") {
		kind = executableWindowsBatch
	}
	return resolvedExecutable{path: resolved, kind: kind}, nil
}

func (executable resolvedExecutable) command(ctx context.Context, arguments []string) (*exec.Cmd, error) {
	var command *exec.Cmd
	if executable.kind == executableNative {
		command = exec.CommandContext(ctx, executable.path, arguments...)
	} else {
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			var err error
			comspec, err = exec.LookPath("cmd.exe")
			if err != nil {
				return nil, fmt.Errorf("resolve Windows command processor: %w", err)
			}
		}
		const shimVariable = "__TASK_LOOP_SHIM"
		environment := replaceProcessEnvironment(os.Environ(), shimVariable, escapeWindowsBatchArgument(executable.path))
		commandParts := []string{`"!` + shimVariable + `!"`}
		for index, argument := range arguments {
			name := fmt.Sprintf("__TASK_LOOP_ARG_%d", index)
			environment = replaceProcessEnvironment(environment, name, escapeWindowsBatchArgument(argument))
			commandParts = append(commandParts, `"!`+name+`!"`)
		}
		command = newWindowsBatchCommand(ctx, comspec, strings.Join(commandParts, " "))
		command.Env = environment
	}
	configureProcessCancellation(command)
	return command, nil
}

func escapeWindowsBatchArgument(argument string) string {
	var escaped strings.Builder
	backslashes := 0
	for _, character := range argument {
		if character == '\\' {
			backslashes++
			continue
		}
		if character == '"' {
			escaped.WriteString(strings.Repeat("\\", backslashes*2+1))
			escaped.WriteRune(character)
			backslashes = 0
			continue
		}
		escaped.WriteString(strings.Repeat("\\", backslashes))
		backslashes = 0
		escaped.WriteRune(character)
	}
	escaped.WriteString(strings.Repeat("\\", backslashes*2))
	return escaped.String()
}

type ReaderInput struct {
	reader *bufio.Reader
	out    io.Writer
}

func NewReaderInput(in io.Reader, out io.Writer) *ReaderInput {
	return &ReaderInput{reader: bufio.NewReader(in), out: out}
}

func (input *ReaderInput) Read(prompt string) (string, bool) {
	fmt.Fprintln(input.out, prompt)
	fmt.Fprint(input.out, "> ")
	answer, err := input.reader.ReadString('\n')
	if err != nil && len(answer) == 0 {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimSuffix(answer, "\n"), "\r"), true
}
