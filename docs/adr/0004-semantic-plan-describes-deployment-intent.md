# ADR-0004: Semantic plan describes deployment intent

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

The plan answers what operations Deployah intends to perform. It does
not answer whether Kubernetes will accept them, whether the deploy will
succeed, or what exact objects the API server will store afterward.

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

A semantic `deployah plan` requires the release and cluster state
needed to construct Previous and Live (ADR-0005). There is no offline
semantic plan. When that state is unavailable, the planner must not
fabricate Previous, Live, Drift, HelmAction, or resource consequences.

A Desired-only render or validation capability may exist separately.
It is not a semantic deployment plan.

Semantic planning does not migrate or repair. It does not rewrite a
Previous manifest to a newer apiVersion, migrate stored Helm release
manifests, or modify the cluster so planning can succeed. When a
required historical or current API representation cannot be
constructed, planning fails with enough context to act on (ADR-0008).
Migration and repair are a separate future capability.

Planning fails when it cannot determine the deployment intent or the
required current state. Examples: discovery or REST mapping fails, a
required Live GET fails, resource identity is ambiguous, or Deployah
configuration contradicts itself. That is not a prediction that the
deploy will fail.

User-visible redaction of sensitive values is ADR-0009.

## Consequences

### Positive

- Plan output stays a statement of intent, not a guess at API-server
  success.
- Planning cannot depend on write dry-runs or managedFields
  reconstruction.
- Later feasibility work can exist without folding prediction back into
  the semantic plan.

### Negative

- Live replicas `4` and Desired replicas `3` may correctly show
  `4 -> 3` even if another field manager owns the field and runtime
  server-side apply fails.
- Desired replicas `100` remains valid intent even if admission later
  rejects values above `20`.
- Operators who want "will this deploy work?" need a different
  capability. This plan does not promise that.
- A semantic plan cannot be produced without the release and cluster
  state needed for Previous and Live.
- An unconstructible Previous or current API representation fails
  planning instead of being rewritten.
