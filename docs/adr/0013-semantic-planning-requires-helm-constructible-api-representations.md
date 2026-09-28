# ADR-0013: Semantic planning requires Helm-constructible API representations

## Status

Accepted

## Context

Logical identity can cross apiVersions (ADR-0005). Helm still has to
REST-map the actual GVKs it builds. Chart CRD lifecycle is ADR-0008.

## Decision

Logical identity and Helm constructibility are separate. Resource
Change classification never depends on Live lookup.

Every Previous and Desired GVK is resolved through current discovery.
That mapping decides scope and whether Helm can construct the
object. It is not admission, webhook, quota, or write-feasibility
prediction (ADR-0004). If mapping fails, planning fails. The error
names the GVK and the resource. Do not classify the object by
guessing another served version.

Semantic planning does not parse `.deployah/crds/` to invent API
availability. A Desired custom resource whose CRD arrives only from
`.deployah/crds/` in the same install fails planning, because
discovery does not yet serve that GVK.

On a real upgrade, chart CRDs are not applied (ADR-0008). They cannot
invent new API availability. Helm must be able to build:

1. the Previous release representation
2. the Desired target representation

If a GVK in the current Helm release manifest is no longer served,
planning fails. If the Desired GVK cannot be mapped, planning fails.
Do not show a CRD Update on upgrade to paper over that.

Do not rewrite Previous. Do not migrate stored Helm release manifests.
Do not mutate the cluster. The planning error must identify enough
context to be actionable, such as the resource and the unavailable
GVK. Exact error strings are implementation details.

Recovery of unserved stored APIs is future work: GitHub issue
[#143](https://github.com/deployah-dev/deployah/issues/143).

## Consequences

### Positive

- Logical identity and Helm GVK mapping stay separate.
- Classification does not depend on a Live read.

### Negative

- An unconstructible Previous or Desired GVK fails planning.
- A custom resource introduced in the same install as its chart CRD
  fails planning, because that CRD is not served yet.
