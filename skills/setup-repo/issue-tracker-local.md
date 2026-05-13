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
- The PRD is `.scratch/<feature-slug>/PRD.md`
- Implementation issues are `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01`
- Review documents are `.scratch/<feature-slug>/reviews/<NN>-<slug>.md`, matching the issue they review
- `progress.txt` is an append-only log — agents add a timestamped entry after each work session
- Triage state is recorded as a `Status:` line near the top of each issue file (see `triage-labels.md` for the role strings)
- Comments and conversation history append to the bottom of the file under a `## Comments` heading

## Commit policy

**Agents must NEVER run `git commit`.** Agents may stage changes with `git add`, but committing is always the human's responsibility. After staging, the agent should present a summary of changes and run the review skill so the human can inspect before committing.

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/` (creating the directory if needed).

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or the issue number directly.
