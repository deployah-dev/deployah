# ADR-0003: Kubernetes delivery and lifecycle of runtime environment

## Status

Accepted

## Context

FileValues and ExplicitValues must reach the process the same way on
every workload, but ownership cannot be the same. A Deployment lives
with the Helm release. A hook Job is created and deleted around deploy.
A `deployah run` Job is not part of the release and may detach.

If run reused the release ConfigMap, two concurrent runs would race and
a failed run could rewrite env for the live app. If a hook ConfigMap
used only `before-hook-creation`, a later deploy that drops FileValues
or the task would leave the old ConfigMap behind: no new hook exists to
trigger delete. If `env` and `envFrom` were flattened in Deployah,
Kubernetes would no longer be the owner of overlap.

## Decision

Representation is the same everywhere. Ownership follows the workload.

- FileValues become a ConfigMap and `envFrom`. ExplicitValues become
  container `env:`. Deployah does not strip overlapping keys. Kubernetes
  makes `env` win over `envFrom`.
- Helm-managed Deployments, StatefulSets, and CronJobs use a stable
  `{fullname}-env` ConfigMap. The pod template checksums that object.
  String values are quoted as data. They are not Helm templates.
- Hook Jobs use the same `{fullname}-env` shape, with the same hook
  events as the Job, weight one less than the Job, and delete policy
  `before-hook-creation,hook-succeeded`. Helm executes all hooks in
  weight order and performs successful-hook cleanup after hook
  execution, so the lower-weight runtime ConfigMap remains available
  for the Job that consumes it. `hook-failed` is not set; failed-hook
  artifacts stay until a later `before-hook-creation` replace.
- `deployah run` never writes the release ConfigMap. Each invocation
  creates a GenerateName ConfigMap, points the Job `envFrom` at the
  name Kubernetes assigned, then sets a Job ownerReference (no
  `blockOwnerDeletion`). Detach is allowed only after ownership is set.
  Normal cleanup is Job TTL plus Kubernetes garbage collection.
- Partial create failures roll back best-effort: delete the Job first
  when both exist, then the ConfigMap, and report anything left behind.

## Consequences

### Positive

- Live app env and a manual run cannot overwrite each other.
- Concurrent runs of the same task get separate ConfigMaps.
- Hook retries replace the hook ConfigMap before the Job starts.
- After a successful hook event, Helm deletes the hook ConfigMap.
- Overlap behavior matches every other Kubernetes workload.

### Negative

- A hook ConfigMap can share a weight with an earlier Job in the same
  phase (`weight - 1`). That is accepted so the ConfigMap still starts
  before its own Job.
- A failed ownership patch can leave leftovers if rollback deletes also
  fail. The error names those objects.
- Run ConfigMaps are not part of `helm diff`. They exist only for that
  Job.
