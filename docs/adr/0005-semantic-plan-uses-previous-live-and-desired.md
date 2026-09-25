# ADR-0005: Semantic plan uses Previous, Live, and Desired

## Status

Accepted

## Context

A deploy touches three different facts: what the last Helm release
declared, what the cluster actually holds, and what the current spec
renders. Collapsing those into one "before -> after" hides whether a
difference is a config edit, cluster drift, or both.

Field-level Live noise also looks like intent if the plan treats
"absent from Desired" as a removal.

## Decision

Semantic planning uses three distinct states.

- Previous is the intent recorded by the Helm release baseline selected
  by Deployah's release-preparation semantics.
- Live is the actual current Kubernetes state.
- Desired is the deterministic Kubernetes intent generated from the
  current Deployah configuration and Helm render.

Those states answer different questions and are independent:

- Previous -> Desired: declarative / release intent change
- Previous -> Live: drift
- Live -> Desired: visible resource consequence when the semantic
  plan includes the corresponding write (ADR-0011)

Do not infer a release change merely by comparing Live and Desired.
Ordinary drift alone is not a release change (ADR-0006). Whether a
requested deployment runs is ADR-0016.

Drift is intrinsic. If Previous and Live are available, Previous ->
Live is always evaluated. There is no opt-in drift mode. When there
is no drift, presentation may omit an empty Drift section.

A field present only in Live and absent from Previous and Desired does
not automatically become field drift. Fields declared in Previous that
differ in Live are drift regardless of who changed them. A field
intentionally absent from Desired that existed in Previous is a real
declarative removal.

Do not report API-server bookkeeping as resource changes or drift:
status, uid, resourceVersion, generation, managedFields, creation
timestamps, and similar server-maintained metadata. This is not a
statement about Kubernetes managed-field ownership.

Previous absent, Live present, Desired present is resource-level
drift: the Live logical resource is unexpected relative to Previous.
That is distinct from field-level Live-only state on an expected
resource. Previous present and Live absent is missing-resource drift,
not synthetic field removals. Hook runtime artifacts are outside this
rule (ADR-0014). Resource consequences for those states are ADR-0011.

Resources with the same group, kind, effective namespace, and name are
the same logical resource. apiVersion is not part of that identity. An
apiVersion transition is not Delete plus Create. Reading the same
logical object through another served apiVersion must not produce
drift solely because `/apiVersion` differs. For a cluster-scoped
resource, effective namespace is empty. Duplicate Desired logical
identities fail planning.

Labels and annotations in Previous or Desired are ordinary declared
fields. Comparison normalization is ADR-0015. Secret presentation is
ADR-0009.

## Consequences

### Positive

- Operators can tell a config edit from cluster drift on the same
  object.
- Live-only noise does not show up as Deployah removals.

### Negative

- Resource consequences and Drift must both be read.
- Declared-surface filtering can hide Live fields the operator cares
  about until they appear in Previous or Desired.
