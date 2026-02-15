# ADR 0001: Chat Pipeline Boundaries

- Status: Accepted
- Date: 2026-02-15

## Context
`internal/app/chat_service.go` accumulated multiple responsibilities (entrypoints, tool loop, multimodal, memory flush, formatting, metrics), making behavior hard to reason about and risky to change.

## Decision
Keep package boundary (`internal/app`) unchanged, but split implementation by runtime responsibility into dedicated files.

## Consequences
- Pros:
  - Lower cognitive load per file.
  - Faster debugging for specific runtime stages.
  - Clear ownership for guard logic, multimodal, and persistence.
- Cons:
  - More files to navigate.
  - Cross-file private helper dependencies require naming discipline.

## Non-Goals
- No API contract changes.
- No data-store/infrastructure changes.
- No prompt/memory policy redesign.
