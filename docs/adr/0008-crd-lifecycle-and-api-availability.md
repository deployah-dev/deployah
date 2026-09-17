# ADR-0008: CRD lifecycle and API availability

## Status

Accepted

## Context

Helm documentation still says existing CRDs are skipped. Deployah's
Helm 4.3.0 install path enables server-side apply, and that create
path is an apply PATCH. An existing CRD can therefore change on
install. Docs and the executed lifecycle are not the same thing.

Install-time CRD handling also decides whether a dependent custom
resource API is available when Helm builds ordinary release resources.
Logical identity across served versions (ADR-0005) is a different
question from whether Helm can REST-map the Desired GVK at that build
point. Those rules do not belong in the general Helm-lifecycle ADR.

## Decision

Deployah follows the CRD lifecycle its Helm 4.3.0 server-side apply
path actually performs. It does not invent a parallel CRD controller.

### Install, upgrade, and uninstall

Helm applies chart `crds/` objects only on install, before ordinary
release resources. With server-side apply enabled, that Create can
apply an existing CRD rather than returning AlreadyExists.

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
upgrade, and it would force conflicts. Helm's install server-side apply
path does not do those things.

### Logical identity versus Desired GVK

These are separate questions.

Logical identity is group, kind, namespace, and name (ADR-0005).
apiVersion is not part of that identity. Live lookup for a custom
resource may use another currently served version of the same group
and kind. Do not classify the object as Create solely because the
Desired apiVersion is not currently mappable.

Helm must still REST-map the Desired GVK when it builds the ordinary
target resource. That availability is required to construct the
operation. It is not admission, webhook, quota, or write-feasibility
prediction (ADR-0004, ADR-0007).

### Install-time API availability

On install, CRDs are applied before ordinary resources are built. If
that apply will deterministically serve the Desired apiVersion,
planning may proceed even when the Desired GVK is not yet
discoverable.

- Entire CRD absent: the logical custom resource is known absent. If
  install will create the CRD and serve the Desired version, show CRD
  Create and a dependent Create.
- CRD already exists and another version is currently served: read
  Live through that served version. Live absent is Create. Live
  present is Update or no visible change from Live versus Desired.
- CRD exists but no currently served or readable representation can
  establish required Live state: planning error.
- Install-time CRD apply will not make the Desired apiVersion served:
  planning error.

### Upgrade-time API availability

On a real upgrade, chart CRDs are not applied. An unmappable Desired
GVK is a planning error even if the same logical object can be read
through another served version. Do not show a CRD Update. Reading Live
through another version does not make a Desired GVK Helm cannot map
constructible.

Example: Live CRD serves `v1`, Widget/app is readable through `v1`,
Desired uses `v2`, `v2` is not served, the operation is a real Helm
upgrade. Result: planning error.

### Discovery

Failure to obtain required current API or discovery information is a
planning failure when that information is needed to construct the
plan. That remains distinct from admission, server-side apply
conflict, webhook, quota, and generic write feasibility (ADR-0004,
ADR-0007).

## Consequences

### Positive

- CRD install, upgrade, and API-availability rules live in one place.
- Logical identity and Desired GVK mapping stay separate, so a version
  transition is not mistaken for Create.
- A custom resource whose Desired apiVersion is not yet served can
  still be planned as Create when install will create the CRD that
  serves it. A later apply or admission failure remains outside the
  plan (ADR-0004).

### Negative

- Chart CRDs can change on install through Helm server-side apply.
  They do not change on upgrade. Operators who need a CRD change after
  the first install cannot get it from a Helm upgrade.
- On a real upgrade, an unmappable Desired apiVersion fails planning
  even if the logical object exists through another served version.
  Adding a CRD version is not an upgrade effect.
