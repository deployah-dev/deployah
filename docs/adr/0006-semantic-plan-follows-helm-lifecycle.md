# ADR-0006: Semantic plan follows Helm lifecycle

## Status

Accepted

## Context

Deployah ships a generated umbrella chart and extra manifests through
Helm. A parallel Kubernetes lifecycle (forced CRD upgrades, install-time
namespace creation on upgrade, planner-invented recreates) makes the
plan describe operations Helm would not attempt.

Helm documentation still says existing CRDs are skipped. Deployah's
Helm 4.3.0 install path sets `CreateNamespace` and server-side apply.
Helm then creates those objects with server-side apply enabled, and
that create path is an apply PATCH. Existing namespaces and CRDs can
therefore change on install. The plan follows the lifecycle Deployah
actually runs, not the skip-if-present wording alone.

## Decision

Deployah follows the lifecycle Helm actually performs. The semantic plan
describes the operations Deployah's real Helm execution path would
attempt. It does not invent an independent Kubernetes lifecycle for
resources whose deployment semantics come from Helm.

Helm runs when this invocation is a Helm install, or when release
intent changed (Previous vs Desired for Helm-managed resources,
including hook definitions). Ordinary Live drift, including a missing
Live object, does not by itself cause Helm to run. ADR-0005 defines
how Resource changes and drift are shown once that choice is known.

### Namespace

Deployah's generated umbrella chart does not define a Namespace
manifest. Namespace lifecycle comes from Helm install
`CreateNamespace`. Deployah enables that flag together with
server-side apply, and Helm applies an implicit Namespace (name, plus
a `name` label) through the server-side apply Create path.

On install:

- target Namespace missing: visible Create
- target Namespace exists and Helm's declared implicit fields differ
  from Live: visible Update
- target Namespace already matches those declared fields: no visible
  change
- Live-only fields Helm did not declare must not become removals
  (ADR-0005)

On a real upgrade Helm does not run the install-time CreateNamespace
path. The plan must not invent a Namespace Create or Update from it. A
missing release Namespace on upgrade is not a planning failure. It
means there is no Namespace Create intent. A later Helm or Kubernetes
failure is outside semantic planning (ADR-0004).

Raw or custom manifests must not define Namespace resources. Namespace
lifecycle belongs to Deployah's Helm configuration. An independent raw
Namespace is a Deployah contract violation. Do not merge it with the
implicit Helm namespace operation.

### CRDs

Follow the CRD lifecycle Helm actually performs on Deployah's path.
Helm applies chart `crds/` objects only on install, before ordinary
release resources. With server-side apply enabled, that Create can
apply or update an existing CRD instead of returning AlreadyExists.

On install:

- CRD absent: Create
- CRD exists and declarative CRD state differs: Update using Helm's
  server-side apply install behavior
- CRD already converged: no visible change

On a real upgrade, chart CRDs are not installed or upgraded. On
uninstall, Helm does not prune CRDs. Do not invent either.

A Deployah-specific `create-replace` CRD path is rejected. It is a
lifecycle beyond Helm: it would update CRDs on upgrade, and it would
force conflicts. Helm's install server-side apply path does not do
those things.

On install, Helm applies CRDs before dependent custom resources. That
does not require a Kubernetes write dry-run. The dependent object is
the same logical resource across served apiVersions (ADR-0005). Helm's
install-time CRD apply may make the Desired version served. Do not
infer Create solely because that Desired version was previously
unmappable.

- Entire CRD absent: the dependent custom resource is known absent.
  Show the CRD Create and a dependent Create. The Desired custom
  resource API need not already be discoverable.
- CRD exists and a currently served version can establish Live: read
  the logical object through that served version. Emit Create or Update
  from actual Live existence and state.
- CRD exists but no currently served or readable representation allows
  Live to be established: planning fails. That is missing current state
  (ADR-0004, ADR-0007), not a predicted admission error.

A real upgrade does not apply chart CRDs. Do not invent a CRD Update
on upgrade. Dependent custom resources still use the Live rules above.

### Tasks and generateName

`preDeploy` and `postDeploy` tasks are Helm hooks. A change to those
definitions is release intent change. If Helm upgrades for another
reason, unchanged hook tasks still run because Helm executes them as
part of the upgrade.

Scheduled tasks are ordinary Helm-managed resources (for example
CronJobs). A schedule task does not create an independent source of
truth for whether Helm runs. The underlying resource change is release
intent.

A Desired resource with `metadata.generateName` (for example
`migrate-`) is a Create that uses that generateName. Do not fabricate a
runtime-generated final name. The unknown final name does not make the
semantic plan partial.

## Consequences

### Positive

- Plan operations match the Helm install, upgrade, and uninstall path
  Deployah actually runs, including server-side apply on install
  Create.
- Hook and schedule tasks are explained in Helm terms, not as a second
  controller.

### Negative

- Implicit Namespace fields and chart CRDs can change on install
  through Helm server-side apply. They do not change on upgrade.
  Operators who need a CRD change after the first install cannot get it
  from a Helm upgrade.
- An unserved Desired apiVersion does not, by itself, fail planning
  when Live can be read through another served version of the same
  group and kind. The later apply may still fail (ADR-0004).
- Raw Namespace manifests are rejected even when Kubernetes would
  accept them.
- Deleted Live objects whose Desired YAML did not change are not
  recreated until Helm runs for some other release-intent reason.
