# ADR-0004: Semantic plan describes intent, not feasibility

## Status

Accepted

## Context

A semantic plan can answer what the release declares, or whether
Kubernetes would accept the write. Those are different questions.
Admission, quotas, RBAC, webhooks, and field ownership decide the
second one. Pulling that into the plan turns a statement of intent
into a guess about a later deploy.

## Decision

The semantic plan describes release intent. It is not an
execution-feasibility engine and not a prediction of the API server's
final state. A plan may be correct even when the later deploy fails.

Resource Changes compare Previous with Desired (ADR-0011). Drift
compares Previous with Live (ADR-0005). Live does not change a
Resource Change.

Rendering Desired uses Helm's client-side dry-run and release
history. Helm discovery during that render is valid. After that
render, classifying a Resource Change uses discovery only to resolve
scope. It does not GET Live objects, and it does not call Create,
Update, Patch, or Delete, including server-side or mutating dry-run
forms of those writes.

These concerns are outside the semantic plan:

- server-side apply and server dry-run prediction
- managedFields migration
- ownership and adoption feasibility
- admission, quota, RBAC, and conflict prediction
- pre-flight checks
- how Helm writes or deletes objects

A later pre-flight capability may cover some of those. It is not part
of this contract.

Required release state is ADR-0010. API constructibility is ADR-0013.
Secret presentation is ADR-0009. Deployah contract validation is
ADR-0007. Ownership is ADR-0012.

## Consequences

### Positive

- Plan output stays a statement of intent, not a guess at API-server
  success.
- A Resource Change does not depend on Live or on a write dry-run.

### Negative

- Previous replicas `3`, Desired replicas `5`, and Live replicas `4`
  report a Resource Change of `3 -> 5`. Live `4` is Drift, not the
  change.
- Desired replicas `100` remains valid intent even if admission later
  rejects values above `20`.
- Operators who want "will this deploy work?" need a different
  capability.
