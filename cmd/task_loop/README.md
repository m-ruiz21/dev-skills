# task-loop

A small, installable Python CLI that coordinates one PRD issue at a time
through triage, TDD development, testing, and multi-dimensional review.

## Install

```bash
make build-task-loop   # from the repository root; adds bin/task-loop to PATH
```

or, as a standard Python package:

```bash
pip install -e cmd/task_loop
```

The package includes the durable threaded-message implementation used by the
workflow and exposes it through `task-loop add-message`.

### Global agent installation

For an agent to run `task-loop` from any repository, install its source
runner into a directory on the user `PATH`. From this repository's root:

```bash
make build-task-loop
mkdir -p "$HOME/.local/bin"
ln -sf "$(pwd)/bin/task-loop" "$HOME/.local/bin/task-loop"
```

Persist the binary directory in the environment used to launch the agent:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Put that export in the appropriate shell startup file, then restart the
terminal or agent process. Confirm that a fresh shell can resolve the command outside this checkout:

```bash
cd "$HOME"
command -v task-loop
task-loop --help
```

Spawned development, testing, and review agents use the globally resolvable
`task-loop add-message` command to write workflow messages. Because child
agents inherit their parent's environment, start the agent only after the
command resolves on `PATH`.

## Usage

```bash
task-loop                                # list available .scratch/<feature>/PRD.md paths
task-loop .scratch/task-loop/PRD.md      # select a PRD, default cap of 10 iterations
task-loop .scratch/task-loop/PRD.md --max-iterations 5
task-loop add-message -file review/01-issue.md -message "Starting work." -from developer
task-loop add-message -file review/01-issue.md -message "Done." -from reviewer -to 1
```

This first slice selects a PRD and initializes the iteration budget through a
testable orchestration interface (`task_loop.orchestration.start_run`). A
second slice adds a testable triage interface
(`task_loop.triage.select_issue`): given a selected PRD, it chooses exactly
one issue from the feature's issue queue and `progress.txt`, prioritizing
`status: review` issues ahead of ordinary `ready-for-agent` work, respecting
`blocked-by` dependencies, and validating that the triage response is exactly
one in-scope issue reference. It also ensures the selected issue has a
`review/<issue-name>.md` document, creating it when absent and leaving it
untouched when present.

A third slice adds a testable development interface
(`task_loop.development.run_development`): the selected issue, PRD,
`progress.txt`, and its review thread are passed to a replaceable development
("TDD") agent, along with instructions requiring strict test-first vertical
slices and prohibiting direct edits to the review document. The raw response
is strictly validated -- only `[completed] message`, `[needs-clarity]
message`, and `[partial] message` (each with a non-empty message) are
accepted; unsupported outcomes, malformed brackets, leading prose, multiple
outcomes, and empty messages all fail explicitly
(`MalformedDevelopmentResponseError`) without touching the review document. A
valid response is appended as a `developer` message by reusing
`task_loop.messages.add_message` -- the same interface `task-loop add-message`
uses -- rather than duplicating file-append behavior. A `[completed]`
response advances the same issue to the testing phase without re-running
triage. A development agent process failure (`DevelopmentAgentError`) is
always distinguishable from a malformed-but-successful response. The
production default agent (`task_loop.development.default_development_agent`)
invokes the Copilot CLI as a subprocess (`copilot --yolo -p <prompt>`),
matching the existing `cmd/ralph` agent-invocation pattern; tests inject a
scripted agent instead.

A fourth slice resolves `[needs-clarity]` requests interactively
(`task_loop.clarity.resolve_clarity`, wired into the public `task-loop` CLI).
When development reports `[needs-clarity] message`, the CLI prints the
request and prompts through a replaceable user-input adapter
(`task_loop.clarity.UserInputAgent`) -- the production
`default_user_input_agent` reads from the real terminal via `input()` and
reports unavailable input (EOF) or an interrupt (Ctrl-C) as cancellation
instead of crashing. A non-empty answer is appended as a `user` reply to the
request's thread through `task_loop.messages.add_message` (never by editing
the review document directly), and the same issue is retried against
development -- without re-running triage -- so the retried agent sees the
answer already threaded into its review context. Cancelled, unavailable, or
blank input raises `ClarityCancelledError` and the CLI stops with a non-zero
exit, leaving the issue unmarked as complete rather than fabricating an
answer. Because triage and development can otherwise loop on repeated
clarity requests, retries are bounded by the run's existing
`--max-iterations` budget: exhausting it while still awaiting clarity also
stops the CLI with a non-zero exit.

A fifth slice handles `[partial] message`: the CLI (`task_loop.cli.main`)
treats it as automatic progress, printing `Developer (partial): <message>`
and retrying the very same issue on the next iteration -- without
re-running triage -- so the retried agent reads the just-appended message
back from the accumulated review context. `[partial]` shares the exact same
iteration loop (and therefore the same `--max-iterations` budget) as
`[needs-clarity]`; neither outcome keeps a separate counter, so a run that
mixes partial progress and clarity requests is still bounded by one shared
cap, and exhausting it while an issue is still only partially complete stops
the CLI with a non-zero exit and an actionable message, leaving every
recorded developer message in place. Partial work is never interpreted as
completion and never reports `Phase: testing` -- only `[completed]` does.
`[partial]` can be retried indefinitely (bounded by the shared budget)
before an agent eventually reports `[completed]`.

A sixth slice adds a testable testing interface
(`task_loop.testing.run_testing`), wired into the public `task-loop` CLI
immediately after a `[completed]` development result -- never before, and
never re-running triage. The selected issue's PRD, `progress.txt`, and
review thread are passed to a replaceable testing agent
(`task_loop.testing.TestingAgent`) along with instructions to run the
repository's existing relevant tests and, on failure, to investigate and
record findings as a `reviewer` message through `task-loop add-message`
before responding. The raw response is strictly validated: only exactly
`[success]` or `[failure]` (after stripping surrounding whitespace) is
accepted -- extra prose, unknown outcomes, empty output, and multiple
bracketed markers all fail explicitly (`MalformedTestingResponseError`).
Because there is no message to append on the CLI's behalf for this outcome
(unlike development's outcomes), a `[failure]` response is only accepted
when the review document actually gained a new `reviewer` message during
the agent's run; if it did not, `run_testing` raises
`MissingInvestigationFindingsError` and the CLI stops with a non-zero exit
rather than silently retrying without evidence. A `[success]` response
advances the run to `Phase: review` and returns; a `[failure]` response with
recorded findings retries development for the very same issue on the next
iteration, so the retried agent reads the reviewer's findings back from the
accumulated review context. A completed development pass and its testing
result belong to the same iteration that produced them -- only a failed
test starts the next iteration -- so `[partial]`, `[needs-clarity]`, and
failed-test retries all continue to share one `--max-iterations` budget. A
testing agent process failure (`TestingAgentError`) is always
distinguishable from a malformed-but-successful response. The production
default agent (`task_loop.testing.default_testing_agent`) invokes the
Copilot CLI as a subprocess (`copilot --yolo -p <prompt>`), mirroring
`default_development_agent`'s replaceable process seam; tests inject a
scripted agent instead. Automated review scoring (`review-diff`) remains out
of scope for this slice -- reaching `Phase: review` simply hands control
back for that later slice to pick up.

A seventh slice adds review scoring (`task_loop.review`), wired into the
public `task-loop` CLI immediately after a `[success]` testing result --
never before, and never when tests fail. A replaceable review agent
(`task_loop.review.ReviewAgent`) is instructed to use the `review-diff`
skill against the staged diff and to respond with ONLY the exact
structured artifact `skills/review-diff/SKILL.md` documents: an object
with a `schemaVersion` string, a `runId` string, a `dimensions` list
(exactly one entry per required dimension -- `security`, `testAdequacy`,
`planAlignment`, `codeQuality`, `architecture` -- each with a `dimension`
name, an integer 0-100 `grade`, and non-empty `evidence`), and a
top-level `findings` list (each with a unique `id`, a `dimension`, a
`severity`, a `status`, a `summary`, and an optional `location`); the
agent's own prose, and its subjective per-dimension `grade`s, never
decide pass or fail. The response is strictly parsed as JSON and then
validated (`score_review`) against that real skill schema -- not a
parallel one invented for task-loop: a non-object payload, a
missing/malformed `schemaVersion` or `runId`, a missing, duplicate, or
unknown dimension, empty or non-list evidence, an out-of-range or
non-integer `grade`, a non-list `findings`, or a finding with a
malformed `id` (missing, non-string, empty, or duplicated), `dimension`,
`severity`, `status`, `summary`, or `location` all raise
`MalformedReviewDataError` explicitly. Only `status: "open"` findings
count toward the load; `addressed`, `waived`, and `invalid` findings are
valid but always contribute zero. `info` findings are accepted but never
increase the load. The skill's own `blocker` severity (part of its
documented six-value normalization scale, alongside `info`, `low`,
`medium`, `high`, and `critical`) is accepted and conservatively weighted
the same as `critical` (20) rather than rejected -- the raw `blocker`
count is still reported on `DimensionScore.counts` for visibility. Each
dimension's score is `100e^(-lambda * L)` with `lambda = -ln(.8) / 20` and
`L = low + medium*5 + high*10 + critical*20 + blocker*20`, matching the
PRD's formula exactly; a dimension passes at 90 or above, and a
dimension's own `grade` never affects this -- only open findings and the
formula do. `render_review` prints every dimension with its numeric
score, a `[#...-]`-style progress bar, and an unambiguous `passed`/`failed`
tag -- colored green/red with ANSI escapes when the CLI's stdout is a
TTY, and the plain uppercase word `PASSED`/`FAILED` otherwise, so the
report stays readable without color support. The overall verdict passes
only when every dimension passes; the CLI prints the rendered report and
exits `0` on an overall pass, or non-zero with an explicit "review did
not pass" message on failure. A review agent process failure
(`ReviewAgentError`) is always distinguishable from malformed review
data. The production default agent
(`task_loop.review.default_review_agent`) invokes the Copilot CLI as a
subprocess (`copilot --yolo -p <prompt>`), mirroring the development and
testing agents' replaceable process seam; tests inject a scripted agent
instead. Automatically retrying development after a failing review, and
the rest of the loop's terminal lifecycle, are a later slice's
responsibility -- this slice only calculates and reports the verdict.

**Correction (post-close):** parent verification found this slice's
original implementation had invented a parallel schema -- a custom object
mapping dimension names directly to finding lists, with `open`/`resolved`
statuses and a rejected `blocker` severity -- instead of consuming the
`review-diff` skill's actual structured artifact
(`schemaVersion`/`runId`/`dimensions`/top-level `findings`, with
`open`/`addressed`/`waived`/`invalid` statuses). The adapter contract and
scorer above were corrected via strict TDD (RED tests against the real
skill artifact from `SKILL.md`'s own example, confirmed failing against
the old scorer, then a GREEN reimplementation) to consume that real
schema directly, with `blocker` conservatively mapped to `critical`'s
weight instead of rejected, and `addressed`/`waived`/`invalid` findings
excluded from the load exactly like `resolved` was before. The nonstandard
`count` extension was dropped entirely: the skill's artifact has none, and
each finding object is exactly one occurrence.

An eighth and final slice completes the bounded loop's termination
contract (`task_loop.orchestration.run_issue_loop`, the deeper
orchestration seam the CLI's control flow was extracted into so it stays
testable). A failing review no longer stops the run: `task_loop.review`
composes an actionable reviewer message straight from the validated
structured artifact -- every failing dimension's score plus its own open
findings (id, severity, summary, location), so even a single sparse
finding still produces an actionable summary -- and appends it through
`task-loop add-message` before the same issue retries development on the
next iteration, exactly like `[partial]`, `[needs-clarity]`, and a failed
test run. All four retry causes share the one `--max-iterations` budget;
triage still runs only once for the retained issue across every retry.
When every review dimension finally passes, the CLI prints the rendered
report, reports the work as ready for external human review, and stops
immediately -- it never closes the issue, collects approval, or selects
another issue. Exhausting the budget instead prints one clear non-success
message naming the selected issue and the latest failure reason (the
partial message, the clarity question, a test-failure note, or the
failing review dimensions and their scores), and leaves the review
document and `progress.txt` exactly as recorded.
