---
name: afk-work-loop
description: Execute an AFK work loop — find ready-for-agent issues, implement them via subagent, self-review with iterative fix cycles, present results for human approval, and re-triage unblocked items. Use when user wants to work AFK issues, run the agent loop, or says "afk", "work loop", or "pick up ready issues".
---

# AFK Work Loop

One-shot loop: discover ready work → implement → self-review → present to human → close or rework.

## 1. Discover ready work

Scan for actionable items, oldest first:

1. **Open reviews needing rework** — files in `.scratch/review/open/` whose `# Review Comments` section contains unresolved `needs-human` comments with new user feedback since the last agent pass.
2. **Ready-for-agent issues** — `.scratch/*/issues/*.md` files with `Status: ready-for-agent` and no unresolved `blocked-by` entries (see [ISSUE-CONVENTIONS.md](ISSUE-CONVENTIONS.md)).

Present both buckets with one-line summaries. If multiple items are ready and the user hasn't specified, use `ask_user` with a multi-select list. If only one item exists, proceed directly.

Work selected items **sequentially** (never in parallel) to avoid working-tree conflicts.

## 2. Implement

For each selected item:

1. Read the issue file and any linked agent brief or prior review doc.
2. Launch a `general-purpose` subagent via the `task` tool. Provide it with:
   - The full agent brief / issue body
   - Any prior review doc (if reworking)
   - Instruction to make direct changes in the working tree
3. When the subagent completes, proceed to step 3.

## 3. Create or update review document

After implementation, create (or update if reworking) a review document in `.scratch/review/open/`. See [REVIEW-DOC.md](REVIEW-DOC.md) for the template and naming conventions.

The review doc must include:
- Frontmatter with `title` and `issue` (path to the issue file)
- Description / overview of all changes made
- File paths and a summary of what changed in each

## 4. Self-review loop (max 3 iterations)

1. Launch a `code-review` agent via the `task` tool. Pass it the issue brief and a summary of changes as context.
2. The review agent documents findings. For each finding, categorize:
   - **`auto-fix`** — clear bug, missing guard, incorrect logic with an obvious fix
   - **`needs-human`** — design decisions, ambiguous requirements, scope questions, tradeoffs
3. Append all findings to the review doc's `# Review Comments` section (see [REVIEW-DOC.md](REVIEW-DOC.md)).
4. Launch a subagent to fix all `auto-fix` items. Mark each fixed comment with `✅ Auto-fixed in iteration N`.
5. If auto-fixes were made and iteration count < 3, go to step 1.
6. After 3 iterations or when no `auto-fix` items remain, proceed to step 5.

## 5. Present results

Show the user:
- A summary of what was implemented and where
- Count of review iterations performed
- Any `needs-human` comments that require their judgment
- A link to the full review doc

If no `needs-human` items remain after the review loop, note that the implementation passed self-review cleanly.

**Wait for user feedback** via `ask_user` before proceeding.

## 6. Process feedback

**If accepted:**
1. Update the issue's `Status:` line to `closed`.
2. Move the issue file to `.scratch/<feature>/issues/closed/`.
3. Move the review doc to `.scratch/review/closed/`.
4. Proceed to step 7 (re-triage).

**If rejected:**
1. Keep the review doc in `.scratch/review/open/`.
2. Append the user's feedback as new comments in the review doc's `# Review Comments` section.
3. Add a `review-doc:` reference in the issue's frontmatter pointing to the review doc path.
4. Leave the issue as `Status: ready-for-agent` for the next invocation.

## 7. Re-triage

After closing an issue, scan all open issues for those whose `blocked-by` list referenced the just-closed issue. For each newly unblocked issue:
1. Remove the resolved entry from `blocked-by`.
2. If `blocked-by` is now empty, change `Status:` from `blocked` to `needs-triage`.

Report any status changes to the user.
