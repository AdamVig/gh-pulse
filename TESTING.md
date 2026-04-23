# Testing Strategy

This project favors behavior-focused tests over line-coverage chasing.

## Goals

- Keep tests fast and deterministic.
- Protect user-visible behavior and critical transitions.
- Avoid coupling tests to incidental implementation details.

## Coverage Policy

- Treat coverage as a health signal, not a hard goal.
- Maintain a practical floor around `90%+` for the package.
- It is acceptable to reduce coverage when that improves readability
  and refactorability without losing meaningful behavioral checks.

## Preferred Tests

- CLI behavior (`--org`, help output).
- Transition/event behavior (`dequeued`, `merged`, `conflicts`,
  `checks failed`).
- Poll-loop resilience (transient failures, rate limit handling,
  non-fatal notifier errors).
- CLI entry behavior (`--once`, startup failures, subprocess exit
  behavior).

## Avoid

- Tests that only prove a line executed with no behavioral value.
- Overly brittle assertions on full GraphQL query text.
- Excessive dependence on exact internal call sequencing unless order
  is the behavior under test.
