# ADR-0006: Semantic plan follows Helm lifecycle

## Status

Accepted

## Context

Deployah ships a generated umbrella chart and extra manifests through
Helm. A parallel Kubernetes lifecycle (forced CRD upgrades, install-time
namespace creation on upgrade, planner-invented recreates) makes the
plan describe operations Helm would not attempt.

Deployah's Helm 4.3.0 install path sets `CreateNamespace` and
server-side apply. Helm then creates that implicit Namespace through
an apply PATCH. An existing target namespace can therefore change on
install. The plan follows the lifecycle Deployah actually runs.

## Decision

Deployah follows the lifecycle Helm actually performs. The semantic plan
describes the operations Deployah's real Helm execution path would
attempt. It does not invent an independent Kubernetes lifecycle for
resources whose deployment semantics come from Helm.

Helm runs when this invocation is a Helm install, or when release
intent changed (Previous vs Desired for Helm-managed resources,
including hook definitions). Ordinary Live drift, including a missing
Live object, does not by itself cause Helm to run. ADR-0005 defines
how resource consequences and drift are shown once that choice is known.

CRD lifecycle and the availability of APIs introduced by CRDs are
defined by ADR-0008.

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

### Tasks and generateName

`preDeploy` and `postDeploy` tasks are Helm hooks. A change to those
definitions is release intent change. If Helm upgrades for another
reason, unchanged hook tasks still run because Helm executes them as
part of the upgrade.

Scheduled tasks are ordinary Helm-managed resources (for example
CronJobs). A schedule task does not create an independent source of
truth for whether Helm runs. The underlying resource change is release
intent.

Task-level action is separate from Kubernetes and hook-definition
consequences (ADR-0005). Changing phase between Helm-managed phases is
a Task Update. Examples: schedule and preDeploy, preDeploy and
postDeploy.

The underlying consequences follow the actual lifecycle. schedule to
preDeploy may mean: previous CronJob Resource Delete, new preDeploy
hook definition Create, task Update. preDeploy to schedule may mean:
previous hook definition removed from the release, new CronJob
Resource Create, task Update.

Do not interpret removal of an old hook definition as a Kubernetes
Delete of leftover runtime hook Jobs.

Jobs or other objects created by executing `preDeploy` and
`postDeploy` hooks are execution artifacts. Their existence, success,
failure, or leftover presence is not declarative release drift. They
do not independently trigger Helm. Semantic planning models the
hook/task definition, the task-level action, and whether that hook
will run in this invocation. It does not treat prior runtime hook
executions as Desired release resources. Failed hook Jobs left for
inspection stay outside Drift and resource consequences.

A current manual task is outside the deploy semantic plan. It is not
shown as a deploy Task by itself, has no release Resource entry merely
because it exists in the spec, and does not trigger Helm. It is a
one-time Deployah-controlled operation outside the Helm release
lifecycle.

Transitions that change the Helm-managed footprint are visible:

- manual to schedule, preDeploy, or postDeploy: show creation of the
  new Helm-managed footprint
- schedule, preDeploy, or postDeploy to manual: show removal of the
  previous Helm-managed footprint

Do not model the resulting manual task as a Helm resource.

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
- Manual tasks stay outside the deploy plan except when a phase change
  alters the Helm-managed footprint.

### Negative

- Implicit Namespace fields can change on install through Helm
  server-side apply. They do not change on upgrade.
- Raw Namespace manifests are rejected even when Kubernetes would
  accept them.
- Leftover hook Jobs are not shown as Drift, even when they remain
  Live after a failed hook.
