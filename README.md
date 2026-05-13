# dev-loop

A Claude Code plugin that provides a PRD-driven development workflow with automated triage, coding, and review loops.

## Skills Included

| Skill | Description |
|-------|-------------|
| `/develop-task` | Find and implement top-priority tasks from the local issue tracker |
| `/triage` | Triage issues through a state machine driven by triage roles |
| `/review-diff` | Multi-dimensional review of staged changes |
| `/tdd` | Test-driven development with red-green-refactor loop |
| `/to-issues` | Break a plan/PRD into independently-grabbable issues |
| `/to-prd` | Turn conversation context into a PRD |
| `/diagnose` | Disciplined diagnosis loop for hard bugs |
| `/prototype` | Build throwaway prototypes to flush out designs |
| `/grill-me` | Interview relentlessly about a plan or design |
| `/grill-with-docs` | Grill against existing domain model and docs |
| `/improve-codebase-architecture` | Find deepening opportunities in a codebase |
| `/setup-repo` | Configure repo for the dev-loop workflow |
| `/write-a-skill` | Create new agent skills |
| `/zoom-out` | Zoom out to module map + callers |

## The `ralph` CLI

An orchestrator that automates the full workflow loop:

```
PRD selection → triage → develop → review → user feedback → repeat
```

### Usage

```bash
make build
ralph 3  # run 3 iterations
```

## Installation

### As a Claude Code plugin (recommended)

```bash
# From the marketplace
/plugin marketplace add m-ruiz21/skills
/plugin install dev-loop

# Or test locally
claude --plugin-dir /path/to/this/repo
```

### Build from source

```bash
git clone https://github.com/m-ruiz21/skills.git
cd skills
make build
```

## Plugin Structure

```
.claude-plugin/plugin.json   # Plugin manifest
skills/                      # All skill definitions (SKILL.md + supporting files)
cmd/ralph/                   # Go source for the ralph orchestrator
bin/                         # Compiled binary (auto-added to PATH when plugin is active)
Makefile                     # `make build` to compile ralph
```

## How It Works

When installed as a Claude Code plugin:

1. **Skills load automatically** — all 14 skill folders under `skills/` become available as `/slash-commands` in Claude Code.
2. **`bin/` is added to PATH** — after `make build`, the `ralph` binary is callable directly from your terminal while the plugin is active.
3. **`ralph` orchestrates the loop** — it spawns `copilot` CLI sessions using the skills (`/triage`, `/develop-task`, `/review-diff`) in sequence, with interactive prompts for PRD selection and user feedback between review cycles.
