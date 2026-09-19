# ADR-0013: Semantic planning requires Helm-constructible API representations

## Status

Accepted

## Context

Logical identity can cross apiVersions (ADR-0005). Helm still has to
REST-map the actual GVKs it builds. Live lookup through another served
version does not make an unmappable Previous or Desired GVK
constructible. Chart CRD lifecycle is ADR-0008.

## Decision

Logical identity, Live lookup, and Helm constructibility are separate.

Live lookup for a custom resource may use another currently served
version of the same group and kind. Do not classify the object as
Create solely because the Desired apiVersion is not currently
mappable.

Helm must REST-map the GVK it actually builds. That is required to
construct the operation. It is not admission, webhook, quota, or
write-feasibility prediction (ADR-0004). Semantic planning does not
parse `.deployah/crds/` to invent API availability.

On a real upgrade, chart CRDs are not applied (ADR-0008). They cannot
invent new API availability. Helm must be able to build:

1. the Previous/current release representation
2. the Desired target representation at the point Helm builds it

If a GVK in the current Helm release manifest is no longer served and
Helm cannot build that current resource, planning fails, even if Live
is readable through another apiVersion. If the Desired GVK cannot be
mapped at the point Helm builds it, planning fails. Do not show a CRD
Update on upgrade to paper over that.

Do not rewrite Previous. Do not migrate stored Helm release manifests.
Do not mutate the cluster. The planning error must identify enough
context to be actionable, such as the resource and the unavailable
GVK. Exact error strings are implementation details.

Recovery of unserved stored APIs is future work: GitHub issue
[#143](https://github.com/deployah-dev/deployah/issues/143).

## Consequences

### Positive

- Logical identity and Helm GVK mapping stay separate.

### Negative

- On a real upgrade, an unconstructible Previous or Desired GVK fails
  planning even if Live is readable through another served version.
