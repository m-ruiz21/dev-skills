package taskloop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

var threadHeaderRE = regexp.MustCompile(`(?m)^-- Thread (\S+)$`)

type ThreadMessage struct {
	ThreadID    string
	IsNewThread bool
}

type MessageStore struct {
	Root           string
	StateDirectory string
	Clock          Clock
}

type CommandShell uint8

const (
	CommandShellPOSIX CommandShell = iota
	CommandShellPowerShell
)

type MessageInputKind uint8

const (
	MessageInputInline MessageInputKind = iota
	MessageInputFile
)

type MessageInput struct {
	kind  MessageInputKind
	value string
}

type MessagePurpose uint8

const (
	MessagePurposeDevelopmentProgress MessagePurpose = iota
	MessagePurposeTestingProgress
	MessagePurposeTestingReview
	MessagePurposeReviewProgress
)

type resolvedMessageInput struct {
	content          string
	consumedPath     string
	consumedIdentity os.FileInfo
}

func (store MessageStore) Add(filePath, message string, sender Sender) (ThreadMessage, error) {
	return store.append(filePath, message, sender, "", false, nil)
}

func (store MessageStore) Reply(filePath, message string, sender Sender, threadID string) (ThreadMessage, error) {
	return store.append(filePath, message, sender, threadID, true, nil)
}

func (store MessageStore) append(filePath, message string, sender Sender, replyTo string, isReply bool, sourceIdentity os.FileInfo) (ThreadMessage, error) {
	if _, err := ParseSender(string(sender)); err != nil {
		return ThreadMessage{}, err
	}
	if strings.TrimSpace(message) == "" {
		return ThreadMessage{}, errors.New("-message must not be empty")
	}
	target, err := store.confinedPath(filePath)
	if err != nil {
		return ThreadMessage{}, err
	}
	if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
		return ThreadMessage{}, fmt.Errorf("path must be a file, not a directory: %s", filePath)
	}
	if isReply {
		if _, statErr := os.Stat(target); statErr != nil {
			return ThreadMessage{}, fmt.Errorf("unknown thread id: %q", replyTo)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return ThreadMessage{}, fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	root, err := canonicalRepositoryRoot(store.Root)
	if err != nil {
		return ThreadMessage{}, err
	}
	if isPathWithin(filepath.Join(root, "review"), target) {
		if err := ensurePhysicalReviewDirectory(root); err != nil {
			return ThreadMessage{}, err
		}
	}
	file, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return ThreadMessage{}, fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	defer file.Close()
	if err := lockFile(file); err != nil {
		return ThreadMessage{}, fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	defer unlockFile(file)
	if sourceIdentity != nil {
		targetIdentity, statErr := file.Stat()
		if statErr != nil {
			return ThreadMessage{}, fmt.Errorf("failed to inspect %s: %w", filePath, statErr)
		}
		if os.SameFile(sourceIdentity, targetIdentity) {
			return ThreadMessage{}, errors.New("-message-file must differ from -file")
		}
	}

	content, err := readLockedFile(file)
	if err != nil {
		return ThreadMessage{}, fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	threadID := replyTo
	isNew := !isReply
	if isNew {
		threadID, err = nextThreadID(content)
		if err != nil {
			return ThreadMessage{}, err
		}
	} else if !containsThread(content, replyTo) {
		return ThreadMessage{}, fmt.Errorf("unknown thread id: %q", replyTo)
	}
	timestamp := store.clock().Now().UTC().Format("2006-01-02T15:04:05Z")
	var block string
	if isNew {
		block = fmt.Sprintf("-- Thread %s\n[%s] - %s\n\n%s\n", threadID, sender, timestamp, message)
	} else {
		block = fmt.Sprintf("-- Reply to Thread %s\n[%s] - %s\n\n%s\n", threadID, sender, timestamp, message)
	}
	if _, err := file.Seek(0, 2); err != nil {
		return ThreadMessage{}, fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	if _, err := file.WriteString(block); err != nil {
		return ThreadMessage{}, fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	return ThreadMessage{ThreadID: threadID, IsNewThread: isNew}, nil
}

func (store MessageStore) clock() Clock {
	if store.Clock != nil {
		return store.Clock
	}
	return systemClock{}
}

func readLockedFile(file *os.File) (string, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return "", err
	}
	content, err := io.ReadAll(file)
	return string(content), err
}

func containsThread(content, id string) bool {
	for _, match := range threadHeaderRE.FindAllStringSubmatch(content, -1) {
		if match[1] == id {
			return true
		}
	}
	return false
}

func nextThreadID(content string) (string, error) {
	ids := make([]int, 0)
	for _, match := range threadHeaderRE.FindAllStringSubmatch(content, -1) {
		id, err := strconv.Atoi(match[1])
		if err != nil {
			return "", fmt.Errorf("existing thread id is not numeric: %q", match[1])
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) == 0 {
		return "1", nil
	}
	return strconv.Itoa(ids[len(ids)-1] + 1), nil
}

func (store MessageStore) confinedPath(path string) (string, error) {
	root, err := canonicalRepositoryRoot(store.Root)
	if err != nil {
		return "", fmt.Errorf("path must stay within the current working tree: %s", path)
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)
	if isPathWithin(filepath.Join(root, "review"), target) {
		if err := ensurePhysicalReviewDirectory(root); err != nil {
			return "", err
		}
		if err := rejectFinalPathRedirect(target, "review file"); err != nil {
			return "", err
		}
	}
	target, err = canonicalPotentialPathWithin(root, filepath.Clean(target))
	if err != nil {
		return "", fmt.Errorf("path must stay within the current working tree: %s", path)
	}
	return target, nil
}

func ParseMessageInput(inline string, hasInline bool, filePath string, hasFile bool) (MessageInput, error) {
	switch {
	case hasInline && hasFile:
		return MessageInput{}, errors.New("argument -message-file: not allowed with argument -message")
	case !hasInline && !hasFile:
		return MessageInput{}, errors.New("one of the arguments -message -message-file is required")
	case hasInline:
		return MessageInput{kind: MessageInputInline, value: inline}, nil
	default:
		return MessageInput{kind: MessageInputFile, value: filePath}, nil
	}
}

func (input MessageInput) Resolve(root string) (resolvedMessageInput, error) {
	switch input.kind {
	case MessageInputInline:
		if strings.TrimSpace(input.value) == "" {
			return resolvedMessageInput{}, errors.New("-message must not be empty")
		}
		return resolvedMessageInput{content: input.value}, nil
	case MessageInputFile:
		canonicalRoot, err := canonicalRepositoryRoot(root)
		if err != nil {
			return resolvedMessageInput{}, err
		}
		path := input.value
		if !filepath.IsAbs(path) {
			path = filepath.Join(canonicalRoot, path)
		}
		path = filepath.Clean(path)
		if err := rejectFinalPathRedirect(path, "-message-file"); err != nil {
			return resolvedMessageInput{}, err
		}
		canonicalPath, err := canonicalExistingPathWithin(canonicalRoot, path)
		if err != nil {
			return resolvedMessageInput{}, fmt.Errorf("-message-file must stay within the current working tree: %s", input.value)
		}
		file, err := os.Open(canonicalPath)
		if err != nil {
			return resolvedMessageInput{}, fmt.Errorf("read -message-file %s: %w", input.value, err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return resolvedMessageInput{}, fmt.Errorf("read -message-file %s: %w", input.value, err)
		}
		if !info.Mode().IsRegular() {
			return resolvedMessageInput{}, fmt.Errorf("-message-file must be a regular file: %s", input.value)
		}
		lexicalInfo, err := os.Lstat(canonicalPath)
		if err != nil {
			return resolvedMessageInput{}, fmt.Errorf("read -message-file %s: %w", input.value, err)
		}
		if !os.SameFile(info, lexicalInfo) {
			return resolvedMessageInput{}, fmt.Errorf("-message-file changed while opening: %s", input.value)
		}
		content, err := io.ReadAll(file)
		if err != nil {
			return resolvedMessageInput{}, fmt.Errorf("read -message-file %s: %w", input.value, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			return resolvedMessageInput{}, errors.New("-message-file content must not be empty")
		}
		return resolvedMessageInput{content: string(content), consumedPath: canonicalPath, consumedIdentity: info}, nil
	default:
		return resolvedMessageInput{}, errors.New("unsupported message input")
	}
}

func (input resolvedMessageInput) ensureDistinctDestination(destination string) error {
	if input.consumedPath == "" {
		return nil
	}
	canonicalDestination, err := resolvePathThroughExistingAncestor(destination)
	if err != nil {
		return fmt.Errorf("resolve -file destination: %w", err)
	}
	if pathsEqual(input.consumedPath, canonicalDestination) {
		return errors.New("-message-file must differ from -file")
	}
	destinationIdentity, err := os.Stat(canonicalDestination)
	if err == nil && input.consumedIdentity != nil && os.SameFile(input.consumedIdentity, destinationIdentity) {
		return errors.New("-message-file must differ from -file")
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect -file destination: %w", err)
	}
	return nil
}

func (input resolvedMessageInput) Consume(destination string) error {
	if input.consumedPath == "" {
		return nil
	}
	if err := input.ensureDistinctDestination(destination); err != nil {
		return err
	}
	if err := rejectFinalPathRedirect(input.consumedPath, "-message-file"); err != nil {
		return err
	}
	info, err := os.Lstat(input.consumedPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect consumed -message-file: %w", err)
	}
	if input.consumedIdentity == nil || !os.SameFile(input.consumedIdentity, info) {
		return fmt.Errorf("-message-file changed before it could be consumed: %s", input.consumedPath)
	}
	if err := os.Remove(input.consumedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove consumed -message-file: %w", err)
	}
	return nil
}

func (store MessageStore) PrepareMessageFile(purpose MessagePurpose) (string, error) {
	root, err := canonicalRepositoryRoot(store.Root)
	if err != nil {
		return "", err
	}
	state, err := canonicalExistingPathWithin(root, store.StateDirectory)
	if err != nil {
		return "", fmt.Errorf("resolve run-owned message state: %w", err)
	}
	stateRoot := filepath.Dir(state)
	runDirectory := filepath.Dir(stateRoot)
	scratchDirectory := filepath.Dir(runDirectory)
	if filepath.Base(stateRoot) != workspaceStateDirectory ||
		filepath.Base(scratchDirectory) != ".scratch" ||
		!pathsEqual(filepath.Dir(scratchDirectory), root) ||
		!strings.HasPrefix(filepath.Base(state), "run-") {
		return "", fmt.Errorf("message state must be an isolated task-loop run directory: %s", state)
	}
	messageDirectory := filepath.Join(state, "messages")
	if err := os.MkdirAll(messageDirectory, 0o700); err != nil {
		return "", fmt.Errorf("create run-owned message directory: %w", err)
	}
	messageDirectory, err = canonicalExistingPathWithin(state, messageDirectory)
	if err != nil {
		return "", err
	}
	filename, err := purpose.filename()
	if err != nil {
		return "", err
	}
	path := filepath.Join(messageDirectory, filename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("clear stale message transport: %w", err)
	}
	return path, nil
}

func (purpose MessagePurpose) filename() (string, error) {
	switch purpose {
	case MessagePurposeDevelopmentProgress:
		return "development-progress.txt", nil
	case MessagePurposeTestingProgress:
		return "testing-progress.txt", nil
	case MessagePurposeTestingReview:
		return "testing-review.txt", nil
	case MessagePurposeReviewProgress:
		return "review-progress.txt", nil
	default:
		return "", fmt.Errorf("unsupported message purpose: %d", purpose)
	}
}

func BuildProgressUpdateInstruction(progressPath, messageFilePath string, sender Sender, commandPath string) (string, error) {
	command, err := BuildAddMessageFileCommand(progressPath, messageFilePath, sender, commandPath)
	if err != nil {
		return "", err
	}
	return "If you change any code, tests, documentation, configuration, generated artifacts, or issue-tracker files, overwrite the fixed run-owned message file " + fmt.Sprintf("%q", messageFilePath) + " with only your summary of files and meaningful changes, then run this exact progress command without editing it: `" + command + "`. Do not put message text in the command, rewrite prior progress, or replace the bundled executable with a bare `task-loop`.", nil
}

func BuildReviewFindingInstruction(reviewPath, messageFilePath string, sender Sender, commandPath string) (string, error) {
	command, err := BuildAddMessageFileCommand(reviewPath, messageFilePath, sender, commandPath)
	if err != nil {
		return "", err
	}
	return "Only when tests fail, overwrite the fixed run-owned message file " + fmt.Sprintf("%q", messageFilePath) + " with only the actionable investigation findings, then run this exact review findings command without editing it: `" + command + "`. Do not put message text in the command. This command targets the review thread, not progress.txt. Do not edit the review file directly or use the progress command for findings.", nil
}

func BuildAddMessageFileCommand(filePath, messageFilePath string, sender Sender, commandPath string) (string, error) {
	if _, err := ParseSender(string(sender)); err != nil {
		return "", err
	}
	arguments := []string{commandPath, "add-message", "-file", filePath, "-message-file", messageFilePath, "-from", string(sender)}
	return formatCommandForShell(arguments, hostCommandShell()), nil
}

func hostCommandShell() CommandShell {
	if runtime.GOOS == "windows" {
		return CommandShellPowerShell
	}
	return CommandShellPOSIX
}

func formatCommand(arguments []string) string {
	return formatCommandForShell(arguments, hostCommandShell())
}

func formatCommandForShell(arguments []string, shell CommandShell) string {
	quoted := make([]string, len(arguments))
	for i, argument := range arguments {
		switch shell {
		case CommandShellPowerShell:
			quoted[i] = "'" + strings.ReplaceAll(argument, "'", "''") + "'"
		default:
			quoted[i] = "'" + strings.ReplaceAll(argument, "'", "'\"'\"'") + "'"
		}
	}
	if shell == CommandShellPowerShell {
		return "& " + strings.Join(quoted, " ")
	}
	return strings.Join(quoted, " ")
}
