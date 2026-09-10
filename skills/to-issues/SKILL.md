---
name: to-issues
description: Break a plan, spec, or PRD into independently-grabbable local task-loop issues using tracer-bullet vertical slices. Use when user wants to convert a plan into issues, create implementation tickets, or break down work into issues.
---

# To Issues

Break a plan into independently-grabbable issues using vertical slices (tracer bullets).

This workflow creates local issues beside a PRD under
`.scratch/<feature>/issues/` through the bundled `task-loop` CLI.

## Process

### 1. Gather context

Work from whatever is already in the conversation context. If the user passes an issue reference (issue number, URL, or path) as an argument, fetch it from the issue tracker and read its full body and comments.

### 2. Explore the codebase (optional)

If you have not already explored the codebase, do so to understand the current state of the code. Issue titles and descriptions should use the project's domain glossary vocabulary, and respect ADRs in the area you're touching.

### 3. Draft vertical slices

Break the plan into **tracer bullet** issues. Each issue is a thin vertical slice that cuts through ALL integration layers end-to-end, NOT a horizontal slice of one layer.

Slices may be 'HITL' or 'AFK'. HITL slices require human interaction, such as an architectural decision or a design review. AFK slices can be implemented and merged without human interaction. Prefer AFK over HITL where possible.

<vertical-slice-rules>
- Each slice delivers a narrow but COMPLETE path through every layer (schema, API, UI, tests)
- A completed slice is demoable or verifiable on its own
- Prefer many thin slices over few thick ones
</vertical-slice-rules>

### 4. Quiz the user

Present the proposed breakdown as a numbered list. For each slice, show:

- **Title**: short descriptive name
- **Type**: HITL / AFK
- **Blocked by**: which other slices (if any) must complete first
- **User stories covered**: which user stories this addresses (if the source material has them)

Ask the user:

- Does the granularity feel right? (too coarse / too fine)
- Are the dependency relationships correct?
- Should any slices be merged or split further?
- Are the correct slices marked as HITL and AFK?

Iterate until the user approves the breakdown.

### 5. Create the approved local issues

Do not create request files or invoke `task-loop` until the user explicitly
approves the final slice list and dependencies.

#### Establish the canonical local PRD

After approval, and before creating any request file, resolve the repository
root and establish one canonical, repository-confined
`.scratch/<feature>/PRD.md`:

1. If the supplied source is already an existing regular file whose canonical
   repository-relative path matches exactly `.scratch/<feature>/PRD.md` (one
   feature directory, then the case-sensitive filename `PRD.md`), reuse it without rewriting it.
   Reject paths that escape the repository after
   resolving filesystem redirects.
2. Otherwise derive a feature slug from the explicit feature name, or from the
   source title/first heading when no name was supplied. Lowercase ASCII,
   replace each run of characters other than `a-z` and `0-9` with one hyphen,
   trim hyphens, and limit the result to 80 characters without leaving a
   trailing hyphen. The result must be non-empty and must not be a Windows
   reserved device name (`con`, `prn`, `aux`, `nul`, `com1`-`com9`, or
   `lpt1`-`lpt9`); prefix a reserved result with `feature-`.
3. Inspect `.scratch/<slug>` before writing. Reuse an existing PRD only when its
   normalized Markdown is exactly the document that would be materialized for
   this request. An explicitly supplied valid local PRD from step 1 is always
   the source and is reused. Never overwrite, truncate, or silently repurpose
   any other PRD or existing directory. If a user explicitly selected the
   colliding slug, report the collision and ask them to choose the existing
   source or an alternative slug. For a derived slug, choose the first absent directory
   from `<slug>-2`, `<slug>-3`, and so on; truncate the base as needed
   to keep the complete suffixed slug within 80 characters.
4. Materialize every accepted source that was not reused in step 1 as UTF-8
   Markdown at the chosen `.scratch/<feature>/PRD.md`. This includes every
   noncanonical local source, such as a local spec, issue, or plan file whose
   path does not match the exact canonical shape, as well as remote and
   conversation sources. Use filesystem directory and write tools with
   create-new/refuse-overwrite behavior, never shell interpolation,
   redirection, or a command-generated document. Preserve the plan's headings,
   decisions, user stories, and constraints. For a remote issue, include a
   `## Source` section containing its canonical issue URL or reference before
   the copied source body; use that same reference as each generated issue's
   `parent`. For a conversation source, write the cohesive plan that the
   approved breakdown was based on. Normalize line endings to LF and end the
   document with one newline so collision comparison is deterministic.
5. Ensure `issues/` and `reviews/` exist and create an empty `progress.txt` only
   when absent. Do not truncate progress or alter existing issue/review files.
   Do not publish, close, label, or otherwise modify a remote source issue.

Before continuing, verify that the selected PRD now exists as a regular file
and still satisfies the exact canonical `.scratch/<feature>/PRD.md` shape.
Use its repository-relative slash-separated path as `prd` below.

Resolve `<plugin-root>` from this skill's installed location: it is the parent
of the `skills` directory containing this `SKILL.md`. Select the bundled
`task-loop` binary using the same OS/architecture mapping as `/develop-task`
(`windows-amd64`, `windows-arm64`, `darwin-amd64`, `darwin-arm64`,
`linux-amd64`, or `linux-arm64`). Resolve it to an absolute path and verify it
exists. Do not invoke `task-loop` from `PATH`, directly write issue Markdown, or
publish these slices to a remote tracker.

Create issues in dependency order (blockers first). Record the repository-
relative path printed as `Created issue: <path>`, then include that exact path
in `blockedBy` for each dependent issue. The CLI writes
`status: ready-for-agent` and the canonical body below; do not add labels,
frontmatter, or body sections yourself.

For every issue, choose one unique, repository-confined request path under its
feature directory. Use the agent runtime's filesystem write tool with
create-new/refuse-overwrite behavior to write this UTF-8 JSON object as literal
tool data:

```json
{
  "prd": ".scratch/<feature>/PRD.md",
  "title": "<approved title>",
  "description": "<complete multiline What to build text>",
  "acceptanceCriteria": [
    "<criterion 1>",
    "<criterion 2>"
  ],
  "parent": "<optional parent reference>",
  "blockedBy": [
    "<created blocker path>"
  ]
}
```

`prd`, `title`, `description`, and a non-empty `acceptanceCriteria` array are
required. `parent` and `blockedBy` are optional. Every criterion and the title
must be a non-empty single line. JSON escaping is performed by the filesystem
write tool/JSON serializer; never construct the document with a shell command,
redirection, here-document/here-string, `printf`, or environment variable.

Invoke the binary only through a process API that has separate executable,
argument-array, and working-directory fields:

```text
executable: <absolute selected task-loop binary path>
arguments:  ["create-issue", "-request-file", <absolute request-file path>]
workingDirectory: <absolute repository root>
```

The fields above describe structured process data, not a command template.
Never interpolate raw values into PowerShell, Bash, Zsh, `cmd.exe`, or any
other shell source. This applies to the plugin path, repository path,
feature/PRD path, request path, title, description, criteria, parent, and
blocker paths.

If the runtime only offers a shell-source interface, build the same argv array
in memory and encode **every** element, including the executable, with one of
these exact reusable literal encoders before joining it:

- PowerShell: `literal(s) = "'" + s.Replace("'", "''") + "'"`; prefix the joined
  argv with `& `.
- Bash/Zsh/POSIX shell: `literal(s) = "'" + s.Replace("'", "'\"'\"'") + "'"`;
  join the encoded argv with one space.

Do not special-case “safe-looking” arguments and do not use double quotes.
These are the same all-argv encoders exercised by task-loop's PowerShell and
Bash command-template integration tests. If the execution tool cannot provide
structured argv and the relevant encoder cannot be applied exactly, stop
rather than run an unescaped command.

Capture stdout and require a zero exit status plus exactly one
`Created issue: <path>` result line. Treat `<path>` as opaque data, verify it is
repository-relative, and record it before continuing. Remove only the request
file created for that issue, using the runtime's filesystem delete operation
rather than a shell command, whether invocation succeeds or fails. For a
dependent slice, create a fresh request document and add the exact successful
blocker path to `blockedBy`. Never guess a dependency filename or derive one
from a title.

<issue-template>
## Parent

A reference to the parent issue on the issue tracker (if the source was an existing issue, otherwise omit this section).

## What to build

A concise description of this vertical slice. Describe the end-to-end behavior, not layer-by-layer implementation.

Avoid specific file paths or code snippets — they go stale fast. Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it here and note briefly that it came from a prototype. Trim to the decision-rich parts — not a working demo, just the important bits.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2
- [ ] Criterion 3

## Blocked by

- A reference to the blocking ticket (if any)

Or "None - can start immediately" if no blockers.

</issue-template>

Do NOT close or modify any parent issue.
