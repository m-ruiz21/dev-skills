# task-loop

`task-loop` is a Go CLI that coordinates one PRD issue at a time through
triage, TDD development, testing, and multi-dimensional review. It keeps the
selected issue across bounded retries and stops for external human approval;
it does not close issues or commit changes.

## Development

The module requires Go 1.23 or newer:

```bash
cd cmd/task_loop
go test ./...
go vet ./...
go run ./cmd/task-loop --help
```

The public commands are:

```text
task-loop
task-loop .scratch/<feature>/PRD.md
task-loop .scratch/<feature>/PRD.md --max-iterations 5
task-loop create-issue -request-file <path>
task-loop create-issue -prd .scratch/<feature>/PRD.md (-title <title> | -title-file <path>) -description-file <path> -acceptance-criteria-file <path> [-parent <ref>] [-blocked-by <issue>]...
task-loop add-message -file <path> (-message <text> | -message-file <path>) -from <role> [-to <thread-id>]
```

With no PRD path, an interactive terminal shows a keyboard picker for the
sorted `.scratch/*/PRD.md` files. Use Up/Down or `j`/`k` to move, Page Up/Page
Down or Home/End to scroll, Enter to select, and Escape, `q`, or Ctrl-C to
cancel. Non-interactive callers retain the script-friendly behavior of printing
the sorted paths and exiting.
`add-message` appends locked, durable threads and accepts the roles `user`,
`reviewer`, and `developer`. Human callers can pass `-message`; generated agent
instructions use a fixed run-owned `-message-file`, which is consumed after a
successful append so message content is never interpolated into shell commands.

`create-issue` accepts either `.scratch/<feature>/PRD.md` or its feature
directory. Automated callers use one repository-confined UTF-8 JSON
`-request-file` with fields `prd`, `title`, `description`,
`acceptanceCriteria`, and optional `parent` and `blockedBy`. The request file is
strict: unknown fields and trailing JSON are rejected, the title and each
criterion must be a non-empty single line, and `acceptanceCriteria` must be
non-empty. The executable path and `["create-issue", "-request-file", path]`
should be passed through a structured process API. Shell-only runtimes must
single-quote every argv element, replacing `'` with `''` for PowerShell or
`'"'"'` for POSIX shells; raw values are never interpolated.

Legacy flags remain for direct human use. `-title-file`, `-description-file`,
and `-acceptance-criteria-file` must name repository-confined regular files. A
title file must contain valid UTF-8 and exactly one non-empty line; an optional
UTF-8 BOM and one final LF or CRLF are ignored, and surrounding whitespace on
that line is trimmed. Additional newlines are rejected. Each non-empty
criteria-file line becomes one unchecked criterion.
Generated titles and blockers are quoted YAML scalars, preserving
YAML-significant text.
It creates `issues/` when needed, chooses the next number across open and closed
issues, derives a lowercase ASCII slug from the title, and writes
`<NN>-<slug>.md` with `status: ready-for-agent`, canonical `blocked-by`
frontmatter, and the Parent, What to build, Acceptance criteria, and Blocked by
body sections. `-parent` is optional and `-blocked-by` may be repeated. Every
blocker must already exist under the same feature's `issues/` tree. The command
prints the repository-relative created path and uses exclusive creation rather
than overwriting an existing file.

At issue start, task-loop snapshots both the real index tree and the effective
working tree through private indexes and a unique private Git object store.
Automated review receives only the independently labeled run-start-to-current
index and effective-worktree deltas, including staged deletions, staged content
hidden by later worktree restoration, unstaged edits, and untracked files.
Identical layers are emitted once. Pre-existing work is excluded, private
snapshot objects never enter the repository object database, concurrent runs
use isolated state, and the user's real index is never rewritten.

## Plugin package

Marketplace installation copies repository contents and does not compile
source, so the six release binaries are intentionally tracked:

```text
bin/task-loop/
├── darwin-amd64/task-loop
├── darwin-arm64/task-loop
├── linux-amd64/task-loop
├── linux-arm64/task-loop
├── windows-amd64/task-loop.exe
├── windows-arm64/task-loop.exe
└── SHA256SUMS
```

From the repository root:

```bash
make package-task-loop         # reproducibly rebuild every target and checksums
make verify-task-loop-package  # check the exact file set and SHA-256 values
make verify-task-loop-reproducible # compare two isolated six-target builds
make smoke-task-loop           # run --help and add-message with the native binary
make check-task-loop           # format, tests, vet, package verification, smoke
make verify-task-loop-tracked   # require the exact rebuilt package in Git unchanged
```

Packaging selects the exact Go 1.23.12 toolchain through `GOTOOLCHAIN`
(downloading it when needed), pins baseline architecture levels, clears
inherited Go build tuning, uses `CGO_ENABLED=0`, `-trimpath`, no VCS stamping,
and an empty build ID. Package and reproducibility checks inspect every
binary's Go build information and reject any compiler-version mismatch. Unix
binaries are written as `0755`; Windows binaries and checksums are `0644`. The
output directory is replaced as one complete payload, so stale target binaries
cannot survive a rebuild.

On Windows, Git cannot preserve Unix executable bits from the filesystem.
Regenerate with `make package-task-loop`, verify with
`make verify-task-loop-package`, then stage package modes deterministically with
`git add --chmod=+x bin/task-loop/darwin-*/task-loop bin/task-loop/linux-*/task-loop`
and `git add --chmod=-x bin/task-loop/windows-*/*.exe bin/task-loop/SHA256SUMS`.

`/develop-task` detects Windows, macOS, or Linux and normalizes amd64/arm64
before resolving the matching plugin-relative binary. The running executable
uses `os.Executable()` to provide phase agents with an absolute, shell-quoted
`add-message` command; `/tdd` and `/review-diff` execute that command exactly.

The Python runtime, launchers, installer, and tests were removed after parity
was established. Go under `internal/taskloop` is the sole implementation.
