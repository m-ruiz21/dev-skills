# Review Document Conventions

## Naming

Review documents follow the pattern: `<feature>-<NN>-<slug>.md`

The name is derived from the source issue. For example:
- Issue: `.scratch/auth/issues/03-token-refresh.md` → Review: `.scratch/review/open/auth-03-token-refresh.md`
- Issue: `.scratch/ui/issues/01-dark-mode.md` → Review: `.scratch/review/open/ui-01-dark-mode.md`

## Directory structure

```
.scratch/review/
├── open/          # Active reviews awaiting human approval
│   └── auth-03-token-refresh.md
└── closed/        # Approved and completed reviews
    └── ui-01-dark-mode.md
```

## Template

```markdown
---
title: {TITLE}
issue: {PATH TO ISSUE FILE}
---

{DESCRIPTION / OVERVIEW OF CHANGES}

### Files changed

- `path/to/file.ext` — summary of what changed
- `path/to/other.ext` — summary of what changed

---

# Review Comments

## Iteration 1

Author: code-review-agent
Category: auto-fix | needs-human

> ref: path/to/file.ext:L10-L15
> Description of the issue found

Resolution: ✅ Auto-fixed in iteration 1
<!-- or -->
Resolution: ⏳ Needs human review

## Iteration 2

Author: code-review-agent
Category: auto-fix

> ref: path/to/file.ext:L22
> New issue introduced by prior fix

Resolution: ✅ Auto-fixed in iteration 2

---

# User Feedback

<!-- Appended when user provides feedback on rejection -->

Author: user
Date: YYYY-MM-DD

> Feedback text here
```

## Comment categories

- **`auto-fix`** — Clear bug, missing guard, incorrect logic with an obvious fix. The agent resolves these without human input.
- **`needs-human`** — Design decisions, ambiguous requirements, scope questions, tradeoffs. These are surfaced to the user.

## Resolution markers

- `✅ Auto-fixed in iteration N` — resolved by the agent in the Nth review pass
- `⏳ Needs human review` — waiting for user judgment
- `✅ Accepted` — user approved after review
- `❌ Rejected — [reason]` — user rejected with explanation

## Updating an existing review doc

When reworking a previously reviewed item (i.e., the review doc already exists in `.scratch/review/open/`):

1. Update the description / overview section to reflect new changes.
2. Update the "Files changed" list.
3. Append new review iterations below existing ones (don't overwrite history).
4. Preserve all prior user feedback in the `# User Feedback` section.
