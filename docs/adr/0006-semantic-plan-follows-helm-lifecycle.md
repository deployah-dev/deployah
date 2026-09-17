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
how Resource changes and drift are shown once that choice is known.

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

- Implicit Namespace fields can change on install through Helm
  server-side apply. They do not change on upgrade.
- Raw Namespace manifests are rejected even when Kubernetes would
  accept them.
