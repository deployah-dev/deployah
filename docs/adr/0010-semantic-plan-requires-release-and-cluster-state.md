# ADR-0010: Semantic plan requires release and cluster state

## Status

Accepted

## Context

A semantic plan is built from Previous and Desired (ADR-0005).
Rendering Desired YAML without the release baseline can still
validate a spec. Treating that render as a semantic plan fabricates
HelmAction and Resource Changes.

## Decision

Resource Changes require enough release state to construct Previous,
and discovery to resolve scope (ADR-0013). Drift on an existing
release requires GET and LIST of Live. A fresh install needs no Live
access and reports empty Drift. A failed Live read fails planning.
There is no partial or incomplete Drift. Drift never writes to Live.
Deployah does not support offline semantic planning of an existing
release.

When that information is unavailable, the planner must not fabricate
Previous, Drift, HelmAction, or Resource Changes.

A fresh install has an empty Previous. That is known release state,
not a missing baseline.

Cluster-independent validation and configuration resolution are
separate capabilities. They are not semantic planning.

## Consequences

### Positive

- Plan output cannot invent a HelmAction or Resource Change from
  Desired YAML alone.

### Negative

- Operators without release access and discovery cannot obtain a
  semantic plan.
- Drift on an existing release needs Live, so it cannot be reported
  from the release baseline alone.
- A failed Live read fails the whole plan. There is no incomplete
  Drift result.
