# ADR-0005: Semantic plan state and drift model

## Status

Accepted

## Context

A deploy touches three different facts: what the last Helm release
declared, what the cluster actually holds, and what the current spec
renders. Collapsing those into one "before -> after" hides whether a
difference is a config edit, cluster drift, or both.

Field-level Live noise (controller injects, defaults, status) also
looks like intent if the plan treats "absent from Desired" as a
removal. The plan then invents operations Helm would not perform, or
hides a missing object behind a pile of fake field deletes.

## Decision

Semantic planning uses three distinct states.

- Previous is the intent recorded by the Helm release baseline selected
  by Deployah's release-preparation semantics.
- Live is the actual current Kubernetes state. It may differ from
  Previous because of manual edits, controllers, admission mutation,
  other tools, or deletion outside Deployah.
- Desired is the deterministic Kubernetes intent generated from the
  current Deployah configuration and Helm render.

Those states answer different questions:

- Previous -> Desired: declarative / release intent change
- Previous -> Live: drift
- Live -> Desired: visible deployment consequence when this invocation
  actually performs the relevant write

Resource changes and drift are separate domain concepts. Do not infer a
deployment operation merely by comparing Live and Desired. Ordinary
modified drift alone does not run Helm and must not be presented as a
configuration edit.

### Resource changes

When Previous and Live agree and Desired differs, the Resource section
shows the intent change from that shared value. Example: Previous `2`,
Live `2`, Desired `3` is a Resource update `2 -> 3` and no drift.

When Live has already moved, the Resource section still starts from
Live if Helm will write. Example: Previous `2`, Live `4`, Desired `3`
is a Resource update `4 -> 3` and drift `2 -> 4`.

When only Live moved and Desired still matches Previous, that is drift
only. Example: Previous `2`, Live `4`, Desired `2` is drift `2 -> 4`
and no Resource update.

A new resource (Previous absent, Live absent, Desired present) is a
Create. Human-readable Create output shows the complete intended
manifest.

A Desired resource that already exists Live, with no Previous object,
is an Update from the relevant Live state to Desired. Helm ownership
conflicts on that object are execution-time failures, not planning
results.

A removed resource (Previous present, Desired absent, Live present) is
a Delete unless Helm lifecycle suppresses it. If Live is already
absent, do not invent a Delete of an object that no longer exists.

### Missing-resource drift

If Previous is present and Live is absent, report missing-resource
drift. Do not represent that as a large set of synthetic field
removals.

Missing-resource drift alone does not run Helm. If Previous and Desired
still match, the plan reports the missing object and does not show a
Create.

If Helm is already running because release intent changed, Helm
recreates a missing Desired object: show a Create with the complete
Desired manifest, and still report the missing-resource drift.

### Keep policy

Visible deletion of a Helm resource that exists in Previous but not
Desired follows Helm's use of the current Live
`helm.sh/resource-policy: keep` annotation.

- If Live currently has keep, release intent may still have changed;
  the visible Delete is suppressed.
- If Previous declared keep and Live no longer has it, that removal is
  drift.
- If keep exists only in Live and was never part of Previous or
  Desired, do not classify that Live-only field itself as drift. Helm
  still honors Live keep for deletion.

### Declared surface

A field that exists only in Live and is absent from both Previous and
Desired is outside Deployah's declared surface: controller-added
fields, Kubernetes defaults, injected annotations, labels, sidecars,
and other Live-only state. Such fields must not become fake removals
and must not automatically become drift.

This is not a statement about Kubernetes managed-field ownership. It
only means Deployah did not declare that field in Previous or Desired.

If a field existed in Previous and is intentionally absent from
Desired, that is a real declarative removal.

Do not report API-server bookkeeping as resource changes or drift:
status, uid, resourceVersion, generation, managedFields, creation
timestamps, and similar server-maintained metadata.

### Labels, annotations, and structure

Labels and annotations are normal declarative fields. A declared add,
change, or removal is a real resource change. Do not globally ignore
metadata.

Helm-generated ownership metadata is different. Metadata that Helm
deterministically adds during its lifecycle must be normalized between
equivalent effective representations so raw rendered YAML and Helm's
applied representation do not produce artificial release changes.

The plan needs meaningful structural diffs: changing one field inside
an object or list should not force the whole resource to look opaque.
Live-only state outside the declared surface still must not become
synthetic removal intent. The diff library, algorithm, and
list-alignment strategy are implementation concerns, not this
decision.

JSON type changes (scalar to object, object to scalar, array to
scalar) are represented by that structural diff. Do not invent extra
semantic interpretation.

Human-readable Drift uses the same YAML-oriented diff style as Resource
changes. JSON Pointer paths are not the primary human UX.
Machine-readable output may use structural paths.

### Identity

Resources with the same group, kind, namespace, and name are the same
logical resource even if the served apiVersion changes. An apiVersion
transition is part of a resource update, not an automatic Delete plus
Create. Reading the same logical object through another served
apiVersion must not produce drift solely because `/apiVersion` differs.

If multiple Desired resources resolve to the same logical identity, the
planner fails rather than silently selecting one.

## Consequences

### Positive

- Operators can tell a config edit from cluster drift when both happen
  on the same object.
- Live-only noise does not show up as Deployah removals.
- A missing object is reported as missing, not as hundreds of field
  deletes.
- Helm keep and Helm ownership metadata follow Helm, not a parallel
  rule.

### Negative

- Resource and Drift must both be read. Looking only at Resource hides
  how Live departed from Previous.
- A previously declared object that was deleted from the cluster is
  not recreated until release intent changes (or Helm runs for some
  other declared reason). Missing-resource drift does not, by itself,
  deploy it again.
- Declared-surface filtering can hide Live fields the operator cares
  about until they appear in Previous or Desired.
