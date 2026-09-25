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
install.

## Decision

Deployah follows the lifecycle Helm actually performs. The semantic
plan describes that lifecycle: which operations Helm would attempt,
and which it would not. The plan does not decide whether a requested
deployment runs (ADR-0016).

The plan reports a release change when the desired release differs
from the release Helm last recorded, including hook definitions.
Ordinary live drift, including a missing live object, is not by
itself a release change. A plan with no release change does not
invent resource consequences (ADR-0011). Drift stays independent
(ADR-0005). When Helm upgrades, unchanged preDeploy and postDeploy
hooks still run (ADR-0014).

CRD lifecycle is ADR-0008.

Namespace lifecycle comes from Helm install `CreateNamespace`, not a
chart Namespace manifest. Deployah enables that flag together with
server-side apply. Helm applies an implicit Namespace (name, plus a
`name` label) through the server-side apply Create path.

On install: missing Namespace is Create; existing Namespace with
differing declared implicit fields is Update; already matching is no
visible change. Live-only fields Helm did not declare must not become
removals (ADR-0005).

On a real upgrade Helm does not run CreateNamespace. Do not invent a
Namespace Create or Update from it. A missing release Namespace on
upgrade is not a planning failure. A later Helm or Kubernetes failure
is outside semantic planning (ADR-0004).

Raw or custom manifests must not define Namespace resources. That is a
Deployah contract violation (ADR-0007). Do not merge a raw Namespace
with the implicit Helm namespace operation.

A Desired resource with `metadata.generateName` is a Create that uses
that generateName. Do not fabricate the final runtime name. The
unknown final name does not make the semantic plan partial.

## Consequences

### Positive

- Plan operations match the Helm install, upgrade, and uninstall path
  Deployah actually runs, including server-side apply on install
  Create.

### Negative

- Implicit Namespace fields can change on install through Helm
  server-side apply. They do not change on upgrade.
- Raw Namespace manifests are rejected even when Kubernetes would
  accept them.
