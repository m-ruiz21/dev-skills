---
name: review-diff
description: Run a multi-dimensional review of staged changes using parallel subagents for security, test gaps, plan alignment, code quality, and architecture. Compiles findings into a review document with pass/fail summary and severity ratings. Use when user says "review", "check my changes", "review staged", or as part of the dev-review loop.
---

# Review

Run five parallel review dimensions against the staged diff, compile findings into a review document with a pass/fail summary.

**Commit policy: This skill does NOT commit.** It reviews staged changes and writes a review document. The human commits.

## Process

### 1. Gather context

Determine what to review and where the review doc should go:

- Run `git diff --staged` to get the staged diff. If nothing is staged, tell the user and stop.
- Identify the feature being worked on. Check if the user passed an issue path or feature slug. If not, infer from the staged files or ask.
- Locate the PRD at `.scratch/<feature>/PRD.md` and the relevant issue file(s) in `.scratch/<feature>/issues/`.

### 2. Launch review subagents

Launch **five** background `explore` subagents in parallel, each receiving the full staged diff and relevant context:

#### a) Security review
Prompt the agent to analyze the staged diff for:
- Injection vulnerabilities (SQL, XSS, command injection)
- Authentication/authorization gaps
- Secrets or credentials in code
- Insecure dependencies or configurations
- Data exposure risks

Severity: 🔴 critical, 🟠 high, 🟡 medium, 🔵 low

#### b) Test gap analysis
Prompt the agent to analyze the staged diff for:
- New code paths without test coverage
- Modified behavior without updated tests
- Edge cases that should be tested but aren't
- Regression risk from untested changes

Severity: 🔴 critical, 🟠 high, 🟡 medium, 🔵 low

#### c) Plan alignment
Prompt the agent with the staged diff AND the PRD + issue content to check:
- Does the implementation match the acceptance criteria?
- Are there deviations from the PRD's implementation decisions?
- Is anything in the issue's "Out of scope" being implemented?
- Are there acceptance criteria not addressed by the changes?

Severity: 🔴 blocker (scope violation), 🟠 drift (deviation from plan), 🟡 gap (missing criteria), 🔵 note

#### d) Code quality
Prompt the agent to analyze the staged diff for:
- Logic errors and off-by-one bugs
- Error handling gaps (missing try/catch, unchecked returns)
- Performance concerns (N+1 queries, unnecessary allocations)
- Naming clarity and code readability
- DRY violations and unnecessary complexity

Severity: 🔴 bug, 🟠 high, 🟡 medium, 🔵 nitpick

#### e) Architecture
Prompt the agent with the staged diff and the `improve-codebase-architecture` skill's vocabulary (modules, interfaces, depth, seams, locality, leverage) to check:
- Are new modules deep or shallow?
- Do changes respect existing seams or leak across them?
- Is there unnecessary coupling introduced?
- Would the deletion test pass for any new abstractions?
- Do changes align with existing ADRs?

Severity: 🔴 structural (breaks architectural invariant), 🟠 concern, 🟡 suggestion, 🔵 note

### 3. Compile review document

Once all five subagents return, compile their findings into a single review document.

#### Location

- If a feature slug is known: `.scratch/<feature>/reviews/<NN>-<slug>.md`
- The `<NN>-<slug>` matches the issue being reviewed

#### Format

```markdown
---
title: Review — {ISSUE TITLE}
issue: {PATH TO ISSUE FILE}
prd: {PATH TO PRD FILE}
---

# Review Summary

| Dimension         | Verdict | Critical | High | Medium | Low |
|-------------------|---------|----------|------|--------|-----|
| Security          | ✅/❌   | 0        | 0    | 0      | 0   |
| Test gaps         | ✅/❌   | 0        | 0    | 0      | 0   |
| Plan alignment    | ✅/❌   | 0        | 0    | 0      | 0   |
| Code quality      | ✅/❌   | 0        | 0    | 0      | 0   |
| Architecture      | ✅/❌   | 0        | 0    | 0      | 0   |

**Overall: ✅ PASS / ❌ FAIL**

A dimension fails (❌) if it has any critical/blocker findings. The overall review fails if any dimension fails.

### Files changed

- `path/to/file.ext` — summary of what changed

---

## Security

{Findings from security subagent, grouped by severity}

## Test Gaps

{Findings from test gap subagent, grouped by severity}

## Plan Alignment

{Findings from plan alignment subagent, grouped by severity}

## Code Quality

{Findings from code quality subagent, grouped by severity}

## Architecture

{Findings from architecture subagent, grouped by severity}

---

# User Feedback

<!-- Appended when user provides feedback -->
```

### 4. Present to user

Show the summary table and highlight any ❌ dimensions. List critical/high findings inline. Tell the user where the full review doc lives. Remind them that changes are staged but NOT committed — they must `git commit` when ready.

### 5. Update progress

Append a timestamped entry to `.scratch/<feature>/progress.txt`:

```
[YYYY-MM-DD HH:MM] Review completed for issue <NN>-<slug>
  Overall: PASS/FAIL
  Security: ✅/❌ | Tests: ✅/❌ | Alignment: ✅/❌ | Quality: ✅/❌ | Architecture: ✅/❌
  Review doc: .scratch/<feature>/reviews/<NN>-<slug>.md
```
