---
name: to-prd
description: Turn the current conversation context into a PRD and publish it to the project issue tracker. Use when user wants to create a PRD from the current context.
---

This skill takes the current conversation context and codebase understanding and produces a PRD. Do NOT interview the user — just synthesize what you already know.

The issue tracker and triage label vocabulary should have been provided to you — run `/setup-matt-pocock-skills` if not.

## Process

Before synthesis, read [engineering discipline](../tdd/engineering.md).
Use its reuse ladder and delivery/guardrail sections to choose the smallest
complete scope; carry applicable typing, failure, ownership, documentation, and
validation requirements into implementation/testing decisions. Do not copy a
generic policy checklist into every PRD.

1. Explore the repo to understand the current state of the codebase, if you haven't already. Use the project's domain glossary vocabulary throughout the PRD, and respect any ADRs in the area you're touching.

2. Sketch the existing modules to reuse or modify and any genuinely missing
behavior. Compare standard-library/native facilities and installed dependencies
before proposing custom code. Extract a deep module only when it improves
locality or hides real complexity, not merely to introduce another layer.

A deep module (as opposed to a shallow module) is one which encapsulates a lot of functionality in a simple, testable interface which rarely changes.

Check with the user that these modules match their expectations and prioritize
behavioral tests. Non-trivial implementation still requires runnable checks;
prioritization does not waive security or required validation gates.

3. Write the PRD using the template below, then publish it to the project issue tracker. For local-markdown issue trackers, derive `<feature-slug>` as lowercase ASCII letters/numbers separated by single hyphens (80 characters maximum, non-empty, and not a Windows reserved device name), then create `.scratch/<feature-slug>/PRD.md` with create-new/refuse-overwrite behavior along with `issues/`, `reviews/`, and an empty `progress.txt`. Never overwrite an unrelated existing PRD; reuse an exact match, otherwise ask for another slug. Apply the `ready-for-agent` triage label - no need for additional triage.

<prd-template>

## Problem Statement

The problem that the user is facing, from the user's perspective.

## Solution

The solution to the problem, from the user's perspective.

## User Stories

A focused, numbered list of user stories for the agreed scope. Each should be in the format of:

1. As an <actor>, I want a <feature>, so that <benefit>

<user-story-example>
1. As a mobile bank customer, I want to see balance on my accounts, so that I can make better informed decisions about my spending
</user-story-example>

Cover the agreed behavior and important failure paths without speculative
features. Keep the PRD small; split larger work into cohesive, independently
verifiable vertical increments and record deferred work explicitly.

## Implementation Decisions

A list of implementation decisions that were made. This can include:

- The modules that will be built/modified
- The interfaces of those modules that will be modified
- Technical clarifications from the developer
- Architectural decisions
- Schema changes
- API contracts
- Specific interactions
- Reuse choices and evidence justifying new dependencies/abstractions
- Valid typed states, expected failures, ownership/cleanup, and error contracts

Do NOT include specific file paths or code snippets. They may end up being outdated very quickly.

Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it within the relevant decision and note briefly that it came from a prototype. Trim to the decision-rich parts — not a working demo, just the important bits.

## Testing Decisions

A list of testing decisions that were made. Include:

- A description of what makes a good test (only test external behavior, not implementation details)
- Which modules will be tested
- Prior art for the tests (i.e. similar types of tests in the codebase)
- Runnable checks for non-trivial behavior, including relevant boundary/failure
  cases, and the smallest validation scope that preserves required gates

## Out of Scope

A description of the things that are out of scope for this PRD.

## Further Notes

Any further notes about the feature.

</prd-template>
