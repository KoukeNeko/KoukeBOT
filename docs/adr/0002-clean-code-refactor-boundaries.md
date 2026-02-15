# ADR 0002: Clean Code Refactor Boundaries

- Status: Accepted
- Date: 2026-02-15

## Context
The codebase has several high-churn files with mixed responsibilities across runtime composition, API settings orchestration, OAuth flow logic, and web page controllers. This increases the cost and risk of maintenance and feature updates.

## Decision
Perform a behavior-preserving clean-code refactor focused on responsibility splits, type safety, and testability.

### Hard compatibility constraints
- Keep existing HTTP routes, methods, and JSON keys unchanged.
- Keep persisted data formats unchanged (`data/settings.json`, OAuth pool/token files, conversation files).
- Keep domain behavior unchanged unless explicitly bug-fixing existing lint/type/runtime issues.

### Refactor strategy
- Split large files by responsibility while preserving package-level APIs.
- Introduce internal helper interfaces only where needed for testability.
- Keep app-facing facade types and entrypoints stable.
- Add missing frontend test runner and minimal focused tests for high-risk logic.

## Consequences
### Positive
- Lower cognitive load per file.
- Better targeted testing and safer future changes.
- Clear separation of DTO/validation/patch/runtime-apply concerns.

### Negative
- More files and imports to navigate.
- Temporary migration overhead while moving code between files.

## Non-goals
- No product feature expansion as part of refactor.
- No API redesign.
- No storage migration.
- No large UI visual redesign.
