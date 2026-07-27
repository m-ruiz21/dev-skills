---
name: handoff
description: Compact the current conversation into a handoff document for another agent to pick up.
argument-hint: "What will the next session be used for?"
disable-model-invocation: true
---

Write a handoff document summarising the current conversation so a fresh agent can continue the work.

## Where to save

First determine whether this session is working on a feature within a project (i.e. whether a PRD exists):

- Check for a `.scratch/` directory with one or more feature folders (`.scratch/<feature-slug>/PRD.md`).
- If the session is clearly scoped to one of those features, save the handoff **inside that feature folder** as one of its documents, e.g. `.scratch/<feature-slug>/handoff.md`. This keeps it alongside the PRD, issues, and reviews.
- If there is no PRD / feature folder (or the work isn't tied to one), save the handoff to the temporary directory of the user's OS - **not** the current workspace.

## Contents

Include a "suggested skills" section in the document, which suggests skills that the agent should invoke.

Do not duplicate content already captured in other artifacts (specs, plans, ADRs, issues, commits, diffs). Reference them by path or URL instead.

Redact any sensitive information, such as API keys, passwords, or personally identifiable information.

If the user passed arguments, treat them as a description of what the next session will focus on and tailor the doc accordingly.
