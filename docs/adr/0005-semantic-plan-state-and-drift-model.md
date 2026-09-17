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
- Live -> Desired: visible resource consequence when this invocation
  actually performs the relevant write

Release change, drift, and resource consequence are independent. Do
not infer a deployment operation merely by comparing Live and Desired.
Ordinary modified drift alone does not run Helm (ADR-0006) and must
not be presented as a configuration edit.

### Drift

Drift is intrinsic to every semantic plan. If Previous and Live are
available, Previous -> Live is always evaluated. There is no opt-in
drift mode. When there is no drift, presentation may omit an empty
Drift section.

If Previous is present and Live is absent, report missing-resource
drift. Do not represent that as a large set of synthetic field
removals.

Missing-resource drift alone does not run Helm. If Previous and Desired
still match, the plan reports the missing object and does not show a
Create.

If Helm is already running because release intent changed, Helm
recreates a missing Desired object: show a Create with the complete
Desired manifest, and still report the missing-resource drift.

Previous absent, Live present, Desired present is resource-level
drift: Previous does not contain that logical resource, so the Live
object is unexpected. That is distinct from field-level Live-only
state on an otherwise expected resource. A field present only in Live
and absent from both Previous and Desired does not automatically
become field drift. An entire logical resource present in Live and
Desired while absent from Previous is meaningful resource-level
drift. Leftover hook runtime objects are outside this rule
(ADR-0006).

### Resource consequences

Visible resource consequences use this vocabulary:

- Create
- Update
- Delete
- Retain

None is the conceptual absence of a resource consequence. It is not a
stored action or a required output row. Do not add Replace. Immutable
field rejection and replacement feasibility are outside semantic
planning (ADR-0004).

When Previous and Live agree and Desired differs, the resource
consequence is an Update from that shared value. Example: Previous
`2`, Live `2`, Desired `3` is Update `2 -> 3` and no drift.

When Live has already moved, the resource consequence still starts
from Live if this invocation will write. Example: Previous `2`, Live
`4`, Desired `3` is Update `4 -> 3` and drift `2 -> 4`.

When only Live moved and Desired still matches Previous, that is drift
only. Example: Previous `2`, Live `4`, Desired `2` is drift `2 -> 4`
and no resource consequence.

A new resource (Previous absent, Live absent, Desired present) is
Create. Human-readable Create output shows the complete intended
manifest.

A Desired resource that already exists Live, with no Previous object,
takes its resource consequence from Live versus Desired: Live differs
from Desired is Update; Live already equals Desired is no resource
consequence. Helm ownership conflicts on that object are
execution-time failures, not planning results. Ownership presentation
is the Adoptable condition below, not a resource consequence.

A removed resource (Previous present, Desired absent, Live present) is
Delete unless Helm lifecycle leaves the object (Retain). If Live is
already absent, do not invent a Delete of an object that no longer
exists.

Release intent can change without a Live -> Desired write:

- Previous differs from Desired, Live already equals Desired: release
  intent changed, Helm may upgrade (ADR-0006), Previous -> Live is
  Drift, and there is no resource consequence. Do not invent an Update
  merely because Helm runs.
- Previous present, Desired absent, Live absent: release intent
  changed, Helm upgrade is needed to update release state, report
  missing-resource drift, and do not emit Delete. Missing Live state
  alone still does not trigger Helm.

### Retain

Retain means a resource leaves the Desired/release state, but Helm
lifecycle policy intentionally leaves the Live object in the cluster.

The primary current case is Live `helm.sh/resource-policy: keep`.
When that policy suppresses deletion:

- do not report Delete
- report Retain
- make clear the resource leaves the release but remains Live

Release change, Drift, and resource consequence stay distinct.

- If Live currently has keep, release intent may still have changed;
  the resource consequence is Retain, not Delete.
- If Previous declared keep and Live no longer has it, that removal is
  drift.
- If keep exists only in Live and was never part of Previous or
  Desired, do not classify that Live-only field itself as drift. Helm
  still honors Live keep, so a leaving resource is Retain.

### Adoptable

Adoptable is an informational ownership condition. It is not Create,
Update, Delete, or Retain. It is not a guarantee that deployment will
succeed.

It describes an existing Live logical resource that is newly entering
the Desired release footprint and whose ownership would require an
explicit ownership-taking capability if it is not already correctly
owned by this release.

Example: Previous absent, Live present, Desired present. The plan
still determines the resource consequence from Live versus Desired
(Update, or none). Independently, it may surface Adoptable.

If the Live object is already correctly owned by the same Helm
release, do not call it Adoptable merely because Previous is absent.
If ownership is missing or belongs elsewhere, surface Adoptable.

Do not predict that adoption is safe. Do not automatically take
ownership. Deployah's default Helm lifecycle uses TakeOwnership=false.
An explicit ownership-taking deploy capability is a separate
follow-up.

### Declared surface

A field that exists only in Live and is absent from both Previous and
Desired is outside Deployah's declared surface: controller-added
fields, Kubernetes defaults, injected annotations, labels, sidecars,
and other Live-only state. Such fields must not become fake removals
and must not automatically become field drift.

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

Semantic comparison may treat two representations as equal only when
Deployah has an explicit, deterministic, semantics-preserving
equivalence rule. Example: Kubernetes resource quantities whose
canonical forms are equal (`1000m` and `1`).

Such normalization is comparison-only. It must not rewrite the user
manifest, rewrite Previous release manifests, mutate Live state,
become generic API-server normalization, or guess defaulting,
admission mutation, or controller behavior.

The plan needs meaningful structural diffs: changing one field inside
an object or list should not force the whole resource to look opaque.
Live-only state outside the declared surface still must not become
synthetic removal intent. The diff library, algorithm, and
list-alignment strategy are implementation concerns, not this
decision.

JSON type changes (scalar to object, object to scalar, array to
scalar) are represented by that structural diff. Do not invent extra
semantic interpretation.

Human-readable Drift uses the same YAML-oriented diff style as
resource consequences. JSON Pointer paths are not the primary human
UX. Machine-readable output may use structural paths. Sensitive-value
presentation is ADR-0009.

### Identity

Resources with the same group, kind, namespace, and name are the same
logical resource even if the served apiVersion changes. An apiVersion
transition is part of a resource update, not an automatic Delete plus
Create. Reading the same logical object through another served
apiVersion must not produce drift solely because `/apiVersion` differs.

For a cluster-scoped logical resource, namespace is not part of
effective identity. How a raw manifest that still contains
`metadata.namespace` is left unmodified is ADR-0007.

If multiple Desired resources resolve to the same logical identity, the
planner fails rather than silently selecting one.

## Consequences

### Positive

- Operators can tell a config edit from cluster drift when both happen
  on the same object.
- Live-only noise does not show up as Deployah removals.
- A missing object is reported as missing, not as hundreds of field
  deletes.
- Helm keep is Retain: the object leaves the release and stays Live.
- Helm ownership metadata follows Helm, not a parallel rule.

### Negative

- Resource consequences and Drift must both be read. Looking only at
  resource consequences hides how Live departed from Previous.
- A previously declared object that was deleted from the cluster is
  not recreated until release intent changes (or Helm runs for some
  other declared reason). Missing-resource drift does not, by itself,
  deploy it again.
- Helm may upgrade to record release intent even when Live already
  matches Desired, with no resource consequence.
- Adoptable does not take ownership and does not mean the later deploy
  will succeed.
- Declared-surface filtering can hide Live fields the operator cares
  about until they appear in Previous or Desired.
