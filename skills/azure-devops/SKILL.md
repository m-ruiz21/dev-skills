---
name: azure-devops
description: Query Azure DevOps for pull-request stats (approved / authored / reviewed) across organizations, projects, and date ranges via the Azure CLI. Use when the user asks how many PRs they (or someone) approved, authored, or reviewed in ADO, wants PR counts over a time period, or mentions Azure DevOps / ADO PR metrics. Includes the user's known orgs so "across my orgs" works without arguments.
---

# Azure DevOps

Helpers for the user's Azure DevOps work. The Azure CLI (`az`) with the `azure-devops`
extension is the right tool for listing/aggregating PRs — the ADO MCP only reads a single PR
at a time and can't filter by reviewer, vote, or date. Always prefer the bundled script over
ad-hoc `az` one-liners so counts stay consistent.

## Prerequisites

- `az login` (signed in). The script defaults its identity to the signed-in account.
- `az extension add --name azure-devops` (installed once).
- PowerShell 7+ (`pwsh`).

## The user's organizations

Short name → URL (`https://dev.azure.com/<name>`):

| Org | Primary project(s) the user works in |
|-----|--------------------------------------|
| `msdata` | Sentinel Graph |
| `msazure` | One |
| `microsoft` | WDATP |
| `medeina` | Medeina |

Full org list the account can reach: `msdata`, `msazure`, `microsoft`, `medeina`,
`ASIM-Security`, `IdentityDivision`, `msazuredev`, `mseng`, `O365Exchange`, `office`,
`onebranch`, `securityassurance`, `unifiedactiontracker`, `WindowsCyberDefense`.

`scripts/pr-stats.ps1` defaults to `msdata, msazure, microsoft, medeina` when `-Organizations`
is omitted.

## Quick start

```powershell
# How many PRs I approved this year, across my default orgs:
./scripts/pr-stats.ps1 -Role Approved -Since 2026-01-01

# Approved + authored + reviewed breakdown for one org/month:
./scripts/pr-stats.ps1 -Organizations msdata -Role All -Since 2026-06-01 -Until 2026-06-30

# Per-PR detail (id, title, date, vote), not just counts:
./scripts/pr-stats.ps1 -Organizations medeina -Projects Medeina -Role All -Detailed

# Someone else, completed PRs only:
./scripts/pr-stats.ps1 -User alias@microsoft.com -Status completed -Role Approved
```

## `scripts/pr-stats.ps1`

| Param | Default | Meaning |
|-------|---------|---------|
| `-Organizations` | the 4 default orgs | Short names or full URLs |
| `-Projects` | all projects in each org | Restrict to named projects (faster) |
| `-Role` | `Approved` | `Approved` \| `Authored` \| `Reviewed` \| `All` |
| `-User` | signed-in account | Target identity (email / uniqueName) |
| `-Since` / `-Until` | any | Filter by **PR creation date** (`yyyy-MM-dd`) |
| `-Status` | `all` | `all` \| `active` \| `completed` \| `abandoned` |
| `-IncludeApprovedWithSuggestions` | off | Count vote=5 as approved too |
| `-Detailed` | off | One row per PR instead of summary only |

### Semantics (read before trusting a number)

- **Vote codes:** `10`=Approved, `5`=Approved with suggestions, `0`=no vote, `-5`=waiting,
  `-10`=rejected. `Approved` counts `10` (plus `5` with the flag).
- **`Reviewed`** = every PR where the user was an assigned reviewer, any vote. `Approved` is a
  subset of it, so in `-Role All` a PR can appear under both.
- **Date range filters PR *creation* date**, not when the vote was cast (ADO doesn't expose a
  vote timestamp here). State this caveat when reporting.
- `--top 2000` per project; the script warns if a project hits that cap (widen with `-Projects`
  or narrower dates if so).

### How it works

Loops orgs → projects (auto-enumerated via `az devops project list` unless `-Projects` given) →
roles, calling `az repos pr list` with `--reviewer` (Approved/Reviewed) or `--creator`
(Authored), then filters votes/dates client-side and aggregates.

## Extending this skill

This is the user's home for ADO tooling. Add new scripts under `scripts/` (e.g. work-item
queries, pipeline status, PR cycle-time) and document them as new sections here.
