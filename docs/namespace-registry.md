# Project-to-Namespace Registry (Optional; Dedicated Namespace Recommended; K8s/OpenShift-ready)

## Goals

- Improve UX by mapping (tenant, project, environment) → Kubernetes namespace so users rarely need `--namespace`.
- Support duplicate project names across teams/tenants.
- Keep it optional and opt-in; no CRD required.
- Work on both Kubernetes and OpenShift; degrade gracefully when disabled or unavailable.

## Non-goals

- Managing secrets or other sensitive data in the registry.
- Enforcing cluster-wide RBAC policies.
- Changing Helm release naming (remains `project-environment`).

## Overview

We introduce a small, optional registry that stores where a given project/environment lives, optionally keyed by a tenant. Commands consult this registry to auto-select a namespace, so users rarely need `--namespace`. After a successful deploy, the registry is updated with the actual namespace.

- Canonical key: triple `(tenant?, project, environment?)`.
- Stored centrally in a single ConfigMap as JSON.
- Resolution order favors flags, then registry, then kube-context default.
- If keys are ambiguous (e.g., duplicate project across teams and no tenant provided), the CLI prompts to disambiguate.

## Opt-in behavior (Default: Disabled)

- The registry is disabled by default. If you do not configure a registry location, Deployah behaves as today.
- Enable it by pointing Deployah to your registry namespace and ConfigMap.
- If the registry is unreachable or permissions are insufficient, Deployah logs and continues without failing the operation.

Configuration knobs:

- **Flags**
  - `--tenant` (alias `--team`)
  - `--registry-namespace` (e.g., `deployah-system`)
  - `--registry-configmap` (default suggestion: `deployah-namespace-registry`)
- **Env vars**
  - `DEPLOYAH_TENANT`
  - `DEPLOYAH_REGISTRY_NAMESPACE`
  - `DEPLOYAH_REGISTRY_CONFIGMAP`

The registry is considered “enabled” only when `--registry-namespace` (or `DEPLOYAH_REGISTRY_NAMESPACE`) is set.

## Data model

One `ConfigMap` holds a JSON document with bindings:

```json
{
  "version": "1",
  "bindings": [
    { "tenant": "team-a", "project": "foo", "environment": "prod", "namespace": "team-a-foo-prod" },
    { "tenant": "team-a", "project": "foo", "environment": "",     "namespace": "team-a-foo" },
    { "tenant": "team-b", "project": "foo", "environment": "prod", "namespace": "team-b-foo-prod" }
  ]
}
```

- `environment` may be empty to represent a project-wide default.
- `tenant` may be empty for single-tenant usage.

## Recommended bootstrap (dedicated namespace)

Use a dedicated namespace (project on OpenShift), e.g., `deployah-system`. This keeps the registry private and avoids relying on `default` or `kube-public`.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: deployah-system
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: deployah
  namespace: deployah-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: deployah-registry-writer
  namespace: deployah-system
rules:
# For updates/reads of the specific CM by name:
- apiGroups: [""]
  resources: ["configmaps"]
  resourceNames: ["deployah-namespace-registry"]
  verbs: ["get","update","patch"]
# For initial creation (cannot be constrained by resourceNames):
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: deployah-registry-writer
  namespace: deployah-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: deployah-registry-writer
subjects:
- kind: ServiceAccount
  name: deployah
  namespace: deployah-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: deployah-namespace-registry
  namespace: deployah-system
  labels:
    app.kubernetes.io/managed-by: deployah
data:
  mappings.json: |
    {"version":"1","bindings":[]}
```

- Kubernetes: `kubectl apply -f <file.yaml>`
- OpenShift: `oc apply -f <file.yaml>` (or create the project with `oc new-project deployah-system` first)
- If you run Deployah with a user identity (not the SA), bind the RoleBinding to your user instead of the SA.

## Enabling in Deployah

Examples:

- Per-command flags:
  - `deployah status prod --tenant team-a --registry-namespace deployah-system --registry-configmap deployah-namespace-registry`
- Environment for your session/CI:

```bash
export DEPLOYAH_TENANT=team-a
export DEPLOYAH_REGISTRY_NAMESPACE=deployah-system
export DEPLOYAH_REGISTRY_CONFIGMAP=deployah-namespace-registry
```

When these are set, the registry is enabled and used; otherwise it is ignored.

## Resolution algorithm

Inputs: `tenant?`, `project`, `environment?`.

Order:

1) `--namespace` flag (if set) wins; skip registry.
2) If registry is enabled:
   - If tenant provided: try exact `(tenant, project, env)`; else `(tenant, project, "")`.
   - If tenant not provided: gather matches for `(project, env)` then `(project, "")`, dedupe by namespace.
     - 0 matches → continue.
     - 1 match → use it.
     - >1 matches → interactive prompt to pick `(tenant, namespace)`. Optional “remember” writes back only if registry enabled.
3) Fallback to kube-context default namespace.
4) As a last resort, prompt for a namespace.

Deploy-time write-back:

- After a successful non-dry-run install/upgrade, read effective namespace from release status and call `Upsert(tenant, project, env, namespace)`. If write is forbidden or fails, log and continue.

## Integration points (minimal edits)

- `internal/registry/namespace_registry.go` (new):
  - JSON-backed registry with:
    - `Resolve(tenant, project, environment)` → `{Namespace, OK, Ambiguous, Candidates}`
    - `Upsert(tenant, project, environment, namespace)` error
  - No auto-detection; constructed with an explicit `Location{Namespace, Name}` only when enabled.
- `internal/runtime/runtime.go`:
  - Helper: `HelmForNamespace(namespace string) (*helm.Client, error)`
- `internal/cmd/root.go`:
  - Add persistent flags/env: `--tenant`, `--registry-namespace`, `--registry-configmap`.
  - Registry is “enabled” iff `registry-namespace` is non-empty.
- `internal/cmd/deploy.go`:
  - After success (non-dry-run), if registry enabled: `GetReleaseStatus` → `Upsert(...)` (errors ignored/logged).
- `internal/cmd/status.go`:
  - If registry enabled, resolve namespace first; on ambiguity, prompt; else fall back to `rt.Helm()`.
- `internal/cmd/logs.go`, `internal/cmd/delete.go` (optional follow-up):
  - Use registry to preselect namespace when a project/env is known. Current flows already determine namespace after user selects a release.

## OpenShift notes

- Prefer a dedicated project (namespace) like `deployah-system` to avoid relying on `default` or `kube-public` visibility semantics.
- Cluster policies often restrict listing namespaces; this design avoids requiring `list namespaces`.
- Apply via `oc apply`; or `oc new-project deployah-system` then apply the RBAC/ConfigMap.

## Security/RBAC

- The registry stores only non-sensitive identifiers.
- Writes require rights in the dedicated namespace; reads require `get configmaps` on the specified ConfigMap.
- Failures to read/write never break Deployah operations.

Minimal Kubernetes RBAC when using a dedicated namespace is provided in the bootstrap section above.

## Testing

- Unit tests for resolve precedence, ambiguity handling, and upsert idempotency.
- Integration:
  - Disabled mode (no flags/env) → current behavior.
  - Enabled mode with dedicated `deployah-system` → status/deploy without `--namespace`.
  - OpenShift flows using a dedicated project.

## Performance

- One small ConfigMap read per command when enabled (can be cached in-memory for a short TTL).
- One write after successful deploy (optional, best-effort).

## Backwards compatibility

- No changes to release names.
- All existing flags continue to work; the registry only improves defaults.
- If the registry is absent or unreadable, behavior falls back to current logic.

## Future enhancements

- Optional `manifest.tenant` to default `--tenant` (schema stays optional/backwards-compatible).
- User-local cache `~/.config/deployah/namespace-registry.json` as a read-only fallback when cluster registry is unavailable.
-- “Remember this choice” in the ambiguity prompt to automatically upsert a default binding.
