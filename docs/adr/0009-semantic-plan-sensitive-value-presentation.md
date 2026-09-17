# ADR-0009: Semantic plan sensitive-value presentation

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

By default, user-visible and machine-readable semantic-plan output
must redact sensitive Secret values. This applies to Create
full-manifest output, Update diffs, Drift output, task and hook
definition output where a Kubernetes Secret is represented, and
machine-readable output unless that format is explicitly designed
otherwise.

The structural shape may remain visible while sensitive values are
masked.

The architecture must support an explicit user opt-in to reveal
sensitive values. Default is redacted. Reveal is opt-in. Reveal must
not change semantic comparison or deployment behavior.

## Consequences

### Positive

- Plan output is safe to share and log by default.
- Comparison still uses real values, so redaction cannot hide a real
  change.

### Negative

- Default output cannot show the exact Secret payload. Operators who
  need that must opt in.
