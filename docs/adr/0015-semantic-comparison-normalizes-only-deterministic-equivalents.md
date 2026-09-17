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
equivalence rule. Example: Kubernetes resource quantities whose
canonical forms are equal (`1000m` and `1`).

Normalization is comparison-only. It must not rewrite user manifests,
rewrite Previous Helm manifests, mutate Live objects, become generic
Kubernetes normalization, or predict admission, defaulting, webhook,
or controller behavior.

Metadata that Helm deterministically adds during its lifecycle must be
normalized between equivalent effective representations so raw
rendered YAML and Helm's applied representation do not produce
artificial release changes.

The plan needs meaningful structural comparison: changing one field
inside an object or list should not force the whole resource to look
opaque. Live-only undeclared state must not become synthetic removals
(ADR-0005). JSON type changes are represented by that structural
comparison. Do not invent extra semantic interpretation.

Human-readable Drift uses the same YAML-oriented diff style as
resource consequences. JSON Pointer paths are not the primary human
UX. Machine-readable output may use structural paths. Exact diff
library, list-alignment algorithm, and implementation mechanism are
not this decision.

## Consequences

### Positive

- Equivalent quantity encodings and Helm ownership metadata do not
  appear as fake release changes.

### Negative

- Only listed, deterministic rules count as equal. Unknown encodings
  still show as differences.
