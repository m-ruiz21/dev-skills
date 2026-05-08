---
name: afk-work-loop
description: Execute an AFK work loop — find ready-for-agent issues, implement them via subagent, self-review with iterative fix cycles, present results for human approval, and re-triage unblocked items. Use when user wants to work AFK issues, run the agent loop, or says "afk", "work loop", or "pick up ready issues".
---

# AFK Work Loop

One-shot loop: discover ready work → implement → self-review → present to human → close or rework.

## Loop

1. **Discover** — Scan for actionable items, oldest first: open reviews needing rework (`.scratch/review/open/` with new user feedback), then `ready-for-agent` issues with no unresolved `blocked-by` entries. Present candidates and let the user pick if multiple exist.
2. **Implement** — Read the issue file and any prior review doc. Launch a `general-purpose` subagent to make changes in the working tree. Work items sequentially to avoid conflicts.
3. **Review** — Create or update a review doc in `.scratch/review/open/`. Run up to 3 self-review iterations with a `code-review` agent: auto-fix clear bugs, surface `needs-human` items. See `docs/agents/review-conventions.md` for the review doc format.
4. **Present** — Show the user a summary of changes, review iteration count, and any `needs-human` items. Wait for feedback.
5. **Close or rework** — If accepted: set status to `closed`, move the issue to `closed/`, move the review doc to `.scratch/review/closed/`. If rejected: append feedback to the review doc and leave the issue as `ready-for-agent`. See `docs/agents/issue-conventions.md` for issue lifecycle.
6. **Re-triage** — After closing, scan open issues for those unblocked by the closure. Update their status from `blocked` to `needs-triage`.
