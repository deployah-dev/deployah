# ADR-0007: Semantic plan validation boundaries

## Status

Accepted

## Context

Planning can fail in two different ways: Deployah cannot tell what it
is supposed to do, or Kubernetes might later refuse a write. Treating
those as the same thing either rejects valid intent (admission, quotas,
immutable fields) or accepts contradictory Deployah input (duplicate
identities, competing namespace owners, hook annotations on raw
manifests).

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
admission policy, quotas, write permissions, immutable-field rules,
server-side apply ownership, webhooks, controllers, and other
API-server feasibility checks stay at runtime. Do not silently repair
arbitrary raw manifests. Do not rewrite user fields because Deployah
thinks Kubernetes would prefer something else.

A user may supply arbitrary cluster-scoped resources in raw manifests.
Do not reject or rewrite them merely because they are unusual. Use
discovery and scope internally only for resource identity and Live
reads, not as generic manifest normalization.

If a cluster-scoped raw manifest includes `metadata.namespace`,
Deployah does not rewrite or repair that field. For a cluster-scoped
logical resource, namespace is not part of effective identity
(ADR-0005). Live lookup uses that identity. The raw user manifest
stays unmodified. Whether Kubernetes later accepts or rejects the
namespace field is a runtime concern unless a Deployah invariant is
violated.

If that YAML later fails Kubernetes validation, that is an
execution-time outcome unless it violated a Deployah invariant.

Deployah may still validate its own input contract. Content placed in
the CRD-specific Deployah location must actually be a CRD. Duplicate
identities and Deployah-owned abstraction conflicts remain invalid.

Semantic planning does not prove that Kubernetes will accept a CRD's
schema or spec. It does not run OpenAPI feasibility checks, write
dry-runs, CRD schema acceptance prediction, or admission prediction.
If the Desired CRD declaration says an API version is served, planning
may reason from that declared intent according to ADR-0008 lifecycle
ordering. The API server may still reject the CRD later.

If the planner needs discovery, REST mapping, or a Live GET to
determine current state and the read fails, planning fails: discovery
unavailable, mapping unresolved, GET forbidden, cluster unavailable.
That is missing information needed to construct the plan, not
prediction. CRD lifecycle and custom-resource API availability are
defined by ADR-0008.

Do not use managedFields as the semantic source of truth for Deployah
ownership. Previous and Desired define the declared surface (ADR-0005).
managedFields may matter to a future feasibility capability. They are
outside this semantic plan.

## Consequences

### Positive

- Contradictory Deployah input fails in planning, before Helm runs.
- Generic Kubernetes refusal does not have to be anticipated in the
  plan.
- Cluster-scoped extras remain expressible without a Deployah
  allowlist of "normal" kinds.

### Negative

- Live replicas `4` and Desired replicas `3` still plan as `4 -> 3`
  when another field manager owns replicas. A later server-side apply
  conflict is outside the plan.
- Desired replicas `100` still plans with that value if admission will
  reject it.
- Mutating webhooks that inject sidecars, labels, annotations, or
  defaults are not predicted. They may appear later as Live-only state
  (ADR-0005).
- Invalid Kubernetes YAML that does not break a Deployah rule can pass
  planning and fail at deploy.
- A cluster-scoped raw manifest may still carry `metadata.namespace`.
  Deployah leaves that field in the user YAML. Kubernetes may reject
  it later.
- A Desired CRD may still be refused by the API server after planning
  reasoned from its declared served versions.
