# Issue tracker: Local Markdown

Issues, PRDs, and reviews for this repo live as markdown files in `.scratch/`.

## Directory structure

```
.scratch/<feature-slug>/
├── PRD.md                          # The PRD for this feature
├── progress.txt                    # Agents append progress notes after each work session
├── issues/
│   ├── 01-login-flow.md            # Open issues
│   ├── 02-session-store.md
│   └── closed/
│       └── 03-token-refresh.md     # Closed issues
└── reviews/
    ├── 01-login-flow.md            # Open reviews (awaiting human approval)
    └── closed/
        └── 02-session-store.md     # Approved/completed reviews
```

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- Feature slugs contain lowercase ASCII letters and numbers separated by single hyphens, are at most 80 characters, and are neither empty nor Windows reserved device names.
- The PRD is `.scratch/<feature-slug>/PRD.md`
- Implementation issues are `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01`
- Review documents are `.scratch/<feature-slug>/reviews/<NN>-<slug>.md`, matching the issue they review
- `progress.txt` is an append-only log — agents add a timestamped entry after each work session
- Issue triage and dependencies use the canonical YAML frontmatter below; body
  `Status:` lines are not compatible with task-loop
- Comments and conversation history append to the bottom of the file under a `## Comments` heading

## Issue format

An issue with no dependencies starts with this exact frontmatter:

```yaml
---
title: <issue title>
status: ready-for-agent
blocked-by: []
---
```

For a dependent issue, replace `blocked-by: []` with the canonical YAML list:

```yaml
blocked-by:
  - .scratch/<feature-slug>/issues/<NN>-<slug>.md
```

After the frontmatter, task-loop generates an optional `## Parent` section,
then `## What to build`, `## Acceptance criteria`, and `## Blocked by`.

## Commit policy

**Agents must NEVER run `git commit`.** Agents may stage changes with `git add`, but committing is always the human's responsibility. After staging, the agent should present a summary of changes and run the review skill so the human can inspect before committing.

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/` (creating the directory if
needed). Use create-new/refuse-overwrite writes: reuse a PRD only when it is the same source,
and never replace an unrelated PRD silently.

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or the issue number directly.
