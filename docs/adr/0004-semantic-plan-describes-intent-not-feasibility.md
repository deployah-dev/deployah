# ADR-0004: Semantic plan describes intent, not feasibility

## Status

Accepted

## Context

Deployah's plan was treated as a preview of the Kubernetes state a
deploy would produce. That mixed two questions: what Deployah intends
to do, and whether the API server, admission, quotas, RBAC, and
controllers would accept or mutate it.

Answering the second question pulled planning onto write dry-runs,
managedFields reconstruction, and partial results when prediction was
uncertain. A correct statement of intent then looked incomplete
whenever Kubernetes might later refuse the write.

## Decision

The semantic plan describes Deployah's intended Kubernetes operations
if this deployment runs. It is not an execution-feasibility engine and
not a Kubernetes state prediction engine. A plan may be correct even
when the later deploy fails.

These concerns are outside semantic planning:

- admission controllers and validating or mutating webhooks
- quota availability and write RBAC
- server-side apply ownership conflicts and immutable-field rejection
- Kubernetes defaulting, controller reconciliation, and final generated
  names
- the exact final API-server state

A later prediction, preflight, or feasibility capability may cover some
of those. It is not part of this contract, including under a renamed
prediction abstraction.

Semantic planning is read-only toward the cluster. GET, discovery, and
REST mapping are allowed. CREATE, UPDATE, PATCH, DELETE, and any
dry-run form of those writes are not. Dry-run mutation is not part of
Previous, Live, or Desired.

Required release and cluster state is ADR-0010. Resource consequences
are ADR-0011. API constructibility and migration are ADR-0013. Secret
presentation is ADR-0009. Deployah contract validation is ADR-0007.

## Consequences

### Positive

- Plan output stays a statement of intent, not a guess at API-server
  success.
- Planning cannot depend on write dry-runs or managedFields
  reconstruction.

### Negative

- Live replicas `4` and Desired replicas `3` may correctly show
  `4 -> 3` even if another field manager owns the field and runtime
  server-side apply fails.
- Desired replicas `100` remains valid intent even if admission later
  rejects values above `20`.
- Operators who want "will this deploy work?" need a different
  capability.
