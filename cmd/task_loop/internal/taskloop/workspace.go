package taskloop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	workspaceStateDirectory = ".task-loop"
	issueDeltaFilename      = "issue-owned.diff"
)

type WorkspaceBaseline struct {
	RepositoryRoot           string
	RunDirectory             string
	StateDirectory           string
	ObjectDirectory          string
	AlternateObjectDirectory string
	IndexPath                string
	IndexTree                string
	WorktreeTree             string
	gitRunner                workspaceGitRunner
}

type IssueDelta struct {
	Path string
}

type workspaceGitRunner func(context.Context, []string, string, ...string) (string, error)

func CaptureWorkspaceBaseline(ctx context.Context, repositoryRoot, runDirectory string) (WorkspaceBaseline, error) {
	return captureWorkspaceBaseline(ctx, repositoryRoot, runDirectory, runGit)
}

func captureWorkspaceBaseline(ctx context.Context, repositoryRoot, runDirectory string, git workspaceGitRunner) (WorkspaceBaseline, error) {
	root, err := canonicalRepositoryRoot(repositoryRoot)
	if err != nil {
		return WorkspaceBaseline{}, err
	}
	run, err := canonicalExistingPathWithin(filepath.Join(root, ".scratch"), runDirectory)
	if err != nil {
		return WorkspaceBaseline{}, err
	}
	if filepath.Dir(run) != filepath.Join(root, ".scratch") {
		return WorkspaceBaseline{}, fmt.Errorf("run directory must be one feature directory below .scratch: %s", runDirectory)
	}
	if err := ensureGitRepositoryRoot(ctx, root, git); err != nil {
		return WorkspaceBaseline{}, err
	}

	stateRoot := filepath.Join(run, workspaceStateDirectory)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return WorkspaceBaseline{}, fmt.Errorf("create workspace state root: %w", err)
	}
	redirect, err := filesystemPathRedirect(stateRoot)
	if err != nil {
		return WorkspaceBaseline{}, fmt.Errorf("inspect workspace state root: %w", err)
	}
	if redirect {
		return WorkspaceBaseline{}, fmt.Errorf("workspace state root must not be a filesystem redirect: %s", stateRoot)
	}
	stateRoot, err = canonicalExistingPathWithin(run, stateRoot)
	if err != nil {
		return WorkspaceBaseline{}, err
	}
	state, err := os.MkdirTemp(stateRoot, "run-")
	if err != nil {
		return WorkspaceBaseline{}, fmt.Errorf("create isolated workspace state: %w", err)
	}
	objects := filepath.Join(state, "objects")
	if err := os.Mkdir(objects, 0o700); err != nil {
		_ = os.RemoveAll(state)
		return WorkspaceBaseline{}, fmt.Errorf("create private git object directory: %w", err)
	}

	alternateObjects, err := gitPath(ctx, root, "objects", true, git)
	if err != nil {
		_ = os.RemoveAll(state)
		return WorkspaceBaseline{}, err
	}
	indexPath, err := gitPath(ctx, root, "index", false, git)
	if err != nil {
		_ = os.RemoveAll(state)
		return WorkspaceBaseline{}, err
	}
	baseline := WorkspaceBaseline{
		RepositoryRoot:           root,
		RunDirectory:             run,
		StateDirectory:           state,
		ObjectDirectory:          objects,
		AlternateObjectDirectory: alternateObjects,
		IndexPath:                indexPath,
		gitRunner:                git,
	}
	baseline.IndexTree, err = baseline.snapshotIndexTree(ctx)
	if err == nil {
		baseline.WorktreeTree, err = baseline.snapshotWorktreeTree(ctx)
	}
	if err != nil {
		_ = os.RemoveAll(state)
		return WorkspaceBaseline{}, err
	}
	return baseline, nil
}

func (baseline WorkspaceBaseline) PrepareDelta(ctx context.Context) (IssueDelta, error) {
	if baseline.IndexTree == "" || baseline.WorktreeTree == "" {
		return IssueDelta{}, fmt.Errorf("workspace baseline is missing")
	}
	root, run, state, objects, alternate, index, err := baseline.validatedPaths()
	if err != nil {
		return IssueDelta{}, err
	}
	baseline.RepositoryRoot = root
	baseline.RunDirectory = run
	baseline.StateDirectory = state
	baseline.ObjectDirectory = objects
	baseline.AlternateObjectDirectory = alternate
	baseline.IndexPath = index

	currentIndexTree, err := baseline.snapshotIndexTree(ctx)
	if err != nil {
		return IssueDelta{}, err
	}
	currentWorktreeTree, err := baseline.snapshotWorktreeTree(ctx)
	if err != nil {
		return IssueDelta{}, err
	}
	indexDelta, err := baseline.diffTrees(ctx, baseline.IndexTree, currentIndexTree)
	if err != nil {
		return IssueDelta{}, fmt.Errorf("build issue-owned index delta: %w", err)
	}
	worktreeDelta, err := baseline.diffTrees(ctx, baseline.WorktreeTree, currentWorktreeTree)
	if err != nil {
		return IssueDelta{}, fmt.Errorf("build issue-owned worktree delta: %w", err)
	}
	deltaPath := filepath.Join(state, issueDeltaFilename)
	if err := os.WriteFile(deltaPath, renderLayeredDelta(indexDelta, worktreeDelta), 0o600); err != nil {
		return IssueDelta{}, fmt.Errorf("write issue-owned review delta: %w", err)
	}
	return IssueDelta{Path: deltaPath}, nil
}

func (baseline WorkspaceBaseline) Cleanup() error {
	if baseline.StateDirectory == "" {
		return nil
	}
	_, _, state, err := baseline.validatedStatePaths()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(state); err != nil {
		return fmt.Errorf("remove isolated workspace state: %w", err)
	}
	return nil
}

func (baseline WorkspaceBaseline) snapshotIndexTree(ctx context.Context) (string, error) {
	if _, err := os.Stat(baseline.IndexPath); err == nil {
		indexContent, err := os.ReadFile(baseline.IndexPath)
		if err != nil {
			return "", fmt.Errorf("read real git index: %w", err)
		}
		indexPath, cleanup, err := baseline.privateIndex()
		if err != nil {
			return "", err
		}
		defer cleanup()
		if err := os.WriteFile(indexPath, indexContent, 0o600); err != nil {
			return "", fmt.Errorf("copy real git index: %w", err)
		}
		tree, err := baseline.runGit(ctx, baseline.gitEnvironment(indexPath), "write-tree")
		if err != nil {
			return "", fmt.Errorf("snapshot real git index: %w", err)
		}
		return strings.TrimSpace(tree), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect real git index: %w", err)
	}
	return baseline.snapshotEmptyIndexTree(ctx)
}

func (baseline WorkspaceBaseline) snapshotEmptyIndexTree(ctx context.Context) (string, error) {
	indexPath, cleanup, err := baseline.privateIndex()
	if err != nil {
		return "", err
	}
	defer cleanup()
	environment := baseline.gitEnvironment(indexPath)
	if _, err := baseline.runGit(ctx, environment, "read-tree", "--empty"); err != nil {
		return "", fmt.Errorf("initialize empty private git index: %w", err)
	}
	tree, err := baseline.runGit(ctx, environment, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write empty index snapshot tree: %w", err)
	}
	return strings.TrimSpace(tree), nil
}

func (baseline WorkspaceBaseline) snapshotWorktreeTree(ctx context.Context) (string, error) {
	indexPath, cleanup, err := baseline.privateIndex()
	if err != nil {
		return "", err
	}
	defer cleanup()
	environment := baseline.gitEnvironment(indexPath)
	if _, err := baseline.runGit(ctx, environment, "rev-parse", "--verify", "HEAD"); err == nil {
		if _, err := baseline.runGit(ctx, environment, "read-tree", "HEAD"); err != nil {
			return "", fmt.Errorf("initialize private git index: %w", err)
		}
	} else if ctx.Err() != nil {
		return "", ctx.Err()
	} else if _, err := baseline.runGit(ctx, environment, "read-tree", "--empty"); err != nil {
		return "", fmt.Errorf("initialize empty private git index: %w", err)
	}
	stateRoot := filepath.Dir(baseline.StateDirectory)
	stateRelative, err := filepath.Rel(baseline.RepositoryRoot, stateRoot)
	if err != nil {
		return "", fmt.Errorf("locate workspace state root: %w", err)
	}
	exclude := ":(exclude)" + filepath.ToSlash(stateRelative) + "/**"
	if _, err := baseline.runGit(ctx, environment, "add", "-A", "--", ".", exclude); err != nil {
		return "", fmt.Errorf("snapshot effective working tree: %w", err)
	}
	tree, err := baseline.runGit(ctx, environment, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write effective working tree snapshot: %w", err)
	}
	return strings.TrimSpace(tree), nil
}

func (baseline WorkspaceBaseline) privateIndex() (string, func(), error) {
	indexFile, err := os.CreateTemp(baseline.StateDirectory, "index-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("reserve private git index: %w", err)
	}
	indexPath := indexFile.Name()
	if err := indexFile.Close(); err != nil {
		_ = os.Remove(indexPath)
		return "", func() {}, fmt.Errorf("close private git index: %w", err)
	}
	if err := os.Remove(indexPath); err != nil {
		return "", func() {}, fmt.Errorf("prepare private git index: %w", err)
	}
	return indexPath, func() { _ = os.Remove(indexPath) }, nil
}

func (baseline WorkspaceBaseline) diffTrees(ctx context.Context, before, after string) ([]byte, error) {
	output, err := baseline.runGit(
		ctx,
		baseline.gitEnvironment(""),
		"diff", "--binary", "--no-ext-diff", "--no-renames", before, after, "--",
	)
	return []byte(output), err
}

func (baseline WorkspaceBaseline) runGit(ctx context.Context, environment []string, arguments ...string) (string, error) {
	git := baseline.gitRunner
	if git == nil {
		git = runGit
	}
	return git(ctx, environment, baseline.RepositoryRoot, arguments...)
}

func (baseline WorkspaceBaseline) gitEnvironment(indexPath string) []string {
	environment := replaceProcessEnvironment(os.Environ(), "GIT_OBJECT_DIRECTORY", baseline.ObjectDirectory)
	environment = replaceProcessEnvironment(environment, "GIT_ALTERNATE_OBJECT_DIRECTORIES", baseline.AlternateObjectDirectory)
	if indexPath != "" {
		environment = replaceProcessEnvironment(environment, "GIT_INDEX_FILE", indexPath)
	}
	return environment
}

func (baseline WorkspaceBaseline) validatedPaths() (string, string, string, string, string, string, error) {
	root, run, state, err := baseline.validatedStatePaths()
	if err != nil {
		return "", "", "", "", "", "", err
	}
	objects, err := canonicalExistingPathWithin(state, baseline.ObjectDirectory)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	if filepath.Dir(objects) != state || filepath.Base(objects) != "objects" {
		return "", "", "", "", "", "", fmt.Errorf("invalid private git object directory: %s", objects)
	}
	alternate, err := canonicalExistingFilesystemPath(baseline.AlternateObjectDirectory)
	if err != nil {
		return "", "", "", "", "", "", fmt.Errorf("resolve real git object directory: %w", err)
	}
	index := filepath.Clean(baseline.IndexPath)
	return root, run, state, objects, alternate, index, nil
}

func (baseline WorkspaceBaseline) validatedStatePaths() (string, string, string, error) {
	root, err := canonicalRepositoryRoot(baseline.RepositoryRoot)
	if err != nil {
		return "", "", "", err
	}
	run, err := canonicalExistingPathWithin(filepath.Join(root, ".scratch"), baseline.RunDirectory)
	if err != nil {
		return "", "", "", err
	}
	stateRootCandidate := filepath.Join(run, workspaceStateDirectory)
	redirect, err := filesystemPathRedirect(stateRootCandidate)
	if err != nil {
		return "", "", "", err
	}
	if redirect {
		return "", "", "", fmt.Errorf("workspace state root must not be a filesystem redirect: %s", stateRootCandidate)
	}
	stateRoot, err := canonicalExistingPathWithin(run, stateRootCandidate)
	if err != nil {
		return "", "", "", err
	}
	stateCandidate := filepath.Clean(baseline.StateDirectory)
	if filepath.Dir(stateCandidate) != stateRoot || !strings.HasPrefix(filepath.Base(stateCandidate), "run-") {
		return "", "", "", fmt.Errorf("invalid isolated workspace state directory: %s", stateCandidate)
	}
	redirect, err = filesystemPathRedirect(stateCandidate)
	if err != nil {
		return "", "", "", err
	}
	if redirect {
		return "", "", "", fmt.Errorf("isolated workspace state must not be a filesystem redirect: %s", stateCandidate)
	}
	state, err := canonicalExistingPathWithin(stateRoot, stateCandidate)
	if err != nil {
		return "", "", "", err
	}
	if !pathsEqual(state, stateCandidate) {
		return "", "", "", fmt.Errorf("invalid isolated workspace state directory: %s", state)
	}
	return root, run, state, nil
}

func renderLayeredDelta(indexDelta, worktreeDelta []byte) []byte {
	var output bytes.Buffer
	output.WriteString("TASK-LOOP ISSUE-OWNED LAYERED DELTA\n")
	if len(indexDelta) > 0 && bytes.Equal(indexDelta, worktreeDelta) {
		output.WriteString("\n=== INDEX + EFFECTIVE WORKTREE (identical) ===\n")
		output.Write(indexDelta)
		return output.Bytes()
	}
	writeDeltaLayer(&output, "INDEX (staged state)", indexDelta)
	writeDeltaLayer(&output, "EFFECTIVE WORKTREE (tracked and untracked content)", worktreeDelta)
	return output.Bytes()
}

func writeDeltaLayer(output *bytes.Buffer, name string, delta []byte) {
	fmt.Fprintf(output, "\n=== %s ===\n", name)
	if len(delta) == 0 {
		output.WriteString("(no issue-owned changes in this layer)\n")
		return
	}
	output.Write(delta)
}

func gitPath(ctx context.Context, root, name string, mustExist bool, git workspaceGitRunner) (string, error) {
	value, err := git(ctx, os.Environ(), root, "rev-parse", "--git-path", name)
	if err != nil {
		return "", fmt.Errorf("resolve git %s path: %w", name, err)
	}
	path := strings.TrimSpace(value)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if mustExist {
		resolved, err := canonicalExistingFilesystemPath(path)
		if err != nil {
			return "", fmt.Errorf("resolve git %s path: %w", name, err)
		}
		return resolved, nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve git %s path: %w", name, err)
	}
	return filepath.Clean(absolute), nil
}

func ensureGitRepositoryRoot(ctx context.Context, root string, git workspaceGitRunner) error {
	output, err := git(ctx, os.Environ(), root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve git repository: %w", err)
	}
	top, err := canonicalRepositoryRoot(strings.TrimSpace(output))
	if err != nil {
		return err
	}
	if !pathsEqual(top, root) {
		return fmt.Errorf("configured root %s is not the git repository root %s", root, top)
	}
	return nil
}

func runGit(ctx context.Context, environment []string, root string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, arguments...)...)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s failed: %w: %s", arguments[0], err, detail)
	}
	return stdout.String(), nil
}

func replaceProcessEnvironment(environment []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, key+"="+value)
}
