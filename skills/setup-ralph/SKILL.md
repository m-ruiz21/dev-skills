---
name: setup-ralph
description: Builds and installs the `ralph` CLI tool (.NET console app) and verifies it's ready to use. Run this if `ralph` is not found on PATH, after cloning the skills repo, or if the build is stale.
disable-model-invocation: true
---

# Setup Ralph

Build and install the `ralph` CLI so it's available from any directory.

## Process

### 1. Check prerequisites

Verify the environment has what's needed:

- **dotnet SDK** — run `dotnet --version`. Must be .NET 9+. If missing, tell the user to install it from https://dot.net and stop.
- **copilot CLI** — run `which copilot` or `copilot --version`. Ralph shells out to `copilot`, so it must be on PATH. Warn if missing but don't block the build.
- **git** — run `git --version`. Needed for ralph's review workflow.

### 2. Build

From the skills repo root (the directory containing `cmd/ralph/Ralph.csproj`):

```bash
dotnet publish cmd/ralph/Ralph.csproj -c Release -o bin/ --self-contained false
```

Confirm the build succeeds with no errors.

### 3. Install to PATH

The built binary lands at `<skills-repo>/bin/Ralph`. Make it accessible:

1. Check if `<skills-repo>/bin/` is already on PATH.
2. If not, offer to add it by appending to the user's shell profile (`~/.bashrc`, `~/.zshrc`, or `~/.profile` depending on what exists):

```bash
export PATH="$PATH:<skills-repo-absolute-path>/bin"
```

3. Alternatively, offer to symlink `bin/Ralph` into a directory already on PATH (e.g. `~/.local/bin/ralph`).

Ask the user which approach they prefer. Don't pick for them.

### 4. Verify

Run `ralph --help` (or `dotnet run --project cmd/ralph/Ralph.csproj -- --help` if not yet on PATH) and confirm it prints usage info without errors.

### 5. Done

Tell the user ralph is ready. Remind them:

- Usage: `ralph <iterations>` from a repo that has `.scratch/<feature>/PRD.md` files.
- The `copilot` CLI must be available when ralph runs.
- To rebuild after changes: `make build` or `dotnet publish cmd/ralph/Ralph.csproj -c Release -o bin/ --self-contained false`.
