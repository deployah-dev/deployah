# ADR-0010: Semantic plan requires release and cluster state

## Status

Accepted

## Context

A semantic plan is built from Previous and Live (ADR-0005). Rendering
Desired YAML without those states can still validate a spec. Treating
that render as a semantic plan fabricates Drift, HelmAction, and
resource consequences.

## Decision

A semantic plan requires enough release state to construct Previous
and enough cluster state to construct Live. Deployah does not support
offline semantic planning.

When that information is unavailable, the planner must not fabricate
Previous, Live, Drift, HelmAction, or resource consequences.

Cluster-independent validation and configuration resolution are
separate capabilities. They are not semantic planning.

## Consequences

### Positive

- Plan output cannot invent a HelmAction or resource consequence from
  Desired YAML alone.

### Negative

- Operators without release and cluster access cannot obtain a
  semantic plan.
