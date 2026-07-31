#!/usr/bin/env pwsh
<#
.SYNOPSIS
  Fetch Azure DevOps pull-request stats for a user across organizations, projects, and a date range.

.DESCRIPTION
  Wraps `az repos pr list`. Counts PRs the user Approved / Authored / Reviewed, filtered by
  PR creation date. Requires the Azure CLI with the `azure-devops` extension, already signed in
  (`az login`). Identity defaults to the signed-in account.

.PARAMETER Organizations
  One or more ADO orgs. Accepts short names ("msdata") or full URLs
  ("https://dev.azure.com/msdata"). Defaults to the user's known orgs.

.PARAMETER Projects
  Optional project filter (names). If omitted, every project in each org is scanned.

.PARAMETER Role
  What to count: Approved (default), Authored, Reviewed, or All (breaks down all three).

.PARAMETER User
  Target identity (email / uniqueName). Defaults to the signed-in `az account` user.

.PARAMETER Since
  Only count PRs created on/after this date (yyyy-MM-dd). Optional.

.PARAMETER Until
  Only count PRs created on/before this date (yyyy-MM-dd). Optional.

.PARAMETER Status
  PR status filter passed to az: all (default), active, completed, abandoned.

.PARAMETER IncludeApprovedWithSuggestions
  When counting Approved, also count vote=5 ("Approved with suggestions"), not just vote=10.

.PARAMETER Detailed
  Emit one row per matching PR (id, project, title, date, vote) instead of only summary counts.

.EXAMPLE
  ./pr-stats.ps1 -Role Approved -Since 2026-01-01
.EXAMPLE
  ./pr-stats.ps1 -Organizations msdata,medeina -Role All -Since 2026-06-01 -Until 2026-06-30
#>
[CmdletBinding()]
param(
  [string[]]$Organizations = @('msdata','msazure','microsoft','medeina'),
  [string[]]$Projects,
  [ValidateSet('Approved','Authored','Reviewed','All')]
  [string]$Role = 'Approved',
  [string]$User,
  [datetime]$Since,
  [datetime]$Until,
  [ValidateSet('all','active','completed','abandoned')]
  [string]$Status = 'all',
  [switch]$IncludeApprovedWithSuggestions,
  [switch]$Detailed
)

$ErrorActionPreference = 'Stop'

function Resolve-OrgUrl([string]$o) {
  if ($o -match '^https?://') { return $o.TrimEnd('/') }
  return "https://dev.azure.com/$o"
}

if (-not $User) {
  $acct = az account show --query "user.name" -o tsv 2>$null
  if (-not $acct) { throw "Not signed in. Run 'az login' first." }
  $User = $acct.Trim()
}

Write-Host "User:   $User"
Write-Host "Role:   $Role"
Write-Host "Range:  $([string]::IsNullOrEmpty($Since) ? '(any)' : $Since.ToString('yyyy-MM-dd')) .. $([string]::IsNullOrEmpty($Until) ? '(any)' : $Until.ToString('yyyy-MM-dd'))"
Write-Host "Status: $Status`n"

$roles = if ($Role -eq 'All') { @('Approved','Authored','Reviewed') } else { @($Role) }
$rows = New-Object System.Collections.Generic.List[object]

foreach ($orgRaw in $Organizations) {
  $org = Resolve-OrgUrl $orgRaw
  $projList = $Projects
  if (-not $projList) {
    $projList = az devops project list --org $org --query "value[].name" -o json 2>$null | ConvertFrom-Json
    if (-not $projList) { Write-Warning "No projects (or no access) for $org"; continue }
  }

  foreach ($proj in $projList) {
    foreach ($r in $roles) {
      $azArgs = @('repos','pr','list','--org',$org,'--project',$proj,'--status',$Status,'--top','2000','-o','json')
      switch ($r) {
        'Authored' { $azArgs += @('--creator',$User) }
        default    { $azArgs += @('--reviewer',$User) }  # Approved + Reviewed both key off reviewer
      }
      $raw = az @azArgs 2>$null
      if (-not $raw) { continue }
      try { $prs = $raw | ConvertFrom-Json } catch { continue }
      if ($null -eq $prs) { continue }
      $prs = @($prs)
      if ($prs.Count -eq 2000) { Write-Warning "$org/$proj [$r]: hit 2000 cap — results may be truncated." }

      foreach ($pr in $prs) {
        $created = [datetime]$pr.creationDate
        if ($Since -and $created -lt $Since) { continue }
        if ($Until -and $created -gt $Until.Date.AddDays(1).AddSeconds(-1)) { continue }

        $vote = ($pr.reviewers | Where-Object { $_.uniqueName -eq $User } | Select-Object -First 1).vote
        if ($r -eq 'Approved') {
          $ok = ($vote -eq 10) -or ($IncludeApprovedWithSuggestions -and $vote -eq 5)
          if (-not $ok) { continue }
        }

        $rows.Add([pscustomobject]@{
          Org     = ($org -replace '^https?://dev\.azure\.com/','')
          Project = $proj
          Role    = $r
          Id      = $pr.pullRequestId
          Title   = $pr.title
          Created = $created.ToString('yyyy-MM-dd')
          Vote    = $vote
          Status  = $pr.status
        })
      }
    }
  }
}

if ($Detailed) {
  $rows | Sort-Object Org,Project,Created | Format-Table -AutoSize
}

Write-Host "`n=== Summary ($($rows.Count) matching PRs) ==="
$rows | Group-Object Role, Org, Project | ForEach-Object {
  [pscustomobject]@{ Role=$_.Group[0].Role; Org=$_.Group[0].Org; Project=$_.Group[0].Project; Count=$_.Count }
} | Sort-Object Role,Org,Project | Format-Table -AutoSize

Write-Host "--- Totals by role ---"
$rows | Group-Object Role | ForEach-Object {
  [pscustomobject]@{ Role=$_.Name; Count=$_.Count }
} | Format-Table -AutoSize

Write-Host "GRAND TOTAL: $($rows.Count)"
