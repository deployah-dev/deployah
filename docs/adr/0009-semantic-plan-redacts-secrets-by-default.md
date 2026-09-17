# ADR-0009: Semantic plan redacts secrets by default

## Status

Accepted

## Context

Semantic comparison needs the real Previous, Live, and Desired values.
Printing those values in plan output can leak Secret data into
terminals, logs, and CI artifacts. Comparison and presentation are
different jobs.

## Decision

Semantic planning may retain the full unredacted values required to
compare Previous, Live, and Desired correctly. Presentation is
separate.

All human-readable and machine-readable semantic-plan output is
redacted by default. This includes Create full-manifest output, Update
diffs, Drift output, and task or hook definition output where a
Kubernetes Secret is represented.

The structural shape may remain visible while sensitive values are
masked.

Revealing sensitive values always requires explicit user opt-in.
Reveal must not change semantic comparison or deployment behavior.

## Consequences

### Positive

- Plan output is safe to share and log by default.
- Comparison still uses real values, so redaction cannot hide a real
  change.

### Negative

- Default output cannot show the exact Secret payload. Operators who
  need that must opt in.
