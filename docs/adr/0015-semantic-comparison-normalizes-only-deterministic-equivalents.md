# ADR-0015: Semantic comparison normalizes only deterministic equivalents

## Status

Accepted

## Context

Rendered YAML, Helm's applied representation, and Live objects often
differ in encoding without differing in meaning. Treating every byte
difference as intent invents release changes. Guessing Kubernetes
defaulting or admission as equivalence would be prediction (ADR-0004).
The declared surface is ADR-0005.

## Decision

Semantic comparison may treat two representations as equal only when
Deployah has an explicit, deterministic, semantics-preserving
equivalence rule. Kubernetes resource quantities such as `1000m` and
`1` are not one of those rules. DiffFields does not treat them as
equal, so a non-canonical quantity is a difference.

Normalization is comparison-only. It must not rewrite user manifests,
rewrite Previous Helm manifests, mutate Live objects, become generic
Kubernetes normalization, or predict admission, defaulting, webhook,
or controller behavior.

Metadata that Helm deterministically adds during its lifecycle must be
normalized between equivalent effective representations so raw
rendered YAML and Helm's applied representation do not produce
artificial release changes.

Drift compares the Previous-declared surface with Live. Maps keep
only keys declared in Previous. An empty Previous map declares no
keys, so Live keys under it are not drift. Lists are compared by
position, and extra Live elements are kept whole. Dropping those
elements would hide drift without schema-aware list semantics.
Previous `[a]` against Live `[a, b]` is Modified. A reordered list is
Modified. There is no strategic-merge, list-map-key, or OpenAPI list
semantics. `/apiVersion` is ignored for Drift.

The only absence equivalences are explicit and path-aware. An empty
`resources.limits` map on a container of an apps Deployment, an apps
StatefulSet, or a batch CronJob equals a Live object that omits that
key. A core v1 Secret folds `stringData` into `data` on the
comparison copy only: each string value is the base64 of its UTF-8
bytes, and `stringData` overrides the same `data` key. Non-string
`stringData` values stay in `stringData`. There is no generic rule
that `null`, `{}`, or `[]` equals absent for other paths, other
kinds, or CRDs.

Snapshots stay separate from comparison copies. The stored Previous
snapshot is never rewritten. Field paths and values may come from the
normalized comparison copies. Presentation renders those paths from
the field values and does not rewrite the stored Previous snapshot.

The plan needs meaningful structural comparison: changing one field
inside an object or list should not force the whole resource to look
opaque. Live-only undeclared state must not become synthetic removals
(ADR-0005). JSON type changes are represented by that structural
comparison. Do not invent extra semantic interpretation.

Human-readable Drift uses the same YAML-oriented diff style as
Resource Changes. JSON Pointer paths are not the primary human
UX. Machine-readable output may use structural paths. Exact diff
library, list-alignment algorithm, and implementation mechanism are
not this decision.

## Consequences

### Positive

- Helm ownership metadata does not appear as a fake release change.
- Drift shows the declared surface. Live-only keys under a declared
  map do not become removals.

### Negative

- Only listed, deterministic rules count as equal. Unknown encodings
  still show as differences.
- An empty `resources.limits` map does not declare keys, so a Live
  value under that map is not drift.
- A cluster-scoped object whose declared `metadata.namespace` the API
  server drops is Modified, because that field stays in Previous.
