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

Drift actions are Modified, Missing, and Unexpected. They are not
create, update, or delete.

- Modified: Previous and Live both exist and the declared surface
  differs. Both snapshots are present and Fields is non-empty.
- Missing: Previous exists and Live does not. Previous is present,
  Live is absent, and Fields is empty.
- Unexpected: a release-owned Live object has no Previous logical
  identity. Previous is absent, Live is present, and Fields is empty.

Modified and Missing name the Previous declaration: its apiVersion,
kind, effective namespace, and name. Unexpected names the observed
Live object. An apiVersion transition alone is not Drift, and it is
not Missing plus Unexpected. A modified or missing entry keeps the
Previous apiVersion even when Live was read through another version.

A fresh install has no Previous. It reads no Live, and its Drift is
empty. A Live object that already uses a Desired name is not Drift
on a fresh install.

On an existing release, each named Previous declaration is read with
GET through that declaration's own REST mapping, effective namespace,
and name. Identity is not resolved through Desired.

Unexpected objects are listed only in buckets taken from Previous:
group, kind, and effective namespace. Each unambiguous bucket is
listed once, through the highest Previous version in that bucket,
with the label selector `deployah.dev/instance` equal to the release
name. An object is Unexpected only when all of these hold: the
instance label matches the release, `deployah.dev/source` is `spec`
or `manifests`, there is no `helm.sh/hook` annotation,
`meta.helm.sh/release-name` and `meta.helm.sh/release-namespace`
match the release, the logical identity is not in Previous, and the
object is not the target Namespace. Chart CRDs and Helm hook runtime
resources are not Previous declarations, so they are outside this
scope. A bucket that contains a generateName-only Previous
declaration is not listed. Named resources in that bucket are still
read with GET.

Drift does not change HelmAction, Resource Changes, tasks, chart
CRDs, Summary, or HasEffects. A plan can be a no-op and still list
Drift.

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
