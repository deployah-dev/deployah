# ADR-0008: Semantic plan follows Helm CRD lifecycle

## Status

Accepted

## Context

Helm documentation still says existing CRDs are skipped. Deployah's
Helm 4.3.0 install path enables server-side apply, and that create
path is an apply PATCH. An existing CRD can therefore change on
install. Docs and the executed lifecycle are not the same thing.

## Decision

Deployah follows the CRD lifecycle its Helm 4.3 server-side apply path
actually performs. It does not invent a parallel CRD controller.

`.deployah/crds/` is opaque root-chart `crds/` input. Deployah
preserves the source files and does not interpret or mutate their
Kubernetes semantics. Helm owns CRD lifecycle.

Helm applies chart `crds/` objects only on install, before ordinary
release resources. With server-side apply enabled, that Create can
apply an existing CRD rather than returning AlreadyExists.

Install-time CRD processing may be disabled by execution intent.
Disabling it does not remove files from the chart. Deployah does not
Create, Apply, Replace, Patch, or Delete chart CRDs itself. Deployah
does not inject Deployah identity labels or annotations into chart
CRDs.

On a real upgrade, chart CRDs are not applied. A CRD newly added after
the initial install is not installed by an ordinary Helm Upgrade.
Upgrade and rollback do not process chart CRDs. Uninstall does not
delete chart CRDs. Do not invent CRD pruning or deletion. CRD source
files do not participate in a parallel Deployah CRD lifecycle.

A Deployah-specific `create-replace` CRD path is rejected as target
architecture. It is a lifecycle beyond Helm: it would update CRDs on
upgrade, and it would force conflicts.

Availability of API representations for dependent resources is
ADR-0013.

## Consequences

### Positive

- CRD install, upgrade, and uninstall follow the Helm path Deployah
  actually runs.

### Negative

- Chart CRDs can change on install through Helm server-side apply.
  They do not change on upgrade. A CRD added after the first install
  is not installed by an ordinary upgrade. Operators who need a CRD
  change after the first install cannot get it from a Helm upgrade.
- Disabling install-time CRD processing on first install leaves chart
  CRDs uninstalled. Later ordinary upgrades will not install them.
