# Issue Conventions

These conventions extend the local issue tracker format (`.scratch/`) with fields that workflow skills depend on.

## Frontmatter fields

Issue files in `.scratch/<feature>/issues/<NN>-<slug>.md` support these frontmatter fields:

```yaml
---
title: Token refresh silently fails
status: ready-for-agent
blocked-by:
  - .scratch/auth/issues/01-login-flow.md
  - .scratch/auth/issues/02-session-store.md
review-doc: .scratch/auth/reviews/03-token-refresh.md
---
```

### `status`

The canonical triage status. Values match `triage-labels.md`:
- `needs-triage`
- `needs-info`
- `ready-for-agent`
- `ready-for-human`
- `blocked`
- `wontfix`
- `closed`

Also supported inline as `Status: <value>` near the top of the issue body for backward compatibility with the existing convention.

### `blocked-by`

A YAML list of paths to issue files that must be closed before this issue can be worked. When all referenced issues are closed, the AFK skill changes status from `blocked` to `needs-triage`.

An issue with a non-empty `blocked-by` list is not eligible for the work loop, even if its status is `ready-for-agent`.

### `review-doc`

Path to the review document in `.scratch/<feature>/reviews/`. Added automatically when the user rejects work so the next invocation has context on what was tried and what the feedback was.

## Closed issue directory

When an issue is accepted and closed:

1. The `status` field (or `Status:` line) is set to `closed`.
2. The issue file is moved to `.scratch/<feature>/issues/closed/<NN>-<slug>.md`.

The `closed/` subdirectory is created if it doesn't exist.

## Dependency resolution

When a skill closes an issue, it scans all open issues (files **not** in a `closed/` directory) for `blocked-by` entries referencing the closed issue's path. For each match:

1. Remove the closed issue's path from the `blocked-by` list.
2. If `blocked-by` is now empty, change `status` to `needs-triage`.
3. Report the status change to the user.

Note: after moving a closed issue to `closed/`, the `blocked-by` references in other issues still point to the original (pre-move) path. The skill should check against both the original path and the new `closed/` path when resolving dependencies.
