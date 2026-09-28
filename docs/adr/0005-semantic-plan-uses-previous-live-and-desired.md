# ADR-0005: Semantic plan uses Previous, Live, and Desired

## Status

Accepted

## Context

A deploy touches three different facts: what the last Helm release
declared, what the cluster actually holds, and what the current spec
renders. Collapsing those into one "before -> after" hides whether a
difference is a config edit, cluster drift, or both.

Field-level Live noise also looks like intent if the plan treats
"absent from Desired" as a removal of Live state.

## Decision

Semantic planning uses three distinct states.

- Previous is the intent recorded by the Helm release baseline
  selected by Deployah's release-preparation semantics. On a fresh
  install, Previous is empty.
- Live is the actual current Kubernetes state.
- Desired is the deterministic Kubernetes intent generated from the
  current Deployah configuration and Helm render.

Those states answer different questions:

- Previous -> Desired: Resource Changes (ADR-0011)
- Previous -> Live: Drift

Live -> Desired is not a semantic comparison. Do not infer a
Resource Change or Drift by comparing Live and Desired. Ordinary
drift alone is not a release change (ADR-0006). Whether a requested
deployment runs is ADR-0016.

Drift needs Previous. A fresh install has no Previous, so it reports
no Drift. Unexpected-resource drift applies only to an existing
release: a Live object with no Previous record. A field present only
in Live and absent from Previous and Desired does not automatically
become field drift. Fields declared in Previous that differ in Live
are drift regardless of who changed them. A field intentionally
absent from Desired that existed in Previous is a declarative
removal: a Resource Change, not Drift by itself.

Do not report API-server bookkeeping as Resource Changes or drift:
status, uid, resourceVersion, generation, managedFields, creation
timestamps, and similar server-maintained metadata. This is not a
statement about Kubernetes managed-field ownership.

A named resource's logical identity is group, kind, effective
namespace, and name. apiVersion is not part of that identity. An
apiVersion transition is not Delete plus Create. Reading the same
logical object through another served apiVersion must not produce
drift solely because `/apiVersion` differs.

Effective namespace is part of identity. Snapshots keep the declared
content unmodified. For a namespaced resource, an omitted
`metadata.namespace` and an explicit release namespace are the same
effective namespace. For a cluster-scoped resource, effective
namespace is empty. A declared `metadata.namespace` on that object
stays in the snapshot. It is ordinary declared content: it is not
stripped, and it is not part of identity.

`metadata.generateName` without `metadata.name` has no logical
identity. It is a fallback declaration key for pairing Previous with
Desired: group, kind, effective namespace, and generateName. That key
cannot locate a Live object. Duplicate logical identities on one side
fail planning. Duplicate generateName keys on one side fail planning
because pairing is ambiguous. Setting both name and generateName
fails planning. Setting neither fails planning.

Labels and annotations in Previous or Desired are ordinary declared
fields. Comparison normalization is ADR-0015. Secret presentation is
ADR-0009. Hook runtime artifacts are outside Drift (ADR-0014).

## Consequences

### Positive

- Operators can tell a config edit from cluster drift on the same
  object.
- A fresh install reports Resource Changes and no Drift.
- Live-only noise does not show up as Deployah removals.

### Negative

- On an existing release, Resource Changes and Drift must both be
  read.
- A generateName declaration cannot be matched to a Live object by
  that key alone.
