# ADR-0007: Deployah validates its contract, not Kubernetes feasibility

## Status

Accepted

## Context

Planning can fail in two different ways: Deployah cannot tell what it
is supposed to do, or Kubernetes might later refuse a write. Treating
those as the same thing either rejects valid intent or accepts
contradictory Deployah input.

managedFields and write dry-runs look like a way to prove ownership
and schema validity before deploy. They pull semantic planning back
into prediction (ADR-0004).

## Decision

Deployah validates Deployah's contract. It does not attempt to
validate all Kubernetes correctness.

Reject inputs that conflict with Deployah abstractions or make
behavior ambiguous:

- raw manifests that use Helm hook annotations; Deployah tasks are the
  supported hook abstraction
- duplicate logical resource identities (ADR-0005)
- a raw resource that collides with a resource Deployah generates or
  manages
- raw Namespace resources; Namespace lifecycle belongs to Deployah's
  Helm configuration (ADR-0006)
- mutually incompatible Deployah configuration, including combinations
  the spec itself defines as exclusive (for example `replicas` with
  `autoscaling.enabled`)
- ambiguity that prevents the planner from identifying or reading the
  required Live resource

Those are Deployah contract violations.

If a raw manifest does not conflict with that model, Deployah does not
need to prove Kubernetes will accept it. Generic schema validity,
admission, quotas, write permissions, immutable-field rules,
server-side apply ownership, webhooks, controllers, and other
API-server feasibility checks stay at runtime. Do not silently repair
arbitrary raw manifests.

A user may supply arbitrary cluster-scoped resources in raw manifests.
Do not reject or rewrite them merely because they are unusual.
Discovery determines effective scope and identity (ADR-0005). For a
cluster-scoped resource, namespace is not part of effective identity.
If that YAML includes `metadata.namespace`, Deployah does not rewrite
the field. Later Kubernetes rejection is a runtime concern unless a
Deployah invariant was violated.

`.deployah/crds/` is an opaque Helm-chart file boundary. Deployah
validates only the CRD source and file contract it owns. It does not
inspect CRD document semantics. Kubernetes acceptance belongs to Helm
and Kubernetes.

If the planner needs discovery, REST mapping, or a Live GET to
determine current state and the read fails, planning fails. That is
missing information, not prediction. Do not use managedFields as the
semantic source of truth for Deployah ownership.

## Consequences

### Positive

- Contradictory Deployah input fails in planning, before Helm runs.
- Generic Kubernetes refusal does not have to be anticipated in the
  plan.

### Negative

- Invalid Kubernetes YAML that does not break a Deployah rule can pass
  planning and fail at deploy.
- A cluster-scoped raw manifest may still carry `metadata.namespace`.
  Kubernetes may reject it later.
