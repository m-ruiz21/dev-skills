---
name: setup-repo 
description: Sets up an `## Agent skills` block in AGENTS.md/CLAUDE.md and `docs/agents/` so the engineering skills know this repo's configured project tracker, local task-loop issue conventions, triage label vocabulary, and domain doc layout. Run before first use of `to-issues`, `to-prd`, `triage`, `diagnose`, `tdd`, `improve-codebase-architecture`, or `zoom-out` — or if those skills appear to be missing this repository context.
disable-model-invocation: true
---

# Setup Repo 

Scaffold the per-repo configuration that the engineering skills assume:

- **Project issue tracker** — where `to-prd` publishes PRDs and `triage`
  manages issues (GitHub by default; local markdown is also supported)
- **Local task-loop issues** — `to-issues` always creates implementation
  slices in `.scratch/<feature>/issues/` through the bundled `task-loop`,
  reusing or materializing `.scratch/<feature>/PRD.md` first, even when the
  configured project tracker is GitHub, GitLab, or another service
- **Triage labels** — the strings used for the five canonical triage roles
- **Domain docs** — where `CONTEXT.md` and ADRs live, and the consumer rules for reading them

This is a prompt-driven skill, not a deterministic script. Explore, present what you found, confirm with the user, then write.

## Process

### 1. Preflight task-loop

Before inspecting or changing the target repository, verify the plugin's
packaged `task-loop` CLI. Resolve `<plugin-root>` from this skill's installed
location: it is the parent of the `skills` directory containing this
`SKILL.md`. Use the exact host detection, architecture normalization, and
binary mapping documented in `/develop-task` section 2; do not invent a second
mapping. It must select one of `windows-amd64`, `windows-arm64`,
`darwin-amd64`, `darwin-arm64`, `linux-amd64`, or `linux-arm64`, with
`task-loop.exe` on Windows and `task-loop` elsewhere. Resolve the selected
binary to an absolute path and require it to be a regular file.

Invoke the selected binary through a process API with separate executable,
argument-array, and working-directory fields:

```text
executable: <absolute selected task-loop binary path>
arguments:  ["--help"]
workingDirectory: <absolute repository root>
```

This is structured process data, not shell source. If only a shell-source
interface is available, use the exact all-argv literal encoder from
`/develop-task` section 2 for the executable and every argument. Require a zero
exit status and stdout containing `usage: task-loop`. A launch error (including
permission denied or an invalid executable), nonzero exit, or unexpected help
output fails preflight.

On an unsupported platform, stop and list the six supported targets. If the
binary is absent or preflight fails, stop with this remediation, including the
resolved target and absolute expected path:

> Bundled task-loop is unavailable or not executable for `<target>` at
> `<path>`. Reinstall or update the dev-loop plugin/package so its
> `bin/task-loop/` payload contains the packaged binaries, then rerun
> `/setup-repo`.

Do not download, build, install, or change permissions during this setup
workflow.

### 2. Explore

Look at the current repo to understand its starting state. Read whatever exists; don't assume:

- `git remote -v` and `.git/config` — is this a GitHub repo? Which one?
- `AGENTS.md` and `CLAUDE.md` at the repo root — does either exist? Is there already an `## Agent skills` section in either?
- `CONTEXT.md` and `CONTEXT-MAP.md` at the repo root
- `docs/adr/` and any `src/*/docs/adr/` directories
- `docs/agents/` — does this skill's prior output already exist?
- `.scratch/` — sign that a local task-loop workspace or local-markdown project
  tracker convention is already in use

### 3. Present findings and ask

Summarise what's present and what's missing. Then walk the user through the
four decisions **one at a time** — present a section, get the user's answer,
then move to the next. Don't dump all four at once.

Assume the user does not know what these terms mean. Each section starts with a short explainer (what it is, why these skills need it, what changes if they pick differently). Then show the choices and the default.

**Section A — Issue tracker.**

> Explainer: The configured "project issue tracker" is where `to-prd`
> publishes PRDs and `triage` reads and manages issues. `to-issues` consults
> this tracker only when the user explicitly supplies an existing issue as its
> source. It never publishes the generated implementation slices to the
> configured tracker: those always go to
> `.scratch/<feature>/issues/` through the bundled `task-loop`, after a
> repository-confined local PRD is reused or materialized. Pick the place you
> track project-level work and incoming issues for this repo.

Default posture: `to-prd` and `triage` were designed for GitHub. If a `git
remote` points at GitHub, propose that. If a `git remote` points at GitLab
(`gitlab.com` or a self-hosted host), propose GitLab. Otherwise (or if the user
prefers), offer:

- **GitHub** — issues live in the repo's GitHub Issues (uses the `gh` CLI)
- **GitLab** — issues live in the repo's GitLab Issues (uses the [`glab`](https://gitlab.com/gitlab-org/cli) CLI)
- **Local markdown** — issues live as files under `.scratch/<feature>/` in this repo (good for solo projects or repos without a remote)
- **Other** (Jira, Linear, etc.) — ask the user to describe the workflow in one paragraph; the skill will record it as freeform prose

**Section B — Triage label vocabulary.**

> Explainer: When the `triage` skill processes an incoming issue, it moves it through a state machine — needs evaluation, waiting on reporter, ready for an AFK agent to pick up, ready for a human, or won't fix. To do that, it needs to apply labels (or the equivalent in your issue tracker) that match strings *you've actually configured*. If your repo already uses different label names (e.g. `bug:triage` instead of `needs-triage`), map them here so the skill applies the right ones instead of creating duplicates.

The five canonical roles:

- `needs-triage` — maintainer needs to evaluate
- `needs-info` — waiting on reporter
- `ready-for-agent` — fully specified, AFK-ready (an agent can pick it up with no human context)
- `ready-for-human` — needs human implementation
- `wontfix` — will not be actioned

Default: each role's string equals its name. Ask the user if they want to override any. If their issue tracker has no existing labels, the defaults are fine.

**Section C — Domain docs.**

> Explainer: Some skills (`improve-codebase-architecture`, `diagnose`, `tdd`) read a `CONTEXT.md` file to learn the project's domain language, and `docs/adr/` for past architectural decisions. They need to know whether the repo has one global context or multiple (e.g. a monorepo with separate frontend/backend contexts) so they look in the right place.

Confirm the layout:

- **Single-context** — one `CONTEXT.md` + `docs/adr/` at the repo root. Most repos are this.
- **Multi-context** — `CONTEXT-MAP.md` at the root pointing to per-context `CONTEXT.md` files (typically a monorepo).

**Section D — Review & issue conventions.**

> Explainer: `to-issues` creates local implementation issues through
> `task-loop`, and `develop-task` consumes them and maintains their review
> documents. This local workflow is required regardless of the project tracker
> chosen in Section A. Issues live in `.scratch/<feature>/issues/`; review docs
> live in `.scratch/<feature>/reviews/` (active) and
> `.scratch/<feature>/reviews/closed/` (done). Issue files use YAML frontmatter
> for status, dependencies, and review doc links.

Show the user the defaults from the seed templates
([review-conventions.md](./review-conventions.md) and
[issue-conventions.md](./issue-conventions.md)). Make clear that
`.scratch/<feature>/issues/` and the task-loop issue schema are required and
cannot be redirected to the configured project tracker. Let the user confirm
or customize only conventions that remain compatible with `task-loop`, such as
review naming and additional frontmatter fields.

### 4. Confirm and edit

Show the user a draft of:

- The `## Agent skills` block to add to whichever of `CLAUDE.md` / `AGENTS.md` is being edited (see step 5 for selection rules)
- The contents of `docs/agents/issue-tracker.md`, `docs/agents/triage-labels.md`, `docs/agents/domain.md`, `docs/agents/review-conventions.md`, `docs/agents/issue-conventions.md`

Let them edit before writing.

### 5. Write

**Pick the file to edit:**

- If `CLAUDE.md` exists, edit it.
- Else if `AGENTS.md` exists, edit it.
- If neither exists, ask the user which one to create — don't pick for them.

Never create `AGENTS.md` when `CLAUDE.md` already exists (or vice versa) — always edit the one that's already there.

If an `## Agent skills` block already exists in the chosen file, update its contents in-place rather than appending a duplicate. Don't overwrite user edits to the surrounding sections.

The block:

```markdown
## Agent skills

### Issue tracker

[one-line summary of the configured project tracker]. `to-prd` and `triage`
use this tracker; `to-issues` uses it only to resolve an explicitly supplied
source issue. See `docs/agents/issue-tracker.md`.

### Triage labels

[one-line summary of the label vocabulary]. See `docs/agents/triage-labels.md`.

### Domain docs

[one-line summary of layout — "single-context" or "multi-context"]. See `docs/agents/domain.md`.

### Review conventions

Review documents track implementation progress and self-review iterations in `.scratch/<feature>/reviews/`. See `docs/agents/review-conventions.md`.

### Issue conventions

`to-issues` creates implementation issues in `.scratch/<feature>/issues/`
through `task-loop`; those files use YAML frontmatter for status, dependencies,
and review doc links. See `docs/agents/issue-conventions.md`.
```

Then write the docs files using the seed templates in this skill folder as a starting point:

- [issue-tracker-github.md](./issue-tracker-github.md) — GitHub issue tracker
- [issue-tracker-gitlab.md](./issue-tracker-gitlab.md) — GitLab issue tracker
- [issue-tracker-local.md](./issue-tracker-local.md) — local-markdown issue tracker
- [triage-labels.md](./triage-labels.md) — label mapping
- [domain.md](./domain.md) — domain doc consumer rules + layout
- [review-conventions.md](./review-conventions.md) — review document structure and workflow
- [issue-conventions.md](./issue-conventions.md) — issue file frontmatter and lifecycle

For "other" issue trackers, write `docs/agents/issue-tracker.md` from scratch using the user's description.

**`.gitignore`**: Ensure `.scratch` is listed in the repo's `.gitignore`. If `.gitignore` doesn't exist, create it. If it exists but doesn't contain `.scratch`, append it. This is done silently — no user confirmation needed.

### 6. Done

Tell the user the setup is complete and identify the consumers precisely:
`to-prd` and `triage` use the configured project tracker and triage labels;
`to-issues` uses the tracker only to resolve an explicitly supplied source
issue, then reuses or materializes `.scratch/<feature>/PRD.md` and always creates local `.scratch/<feature>/issues/` through
`task-loop`; and `develop-task` consumes that local task-loop workspace.
Mention they can edit `docs/agents/*.md` directly later — re-running this skill
is only necessary if they want to switch project trackers or restart from
scratch.
