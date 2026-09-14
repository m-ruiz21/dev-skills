# Refactor Candidates

After TDD cycle, look for:

- **Dead or speculative code** → Remove when usage/contract evidence supports it
- **Custom machinery** → Check existing helpers, stdlib, and native equivalents
- **Duplication** → Reuse or extract only when it improves clarity and locality
- **Long/nested methods** → Early returns or cohesive helpers when clearer
  (keep tests on public interface, not a helper per indentation level)
- **Shallow modules** → Combine or deepen
- **Feature envy** → Move logic to where data lives
- **Primitive obsession** → Parse into meaningful types; use explicit cases
  instead of optional-field bags, without wrapping primitives gratuitously
- **Existing code** the new code reveals as problematic

Preserve behavior and required tests. Smaller diffs and fewer lines are evidence
of less maintenance only when complexity has not moved into callers or clever
expressions. Apply [engineering discipline](engineering.md) during this pass.
