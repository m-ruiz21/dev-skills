---
name: develop-task 
description: Find and implement top-priority tasks from the local issue tracker. Reads progress.txt, picks up reviews needing rework and unblocked ready-for-agent issues, runs non-conflicting tasks in parallel via subagents. Use when user says "work", "worker", "pick up tasks", "afk", or "work loop".
---

# Worker

Discover actionable work → determine parallelism → implement via subagents → update progress.

**Commit policy: Agents must NEVER run `git commit`.** Agents may `git add` (stage) changes. Committing is always the human's responsibility. The human invokes the `review` skill separately.

## Process

### 1. Discover actionable items

Scan `.scratch/` for work, in strict priority order:

**Priority 1 — Reviews needing rework.** Scan `.scratch/*/reviews/` (not `closed/`) for review docs that have new entries in `# User Feedback` since the last progress entry. These take priority because the human has already reviewed and given direction.

**Priority 2 — Unblocked `ready-for-agent` issues.** Scan `.scratch/*/issues/` (not `closed/`) for issues with `status: ready-for-agent` and either no `blocked-by` list or all `blocked-by` entries resolved (moved to `closed/`). Sort oldest first.

### 2. Read progress context

For each candidate, read `.scratch/<feature>/progress.txt` to understand:
- What has already been attempted
- What the current state of the feature is
- Whether there are known pitfalls or context from previous work sessions

Also read the parent PRD (`.scratch/<feature>/PRD.md`) and the issue file for full context.

### 3. Determine parallelism

Group candidates by feature. Tasks that touch different features can run in parallel. Tasks within the same feature run sequentially to avoid conflicts.

Present the execution plan to the user:
```
Ready to work:
  [parallel] auth/03-token-refresh (rework — user feedback exists)
  [parallel] ui/01-dark-mode (ready-for-agent)
  [sequential after ui/01] ui/02-theme-picker (ready-for-agent, same feature)
```

Ask the user to confirm or adjust before launching.

### 4. Implement

For each task, launch a `general-purpose` subagent with:
- The full issue body and acceptance criteria
- The PRD for context
- Any prior review doc and user feedback (for rework items)
- Relevant entries from `progress.txt`
- Instructions to `git add` changed files but NEVER `git commit`

Run non-conflicting tasks as parallel background agents. Wait for all to complete.

### 5. Update progress

After each subagent completes, append a timestamped entry to `.scratch/<feature>/progress.txt`:

```
[YYYY-MM-DD HH:MM] Implemented issue <NN>-<slug>
  Status: complete / partial / blocked
  Changes: brief summary of what was done
  Staged files: list of files added with git add
  Next: what remains (if partial)
```

### 6. Present results

Show the user a summary of all completed work:
- Which tasks were implemented
- What files were staged
- Any issues encountered

Remind the user to run the `review` skill and then `git commit` when ready.
