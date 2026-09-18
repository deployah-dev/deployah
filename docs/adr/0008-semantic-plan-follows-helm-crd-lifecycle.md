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

Helm applies chart `crds/` objects only on install, before ordinary
release resources. With server-side apply enabled, that Create can
apply an existing CRD rather than returning AlreadyExists.

Deployah loads `.deployah/crds/` without mutating CRD metadata, copies
those files into the per-invocation chart `crds/` directory, and lets
Helm 4.3 Install process them. `--skip-crds` maps to Helm
`Install.SkipCRDs`. Skip leaves the files in the chart. It is not a
new HelmAction and invents no CRD lifecycle. Deployah does not
Create, Apply, Replace, Patch, or Delete chart CRDs itself. Origin is
tracked internally from `.deployah/crds/` / `Bundle.CRDs`. Deployah
does not inject Deployah identity labels or annotations into chart
CRDs.

On install:

- CRD absent: visible Create
- CRD exists and its declared state differs: visible Update according
  to Helm's install-time server-side apply behavior
- CRD exists and is already converged: no visible resource change

Live-only fields outside the declared surface must not become
synthetic removals (ADR-0005).

On a real upgrade, chart CRDs are not applied. Do not invent a CRD
Create or Update. On uninstall, do not invent CRD pruning or deletion.

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
  They do not change on upgrade. Operators who need a CRD change after
  the first install cannot get it from a Helm upgrade.
- A first install with skip leaves chart CRDs uninstalled. Later
  ordinary upgrades will not install them.
