---
name: grill-me
description: Interview the user relentlessly about a plan or design until reaching shared understanding, resolving each branch of the decision tree. Use when user wants to stress-test a plan, get grilled on their design, or mentions "grill me".
---

Interview me relentlessly about every aspect of this plan until we reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.

Ask the questions one at a time.

Before the first design question, read
[engineering discipline](../tdd/engineering.md). After tracing the relevant
code, walk the reuse ladder: is the need real, and can existing code, the
standard library, or the platform satisfy it before new abstractions or
dependencies? Give an evidence-backed recommendation, not an automatic veto.

As branches arise, probe typed states and expected failures, side-effect and
cleanup ownership, precise names/contracts, and the smallest runnable check.
Ask what measured constraint would justify extra complexity and what can be
deferred without dropping requirements. Keep the plan a small vertical
increment; publishing draft PRs requires user authorization.

If a question can be answered by exploring the codebase, explore the codebase instead.
