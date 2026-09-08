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

Canonical YAML frontmatter is the format producers should write. Existing
issues are also read when they use `Status: <value>` or
`**Status:** <value>` near the top of the body; these inline forms are
migration compatibility only. If YAML and inline status both exist, YAML takes
precedence. Status parsing preserves unknown values; work-loop eligibility is a
separate policy.

### `blocked-by`

A YAML list of paths to issue files that must be closed before this issue can be worked.

An issue with `status: ready-for-agent` and a non-empty `blocked-by` list is
ineligible for the work loop only while at least one referenced blocker is not
closed. It becomes selectable automatically once every referenced blocker is
closed; resolving dependencies does not change the issue's `status` or
`blocked-by` metadata.

### `review-doc`

Path to the review document in `.scratch/<feature>/reviews/`. Added automatically when the user rejects work so the next invocation has context on what was tried and what the feedback was.

## Closed issue directory

When an issue is accepted and closed:

1. The canonical YAML `status` field is set to `closed` (legacy inline status is read only while migrating existing issues).
2. The issue file is moved to `.scratch/<feature>/issues/closed/<NN>-<slug>.md`.

The `closed/` subdirectory is created if it doesn't exist.

## Dependency resolution

When selecting work, the work loop resolves each `blocked-by` entry by checking
both the referenced path and the file with the same name under the issue
directory's `closed/` subdirectory. This accounts for references that retain an
issue's original path after the issue is moved to `closed/`.

The work loop excludes a `ready-for-agent` issue while any dependency does not
resolve to an issue with `status: closed`. Once all dependencies resolve, the
issue is selectable without rewriting its frontmatter.
