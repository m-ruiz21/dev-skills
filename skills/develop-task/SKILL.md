---
name: develop-task
description: Run task-loop to complete PRD issues one at a time, record every change in progress.txt, obtain human approval, close approved issues, and continue until the PRD is complete. Use when user says "work", "worker", "pick up tasks", "afk", or "work loop".
---

# Worker

Run the bounded `task-loop` workflow for one issue at a time, pause for human
approval, close approved work, and continue until the PRD is complete.

**Commit policy: Do not commit work before the user approves the completed
issue. After approval, the agent may create one issue-completion commit before
continuing to the next issue. Never include unrelated pre-existing changes.**

## Process

### 1. Select the PRD

Find `.scratch/*/PRD.md` files.

- If there is exactly one active PRD, use it.
- If there are multiple active PRDs, ask the user which one to run.
- If there are none, report that there is no PRD work available and stop.

Read the selected PRD and its `.scratch/<feature>/progress.txt` before starting
so prior attempts, known pitfalls, and the current feature state are preserved.

### 2. Run task-loop

Run:

```bash
task-loop .scratch/<feature>/PRD.md
```

`task-loop` owns the issue workflow and bounded retries. Do not duplicate its
phases in this skill or launch a separate implementation subagent around it.
Its phase agents must append any files they change to the feature's
`progress.txt` using `task-loop add-message`. After the command returns,
reconcile those entries against the working tree and append anything they
missed.

If `task-loop` exits unsuccessfully, append the outcome to `progress.txt`,
including the selected issue when known, changes made, failure or blocking
reason, and the next action required. Present the blocker to the user and stop.

### 3. Record all changes

After `task-loop` returns successfully, inspect the working tree and the
entries written by its phase agents, then append a timestamped completion
entry to `.scratch/<feature>/progress.txt`. `progress.txt` is append-only;
never rewrite or delete earlier entries.

Record every code, test, documentation, configuration, generated-artifact, and
issue-tracker change made during the loop:

```text
[YYYY-MM-DD HH:MM] Completed task-loop for issue <NN>-<slug>
  Status: awaiting-user-approval
  Changes:
    - <file or area>: <meaningful change>
  Validation: <tests, builds, or checks performed and their outcomes>
  Staged files: <files staged with git add, or "none">
  Next: user approval
```

If additional changes are made after this entry, append another timestamped
entry describing them before requesting approval again. No change may be left
unrecorded merely because it happened during feedback or cleanup.

### 4. Request human approval

Present the completed issue, meaningful changes, and any caveats. Ask the user
to approve the issue or request changes.

- **Changes requested:** append the feedback to `progress.txt`, make or route
  the requested changes through the appropriate task-loop retry, record the
  resulting changes, and request approval again.
- **Approved:** continue to issue completion.
- **Cancelled:** record that the work remains awaiting approval and stop.

### 5. Complete the approved issue

Once the user approves:

1. Update the issue status to completed according to the local issue format.
2. Move the issue from `.scratch/<feature>/issues/` to
   `.scratch/<feature>/issues/closed/`.
3. If a matching review document exists, mark it accepted and move it from
   `.scratch/<feature>/reviews/` to `.scratch/<feature>/reviews/closed/`.
4. Append an approval/completion entry to `progress.txt`, including every file
   moved or changed.
5. Stage all changes belonging to the approved issue, including its
   issue-tracker and progress updates. Do not stage unrelated changes.
6. Create one commit for the completed issue. Use a concise message identifying
   the issue and include the repository's required commit trailers.

### 6. Continue the issue completion loop

Check the same PRD for another unblocked `ready-for-agent` issue.

- If one exists, return to **Run task-loop** for the same PRD.
- If work remains but is blocked or not ready for an agent, record the state in
  `progress.txt`, explain what prevents continuation, and stop.
- If no incomplete issues remain, treat the PRD as complete.

Repeat the task-loop → progress → user approval → issue completion cycle until
the PRD is complete or the user stops the loop.

### 7. Offer to create a pull request

When every PRD issue is completed:

1. Append a final PRD-completion entry to `progress.txt`.
2. Stage the final progress update.
3. Summarize the completed PRD and ask whether the user wants a pull request
   created.
4. Create the pull request only after the user explicitly accepts. Push the
   issue-completion commits when needed to create the pull request.
