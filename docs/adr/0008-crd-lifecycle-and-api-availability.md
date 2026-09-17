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
question from whether Helm can REST-map the Desired GVK, or the
Previous/current release GVK, at the point Helm builds those
resources. Those rules do not belong in the general Helm-lifecycle ADR.

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
prediction (ADR-0004, ADR-0007). Previous/current constructibility on
a real upgrade is defined below.

### Install-time API availability

On install, Helm processes chart CRDs before it builds ordinary
resources. If the Desired CRD declaration says the relevant version is
served, the planner may reason from that declared intent even when the
Desired GVK is not yet discoverable.

This is not a prediction that the API server will accept the CRD, that
the CRD schema is valid, that discovery will actually appear at
runtime, or that a dependent resource will succeed. Those remain
runtime concerns (ADR-0004, ADR-0007).

- Entire CRD absent: the logical custom resource is known absent. If
  the Desired CRD declaration says the Desired version is served, show
  CRD Create and a dependent Create.
- CRD already exists and another version is currently served: read
  Live through that served version. Live absent is Create. Live
  present is Update or no visible change from Live versus Desired.
- CRD exists but no currently served or readable representation can
  establish required Live state: planning error.
- Desired CRD declaration does not say the Desired apiVersion is
  served: planning error.

### Upgrade-time API availability

On a real upgrade, chart CRDs are not applied. They cannot invent new
API availability during that upgrade.

Helm builds the current release manifest before it constructs the
upgrade target. Both representations must be buildable:

1. the Previous/current Helm release manifest must be REST-mappable by
   Helm
2. the Desired target must be REST-mappable at the point Helm builds
   it

An unmappable Desired GVK is a planning error even if the same logical
object can be read through another served version. Do not show a CRD
Update. Reading Live through another version does not make a Desired
GVK Helm cannot map constructible.

If a GVK in the current Helm release manifest is no longer served and
Helm cannot build that current resource, planning fails. This remains
true when the same logical Live object can be read through a newer
served apiVersion. Do not rewrite Previous to a newer apiVersion. Do
not migrate stored Helm release manifests (ADR-0004). The planning
error must identify enough context to be actionable, such as the
resource and the unavailable GVK.

Example: Previous release is `example.com/v1` Widget/app. Live
Widget/app is readable through `example.com/v2`. Desired uses `v2`.
The cluster no longer serves `v1`. Result: planning error, because
Helm cannot construct its current release resource set.

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
  still be planned as Create when the Desired CRD declaration says that
  version is served and Helm's install path processes that CRD first.
  The API server may still reject the CRD (ADR-0004).

### Negative

- Chart CRDs can change on install through Helm server-side apply.
  They do not change on upgrade. Operators who need a CRD change after
  the first install cannot get it from a Helm upgrade.
- On a real upgrade, an unmappable Desired apiVersion fails planning
  even if the logical object exists through another served version.
  Adding a CRD version is not an upgrade effect.
- On a real upgrade, an unconstructible Previous/current GVK fails
  planning even if Live and Desired use a served version. Semantic
  planning does not rewrite stored Helm release manifests.
