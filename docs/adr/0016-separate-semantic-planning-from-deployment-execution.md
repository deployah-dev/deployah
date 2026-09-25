# ADR-0016: Separate semantic planning from deployment execution

## Status

Accepted

## Context

Deployah has two responsibilities. Semantic planning describes the
release-intent transition (Previous to Desired), plus the resource and
task consequences it derives using release and cluster context. Cluster
state feeds drift and resource consequences. It is not part of the
release-intent transition. Deployment execution carries out a requested
deployment. The two must not be coupled through the semantic planning
result.

## Decision

Semantic planning is descriptive and read-only. It describes
release-intent transitions and their consequences.

Deployment execution is independent of the semantic result.
`deployah deploy` is imperative: after validation, guards, and any
required resize preparation succeed, it invokes Helm.

Deploy does not use `semantic.HelmAction`, `Plan.HasChanges()`, or any
equivalent planning result as an execution gate.

Helm selects Install or Upgrade from the release state.

When Previous equals Desired, the semantic HelmAction is HelmNone, and
an actual deploy of that existing release is a Helm Upgrade. This is
intentional.

## Consequences

- Semantic planning is independent of execution.
- HelmNone means there is no semantic Helm release-intent transition. It
  does not mean deploy avoids Helm.
- Existing releases follow Helm's upgrade path.
- Hooks follow normal Helm upgrade semantics.
- Helm may create a new revision when semantic intent is unchanged.
- Planning stays read-only.
- Execution does not depend on any planning result.
