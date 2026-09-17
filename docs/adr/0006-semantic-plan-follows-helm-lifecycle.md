# ADR-0006: Semantic plan follows Helm lifecycle

## Status

Accepted

## Context

Deployah ships a generated umbrella chart and extra manifests through
Helm. A parallel Kubernetes lifecycle (forced CRD upgrades, install-time
namespace creation on upgrade, planner-invented recreates) makes the
plan describe operations Helm would not attempt.

Helm's own rules are conservative on CRDs and namespaces. Ignoring them
turns the semantic plan into a second orchestrator.

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
manifest. Namespace creation comes from Helm install
`CreateNamespace`: Helm creates the release namespace if it is missing
and continues if it already exists. That create is not an update. If
the target Namespace already exists, the plan must not invent a
Namespace Create or Update from this path.

On a real upgrade Helm does not run install-time namespace creation.
The plan must not invent it. A missing namespace on upgrade is handled
like any other required current-state read (ADR-0004, ADR-0007), not by
creating the namespace.

Raw or custom manifests must not define Namespace resources. Namespace
lifecycle belongs to Deployah's Helm configuration. An independent raw
Namespace is a Deployah contract violation. Do not merge it with the
implicit Helm namespace operation.

### CRDs

Follow Helm's CRD lifecycle. Helm creates CRDs only on install, from
the chart `crds/` directory, before ordinary release resources. If a
CRD is missing on install, Helm installs it and the plan may show a CRD
Create. If the CRD already exists, Helm skips it (regardless of
version) and the plan must not invent a CRD Update.

Helm does not install CRDs on upgrade or rollback, and does not update
or delete CRDs. A changed CRD file is not a Deployah CRD Update. Do not
invent CRD pruning on uninstall.

A Deployah-specific `create-replace` CRD path (server-side apply with
forced conflicts on an existing CRD) is rejected. It is a lifecycle
beyond Helm.

On install, Helm installs CRDs before dependent custom resources. When
the CRD is currently absent and Helm will install a CRD that serves the
Desired apiVersion, the plan may show CRD Create and the dependent
Create together. That does not require a Kubernetes write dry-run, and
it does not require the custom resource API to already be discoverable.

If the Desired custom resource apiVersion is not served, and Helm's
install CRD step would not make it served, planning fails. Helm will
not upgrade the existing CRD to add that version. Do not show a CRD
Update. Do not show a custom-resource write. Failure is "required REST
mapping cannot be established" (ADR-0004), not a predicted admission
error.

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

- Plan operations match what Helm install, upgrade, and uninstall
  would attempt.
- CRD and Namespace behavior stay conservative, as Helm intended.
- Hook and schedule tasks are explained in Helm terms, not as a second
  controller.

### Negative

- An existing CRD does not pick up chart CRD changes through Deployah.
  Operators who need a CRD upgrade do that outside this lifecycle.
- A Desired custom resource version that the live CRD does not serve
  fails planning, even though the YAML may be the intended future API.
- Raw Namespace manifests are rejected even when Kubernetes would
  accept them.
- Deleted Live objects whose Desired YAML did not change are not
  recreated until Helm runs for some other release-intent reason.
