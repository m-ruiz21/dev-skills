---
name: review-diff
description: Run a multi-dimensional review of staged changes or an explicit caller-supplied issue patch using parallel subagents for security, test adequacy, plan alignment, code quality, and architecture. Emits structured grades and findings plus a human-readable review. Use when user says "review", "check my changes", "review staged", or as part of the dev-review loop.
---

# Review

Run five parallel review dimensions against the selected review delta and
compile a versioned structured result. The default delta is the staged diff.
When task-loop supplies an explicit issue-owned patch, review only that patch;
it contains independently labeled run-start-to-current index and effective
working-tree deltas. Identical layers may be emitted once. This preserves staged
deletions and staged content hidden by later worktree restoration while also
showing unstaged and untracked work, without including state that predated the
run. When invoked by Ralph, the JSON artifact is the execution contract and
Ralph renders Markdown and calculates pass/fail.

**Commit policy: This skill does NOT commit.** Its caller owns any later
approval and commit step.

When invoked with a `.scratch/<feature>/progress.txt` path, record any files
the review process creates or modifies by running the exact bundled
`add-message` command supplied by task-loop. Do not replace it with a bare
`task-loop` command or alter the quoting around its absolute executable path.
Do not add an entry for a read-only review that changes no files, and never
rewrite prior progress.

## Process

### 1. Gather context

- If the caller supplies an explicit issue-owned layered delta, read each
  labeled layer and do not run `git diff --staged` or broaden the review to
  other working-tree changes. Treat a combined identical-layer section as both
  index and effective-worktree evidence. Otherwise run `git diff --staged`. If
  every supplied layer is empty, tell the user and stop.
- Identify the feature and issue from the caller, staged files, or local issue
  tracker.
- Read the relevant PRD and issue acceptance criteria.
- If Ralph supplied a run ID and artifact path, preserve both exactly.

### 2. Launch review subagents

Launch five parallel review agents, each receiving the selected delta and
relevant context:

1. **Security** — injection, authorization, secrets, insecure dependencies,
   configuration, and data exposure.
2. **Test adequacy** — uncovered behavior, missing regression paths, edge cases,
   and misleading assertions.
3. **Plan alignment** — acceptance criteria, implementation decisions, scope,
   and omissions.
4. **Code quality** — logic errors, error handling, performance, clarity,
   duplication, and complexity.
5. **Architecture** — module depth, seams, locality, coupling, deletion tests,
   and ADR alignment.

Each agent must return a proposed integer grade from 0 through 100, specific
supporting evidence, and findings. Normalize all finding severities to `info`,
`low`, `medium`, `high`, `critical`, or `blocker`.

### 3. Compile the structured review

Every dimension must appear exactly once and have non-empty evidence. Findings
must retain stable identifiers across review passes and use explicit statuses:
`open`, `addressed`, `waived`, or `invalid`.
When Ralph supplies prior finding identities and dispositions, include them in
the new artifact. Do not silently drop an unresolved finding, and do not reopen
an `addressed`, `waived`, or `invalid` finding without a new stable identifier.

If the caller supplies an artifact path, write exactly to it. Otherwise write
`.scratch/<feature>/reviews/<NN>-<slug>.json`.

```json
{
  "schemaVersion": "1.0",
  "runId": "caller-supplied-run-id",
  "dimensions": [
    {
      "dimension": "security",
      "grade": 90,
      "evidence": ["No changed code constructs commands from untrusted input."]
    },
    {
      "dimension": "testAdequacy",
      "grade": 85,
      "evidence": ["New success and failure paths have focused tests."]
    },
    {
      "dimension": "planAlignment",
      "grade": 95,
      "evidence": ["All issue acceptance criteria are represented in the diff."]
    },
    {
      "dimension": "codeQuality",
      "grade": 88,
      "evidence": ["Errors remain typed and no broad fallback was introduced."]
    },
    {
      "dimension": "architecture",
      "grade": 90,
      "evidence": ["Workflow decisions remain in the orchestration module."]
    }
  ],
  "findings": [
    {
      "id": "TEST-001",
      "dimension": "testAdequacy",
      "severity": "medium",
      "status": "open",
      "summary": "The timeout branch lacks a regression test.",
      "location": {
        "path": "src/example.cs",
        "line": 42,
        "column": 5
      }
    }
  ]
}
```

Allowed dimensions are `security`, `testAdequacy`, `planAlignment`,
`codeQuality`, and `architecture`. Use `findings: []` when there are none.
Never omit a dimension, evidence, or findings.

Do not infer or declare Ralph's pass/fail result. Do not use promise-like text
as a completion signal. Ralph validates the JSON, applies its versioned quality
policy, and renders Markdown from the evaluated result.

### 4. Human-readable projection

When running outside Ralph, also present the grades, evidence, finding IDs,
severities, statuses, and locations, while noting that Ralph policy evaluation
was not performed. When Ralph supplied an artifact path, do not write separate
Markdown; Ralph writes
`.ralph/runs/<run-id>/artifacts/review/attempt-<n>/review.md` from validated
state. Human feedback may be displayed in Markdown, but workflow input always
comes from Ralph's structured approval response and is never recovered by
scraping this document.

### 5. Present to user

List critical/high findings inline and report the structured artifact path.
When using the default staged delta, remind the user that changes are staged
but not committed. Do not make a staging claim for an explicit issue patch.
