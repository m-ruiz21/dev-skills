# dev-loop

A Claude Code plugin that provides a PRD-driven development workflow with automated triage, coding, and review loops.

## Skills Included

| Skill | Description |
|-------|-------------|
| `/azure-devops` | Query Azure DevOps PR stats (approved/authored/reviewed) across orgs, projects, and date ranges |
| `/develop-task` | Find and implement top-priority tasks from the local issue tracker |
| `/handoff` | Compact the current conversation into a handoff document for another agent |
| `/triage` | Triage issues through a state machine driven by triage roles |
| `/review-diff` | Multi-dimensional review of staged changes |
| `/tdd` | Test-driven development with red-green-refactor loop |
| `/to-issues` | Break a plan/PRD into independently-grabbable issues |
| `/to-prd` | Turn conversation context into a PRD |
| `/diagnose` | Disciplined diagnosis loop for hard bugs |
| `/prototype` | Build throwaway prototypes to flush out designs |
| `/grill-me` | Interview relentlessly about a plan or design |
| `/grill-with-docs` | Grill against existing domain model and docs |
| `/improve-codebase-architecture` | Find deepening opportunities in a codebase |
| `/setup-repo` | Configure repo for the dev-loop workflow |
| `/write-a-skill` | Create new agent skills |
| `/zoom-out` | Zoom out to module map + callers |

## The `task-loop` CLI

`task-loop` is the successor to `ralph` and the command you use to **start**
new work: a small, installable Python CLI that coordinates one PRD issue
at a time through triage, TDD development, testing, and multi-dimensional
review. It keeps one selected issue across automatic retries and enforces a
hard iteration budget that defaults to 10.

```bash
make build-task-loop      # symlinks bin/task-loop (added to PATH by the plugin)
task-loop                                # no PRD given: list available PRDs and exit
task-loop .scratch/task-loop/PRD.md      # select a PRD, default cap of 10 iterations
task-loop .scratch/task-loop/PRD.md --max-iterations 5
```

### Install globally for agents

Build the Python CLI, then link it into a user-level binary directory:

```bash
make build-task-loop
mkdir -p "$HOME/.local/bin"
ln -sf "$(pwd)/bin/task-loop" "$HOME/.local/bin/task-loop"
```

Ensure `~/.local/bin` is on `PATH` in the shell that launches the agent. Add
this to `~/.profile`, `~/.bashrc`, or the equivalent shell configuration:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Restart the terminal or agent process after changing `PATH`, then verify the
commands from a directory outside this repository:

```bash
cd "$HOME"
command -v task-loop
task-loop --help
```

The development, testing, and review agents may invoke
`task-loop add-message` as a subprocess. Agents inherit `PATH` from the
process that starts them, so `command -v task-loop` must succeed before
launching the agent.

Omitting the PRD path prints every `.scratch/<feature>/PRD.md` path in
deterministic order and exits without starting an agent. A missing,
unreadable, or non-`PRD.md` path fails with an actionable message, and
`--max-iterations` rejects zero, negative, or malformed values. See
[`cmd/task_loop/README.md`](cmd/task_loop/README.md) for the CLI's source
layout and test suite (`make test-task-loop`).

### Threaded workflow messages

`task-loop add-message` is the supported way for agents and users to
append durable, threaded discussion to a review or progress file -- callers
never edit those documents directly.

```bash
task-loop add-message -file review/01-issue.md -message "Starting work." -from developer
task-loop add-message -file review/01-issue.md -message "Sounds good." -from reviewer -to 1
```

Omitting `-to` creates the file if needed and starts a new, stable thread in
the format `-- Thread <id>` / `[<role>] - <timestamp>` / a blank line / the
message; the command prints `Thread <id>` so callers can capture it for
replies. Passing `-to <thread-id>` appends a reply associated with that
thread and fails for unknown thread ids. `-from` must be one of `user`,
`reviewer`, or `developer`. Appends are serialized with an exclusive file
lock so concurrent or interrupted writes cannot silently truncate the
destination file, and prior content is always preserved byte-for-byte. See
[`cmd/task_loop/README.md`](cmd/task_loop/README.md) for the CLI's source
layout and test suite (`make test-task-loop`).

## The `ralph` CLI

`ralph` remains available for resuming, approving, rejecting, and inspecting
runs it already started; start new work with `task-loop` instead.

An orchestrator that runs one selected PRD issue at a time through a typed workflow:

```
PRD selection → issue selection → development → gates → review → human approval → repair/revalidation
```

Each iteration has a stable run ID, an explicit phase and attempt number, and
invokes Copilot through a replaceable agent adapter. Workflow success is based
on the process exit status and typed agent outcome, not text emitted by the
agent. After development, repository commands run as typed objective gates;
every required gate must pass before the workflow completes. Detailed gate
output is retained under `.ralph/runs/<run-id>/artifacts/gates/`. Review agents
write a versioned JSON artifact under `.ralph/runs/<run-id>/artifacts/review/`;
Ralph validates it, calculates the quality decision in code, and renders the
human-readable, attempt-specific `review.md`. Agent output and Markdown never
control advancement.
Failed required validations and unresolved review evidence are converted into a
typed repair request. Repair runs as a distinct agent operation, then Ralph
reruns gates and structured review for the same run and selected issue.
Every meaningful transition also appends a versioned event and atomically
replaces `.ralph/runs/<run-id>/checkpoint.json`. Checkpoints include the
selected issue, attempts, gates, grades, findings, artifacts, and Git repository
identity. Resume verifies the repository root, branch, HEAD, staged changes, and
relevant working-tree changes before running anything; mismatches are reported
without resetting, stashing, or overwriting user work.
After gates and review pass, Ralph persists an `AwaitingApproval` checkpoint and
returns control. The approval request records the run, issue, exact checkpoint
identity, gate results, grades, findings, and evaluated artifact references.
Approval advances that checkpoint without rerunning completed work. Rejection
or structured feedback becomes typed repair input bound to the same checkpoint
and artifacts; Ralph never scrapes feedback from review Markdown.
An approval resumes into an idempotent `ClosingIssue` phase. Ralph revalidates
the recorded gates, quality decision, actionable findings, required hooks, and
the exact approval before updating the local Markdown issue to `status: closed`
and moving it under `issues/closed/`. Closure, dependency unblocking, and final
artifact writes are safe to replay after interruption. Failed, cancelled,
rejected, and awaiting-approval runs leave the issue open.

Configure gates in `.ralph.json` at the repository root:

```json
{
  "gates": [
    {
      "id": "tests",
      "command": "dotnet test",
      "required": true,
      "timeoutSeconds": 300,
      "concurrent": true
    }
  ],
  "qualityPolicy": {
    "version": "1.0",
    "dimensionWeights": {
      "security": 20,
      "testAdequacy": 20,
      "planAlignment": 20,
      "codeQuality": 20,
      "architecture": 20
    },
    "minimumOverallGrade": 80,
    "minimumDimensionGrades": {
      "security": 70,
      "testAdequacy": 70,
      "planAlignment": 70,
      "codeQuality": 70,
      "architecture": 70
    }
  },
  "repairPolicy": {
    "maximumAttempts": 3,
    "maximumElapsedSeconds": 1800,
    "repetitionThreshold": 2
  },
  "hooks": [
    {
      "id": "pre-completion-policy",
      "event": "completion",
      "command": "./scripts/ralph-policy",
      "mode": "requiredBlocking",
      "timeoutSeconds": 60,
      "maximumRetries": 1
    },
    {
      "id": "telemetry",
      "event": "checkpointPersisted",
      "command": "./scripts/ralph-notify",
      "mode": "optionalObservational"
    }
  ]
}
```

Concurrent gates run together when adjacent in the configuration. A sequential
gate waits for prior concurrent gates and completes before later gates start.
If `qualityPolicy` is omitted, the values shown above are used. Open `critical`
or `blocker` findings always fail quality review regardless of the weighted
grade. Missing, incomplete, malformed, or unsupported review data fails the run
explicitly.
Repair attempts include the initial development attempt. Ralph stops with a
typed terminal failure when the maximum attempt count or elapsed-time limit is
reached. It also fingerprints failed gates, open findings, and failing quality
evidence; an identical fingerprint recurring at the configured threshold trips
the no-progress circuit breaker and reports the recurring evidence.

### Usage

Start new work with `task-loop` (see above). The subcommands below resume,
inspect, and approve/reject runs that a prior invocation already started.

```bash
make build
ralph list-runs         # list complete and incomplete durable runs
ralph resume <run-id>   # resume only the explicitly selected run
ralph approve <run-id> <request-id>
ralph reject <run-id> <request-id> "reason for rejection"
ralph feedback <run-id> <request-id> <feedback-id> "actionable feedback"
ralph cancel <run-id>   # mark an incomplete run cancelled; leave its issue open
```

Run event history is append-only at
`.ralph/runs/<run-id>/events.jsonl`. A resume continues from the latest
successfully checkpointed operation, so completed development, gate, review,
and repair work is not repeated. Unsupported or corrupt checkpoints, invalid
phases, missing required artifacts, and repository mismatches fail explicitly.
An awaiting-approval run requires an exact structured response: wrong run or
request identifiers, missing feedback, and approval in another phase are
explicit errors.

Completed runs publish deterministic projections under
`.ralph/runs/<run-id>/artifacts/final/`: `review.md` combines structured gates,
grades, findings, feedback, approval, and workflow state; `progress.md` is
rendered solely from the ordered event history; and `summary.md` reports the
run ID, issue, attempts, gate outcomes, grades, blocking findings, approval,
closure location, and artifact paths. These documents never control workflow
transitions.

Lifecycle hook events are `runStarted`, `development`, `gates`, `review`,
`checkpointPersisted`, `approval`, `issueClosure`, `failure`, `cancellation`,
and `completion`. Ralph writes a structured JSON context to each command's
standard input, including run, phase, issue, attempt, gate/review summaries,
and artifact references. Required blocking hooks must write JSON such as
`{"decision":"continue","message":"policy passed","diagnostics":[]}` and may
instead decide `block`, `retry`, or `request-input`. Optional observational
hooks may write no output and can never alter a transition. All failures are
recorded; required failures block, while optional failures are surfaced as
warnings. Successfully recorded effects are not repeated on resume.

When a hook requests input, Ralph persists the request and exits safely:

```bash
ralph hook-input <run-id> <request-id> "requested value"
```

## Installation

### As a Claude Code plugin (recommended)

```bash
# From the marketplace
/plugin marketplace add m-ruiz21/skills
/plugin install dev-loop

# Or test locally
claude --plugin-dir /path/to/this/repo
```

### Build from source

```bash
git clone https://github.com/m-ruiz21/skills.git
cd skills
make build
```

To make `task-loop` available to agents outside an active
plugin session, follow [Install globally for agents](#install-globally-for-agents).

## Plugin Structure

```
.claude-plugin/plugin.json   # Plugin manifest
skills/                      # All skill definitions (SKILL.md + supporting files)
cmd/ralph/                   # .NET source for the ralph orchestrator
cmd/task_loop/                # Python source for the task-loop CLI
bin/                          # Compiled/linked binaries (auto-added to PATH when plugin is active)
Makefile                     # `make build` to install task-loop
```

## How It Works

When installed as a Claude Code plugin:

1. **Skills load automatically** — all 14 skill folders under `skills/` become available as `/slash-commands` in Claude Code.
2. **`bin/` is added to PATH** — after `make build`, the `task-loop` command is callable directly from your terminal while the plugin is active.
3. **`task-loop` starts new work** — it selects a PRD and issue, then spawns Copilot CLI sessions for TDD development, testing, and `review-diff`, retrying actionable failures within the iteration budget. `task-loop add-message` appends durable, threaded discussion to review or progress files.
