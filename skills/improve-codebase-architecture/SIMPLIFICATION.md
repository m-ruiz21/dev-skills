# Repository simplification audit

Feed this read-only audit into the existing deepening/opportunity workflow.
Survey first-party modules, dependency manifests, configuration, and tests across
the repository; exclude generated/vendor output from savings. Report the scope
actually examined and gaps rather than claiming exhaustive coverage.

Look for dead code/flags, hand-rolled standard-library facilities, dependencies
covered by native features, speculative factories/interfaces, and wrappers that
only delegate. File size, a single export, or a single implementation alone
does not establish waste. Trace callers, dynamic uses, compatibility contracts,
ADRs, and tests. An interface can earn its keep through isolation or locality.

Before proposing another abstraction, apply the reuse ladder and deletion test:
does removal eliminate complexity or scatter it among callers? Compare removal,
reuse, standard-library/native replacement, and consolidation/deepening. Preserve
required behavior, security, accessibility, diagnostics, and test coverage.

Add audit findings to the numbered deepening candidates, using tags **delete**,
**stdlib**, **native**, **yagni**, or **shrink**. For each include:

- Exact files/locations and caller or usage evidence.
- What to cut and what replaces it (name the concrete API/platform feature,
  or "nothing" for confirmed dead code).
- Locality/leverage and maintenance benefit, not just fewer lines.
- Risks, compatibility constraints, ADR conflicts, and behavioral checks.
- Estimated net lines/dependencies removed after necessary replacements and
  tests, with uncertainty explicit. A dependency counts only when no remaining
  consumer requires it.

Rank by meaningful maintenance benefit, confidence, and migration risk, using
larger verified reductions to prioritize otherwise comparable candidates.
Summarize non-overlapping net lines/dependencies saved; use ranges or "not
estimated" rather than invented precision. If no cut is justified, say so and
still consider genuine deepening opportunities. Report correctness/security
concerns separately instead of turning them into simplification suggestions.

Do not delete, regenerate, install, or auto-refactor anything during the audit.
The user selects a candidate before the existing grilling/design phase.
