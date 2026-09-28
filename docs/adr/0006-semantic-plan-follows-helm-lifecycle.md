# ADR-0006: Semantic plan follows Helm lifecycle

## Status

Accepted

## Context

Deployah ships a generated umbrella chart and extra manifests through
Helm. The plan should describe the release Helm records, not a
parallel Kubernetes lifecycle.

Helm install creates the target namespace before the release, with
CreateNamespace. That namespace is an execution prerequisite. It is
not a release object.

## Decision

Deployah follows the lifecycle Helm actually performs. The semantic
plan uses Helm lifecycle semantics to describe release changes and
their Resource Changes and task changes. The plan does not decide
whether a requested deployment runs (ADR-0016).

The plan reports a release change when the desired release differs
from the release Helm last recorded, including hook definitions.
Ordinary live drift, including a missing live object, is not by
itself a release change. A plan with no release change does not
invent Resource Changes (ADR-0011). Drift stays independent
(ADR-0005). When Helm upgrades, unchanged preDeploy and postDeploy
hooks still run (ADR-0014).

CRD lifecycle is ADR-0008.

The target namespace is an execution prerequisite created by Helm
install, outside the release. It is never a Resource Change. Planning
does not read Live to decide whether that namespace exists. A missing
namespace is not a planning failure.

Neither the Desired render nor the Previous release baseline may
declare that target namespace. If either does, planning fails with a
Deployah contract error (ADR-0007). Deployah does not filter, drop,
or rewrite the stored release or the rendered manifest. Other
Namespace names are ordinary Resource Changes.

A resource with `metadata.generateName` and no `metadata.name` is
paired by its declaration key (ADR-0005). Do not fabricate the final
runtime name.

## Consequences

### Positive

- Resource Changes follow the release Helm records.
- The target namespace stays an execution prerequisite, not a release
  object.

### Negative

- A chart or stored release that declares the target namespace fails
  planning, even when Kubernetes would accept the object.
- An existing release that already claims the target namespace fails
  planning. Deployah does not rewrite the stored manifest.
