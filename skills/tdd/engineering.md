# Engineering discipline

Read this reference at the phase specified by the calling skill. Apply the
relevant sections to the task, language, repository conventions, and existing
API contracts; do not turn it into unrelated cleanup or a mandatory framework.
This file ships with dev-loop; no separate plugin or runtime hook is required.

## Understand, then reuse

Trace the affected behavior, callers, contracts, and tests before selecting a
solution. For defects, establish the root cause and fix it at the appropriate
shared owner, checking affected callers rather than patching the named symptom
or adding guards everywhere.

Try this ladder in order, stopping at the first option that meets the actual
requirements:

1. Can an unnecessary task or speculative feature be omitted? Do not silently
   drop an explicit requirement; discuss a simpler alternative.
2. Reuse an existing repository helper, type, or pattern that really fits.
3. Prefer the language's standard library.
4. Prefer native platform capabilities (browser controls, CSS, database
   constraints, operating-system facilities).
5. Use a suitable already-installed dependency.
6. Write the smallest clear custom implementation; justify any new dependency
   against compatibility, maintenance, security, and required behavior.

Deletion and simplification are options before extraction. Remove dead code,
unused helpers, stale flags, and speculative flexibility when evidence supports
it; source history is preferable to commented-out code. Do not add factories,
configuration, interfaces, or wrappers solely for hypothetical future use.
A single implementation is a prompt to investigate, not proof that an interface
is waste: real isolation, public contracts, and test seams can justify it.
Reuse must preserve edge cases, accessibility, security, and performance needs.
Less code is not a virtue when it hides intent or moves complexity to callers.

## Readability and contracts

- Write once, read 100 times. Choose precise names, neither cryptic nor verbose.
  Methods describe **what**, not incidental implementation details; for example,
  `GetContentHash` need not expose the current hashing algorithm unless that
  algorithm is part of its contract. Do not ban legitimate domain terminology.
- Use domain/platform vocabulary accurately: Azure ARM uses `location`, not
  `region`. Name singular definitions `StorageAccountDestination`, not
  `StorageAccountsDestination`; plural collections still deserve plural names.
  Logs and errors must accurately describe the operation and outcome.
- Aim for single-responsibility functions that fit on one screen. More than two
  indentation levels is a warning; three to four is the practical maximum.
  Prefer early returns or cohesive helpers when they improve clarity, not
  fragmentation merely to meet a count. Keep lines below 120 characters.
- Default to consistent four-space indentation and named arguments where the
  language supports them and they clarify meaning. Follow the repository's
  formatter and idioms instead of imposing whitespace churn or fake named-arg
  patterns on a language that lacks them.
- Parse untrusted inputs into meaningful types at trust boundaries rather than
  repeatedly validating loose values downstream. Model distinct cases with
  discriminated unions/enums instead of bags of optional fields. Prefer total
  functions with exhaustive handling of valid cases and explicit failure cases.
- In TypeScript, narrow unknown data and correct the actual shape. Do not use
  `any` or casts to silence the compiler and defer failures to runtime.
  Justified interop assertions need a documented boundary/invariant, not a ban.
- Prefer `async`/`await` over `.then` chains where supported, preserving
  concurrency, cancellation, and failure propagation.

## Documentation

Avoid obvious or redundant what-comments and commented-out code. Use
Javadoc/JSDoc or the language-equivalent documentation for public or complex
functions to explain contracts, inputs, outputs, and failure semantics, not to
narrate implementation. Other comments should explain necessary **why**:
external dependency behavior, environmental constraints, specifications, or
requirements. Document real simplification limits and the evidence that would
justify revisiting them. Complex request/response model documentation should
include illustrative, safe synthetic values, never real credentials or data.

## Failures and side effects

- Use typed results/discriminated unions (or idiomatic explicit error returns)
  for expected failures. Reserve exceptions for unrecoverable/unreachable
  conditions where idiomatic, while preserving existing API/framework contracts.
- Catch specific exceptions at a suitable recovery/reporting boundary around
  error-prone operations. Do not blanket-catch every function, swallow errors,
  or catch only to rethrow. Translate only when it adds domain meaning; retain
  the original cause and stack. Boundary logging must include sufficient
  diagnostic context/full stack without secrets, and avoid duplicate noise.
- Contain side effects and own cleanup near the operation that acquires a
  resource, using scoped disposal/finally or equivalents. Do not burden callers
  with hidden cleanup obligations; document deliberate ownership transfers.
- Give customers accurate, actionable errors. Put referenced nouns/resource
  names and identifiers in single quotes, e.g. `Storage account 'demoaccount'
  was not found. Check the subscription and account name.` Keep internal stack
  traces in diagnostics, not customer responses. Error strings are not contracts:
  callers use stable error codes/types, not message matching.
- Bound loops, retries, and recursion at risk of runaway with budgets,
  termination conditions, timeouts, or cancellation appropriate to the operation.
  Never hide legitimate failures behind success-shaped defaults.

## Delivery and checks

Keep PRDs and changes small and cohesive; split larger work into independently
verifiable vertical increments, not speculative infrastructure layers.
Recommend early draft PR feedback when useful, but publish/push only when the
user authorizes it. Preserve explicit no-git/no-publication instructions.

Implementation is unfinished without runnable checks for non-trivial behavior.
Use existing test infrastructure, regressions, and relevant failure/edge cases;
a smoke check is not a substitute for required tests. Keep builds fast with the
smallest meaningful validation, escalating for shared contracts and risk without
weakening required gates. Preserve red-green-refactor when using TDD.

## Guardrails

These apply throughout planning, development, and review, not only at completion.
Stop unsafe actions and report evidence; never perform them to demonstrate a
finding. In review, normalize **BLOCKER** to `blocker` and **HIGH** to `high`
within the existing severity schema; retain all numeric grades and caller gates.

- **BLOCKER:** real secrets/tokens/private keys/certificates containing private
  keys or credential-bearing environment files committed or proposed for commit.
  Obvious safe examples/templates, public certificates, and public configuration
  are not secrets merely because of a filename. Never quote secret values.
- **BLOCKER:** force pushing or rewriting history of the default branch or an
  open-PR branch. Do not suggest it as routine repair or publish automatically.
- **HIGH:** suppressing analyzers, linters, or tests to force a passing build.
  Fix the root cause; distinguish justified, scoped false-positive suppression.
- **HIGH:** manually editing generated artifacts. Change the source/generator
  and regenerate with the supported tool, then verify the output.
- **HIGH:** TypeScript `any`/casts used to silence a compiler error. Fix types
  or narrow data; distinguish justified interop assertions with evidence.
