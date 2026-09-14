# Simplification lens

Augment code-quality and architecture review; never replace correctness,
security, test adequacy, plan alignment, or the five-dimension grading contract.
Read the affected implementation and consumers, not just line counts. Review
only the supplied delta; surrounding code provides context, not extra scope.

For each evidence-backed opportunity, identify the location, **what to cut**,
**what replaces it**, and why behavior/contracts survive:

- **delete** — dead helpers, unused flags, or unreachable flexibility; replacement
  may be nothing. Check dynamic uses and public compatibility before concluding
  something is unused.
- **stdlib** — custom code covered by a named standard-library API. Verify
  semantics and edge cases, not just a similar name.
- **native** — a dependency or wrapper covered by a named platform feature.
  Check supported platforms, accessibility, and operational constraints.
- **yagni** — speculative abstraction, pass-through layer, or configuration with
  no demonstrated need. A single caller/implementation is a clue, not a verdict;
  contracts, isolation, and meaningful test seams may justify it.
- **shrink** — a clearer equivalent expression or local simplification, not
  clever compression or migrating complexity into callers.

Each finding needs concrete usage/contract evidence, maintenance benefit,
replacement, and a verification path. Do not recommend deleting validation,
security/accessibility controls, cleanup, diagnostics, or required tests to save
lines. Prefer root-cause consolidation over duplicating workaround guards.
List proposals only; do not apply them during review.

Use the existing finding fields: put the tag, cut, replacement, and rationale
in `summary`; use the normal `location`. Put supporting checks and estimates
in the owning dimension's `evidence`. Do not add JSON fields or a sixth
dimension. Deduplicate overlap between code-quality and architecture findings.
Use the shared engineering guardrails for severity; mere style/size preferences
are not automatically `high`.

Report estimated **net lines saved** (removed minus added), including necessary
replacement code and tests; give a range or "not estimated" when uncertain.
Do not double-count overlapping alternatives, generated/vendor files, or tests
whose behavior must be retained. If none is justified, record that evidence and
continue the other dimensions, never infer "ship" or pass from simplicity alone.
Savings are context for maintainers, not a grade, quota, or gate.
