# Platform file

`deployah.platform.yaml` lives next to `deployah.yaml` and describes where
things run. The platform team owns it: it registers which environment names
exist, and maps each one to a real Kubernetes context, one or more domains, a
TLS strategy, and optional storage classes. It can also define org-wide
[profiles](#profiles) at the root. When this file is present,
`deployah deploy <environment>` only accepts names registered here. Any
component that uses `expose` or `profiles` requires it. This file is not
processed with `${...}` substitution: it holds real values, not templates.

The developer's half is [`deployah.yaml`](spec-reference.md); the
[README](../README.md#who-this-is-for) describes the boundary between them.

```yaml
apiVersion: platform/v1-alpha.3
profiles:
  default:
    nodeSelector:
      workload: general
  public-web:
    podLabels:
      tier: web
    allowedDomains: [public]
  high-security:
    securityContext:
      runAsNonRoot: true
    containerSecurityContext:
      readOnlyRootFilesystem: true
      allowPrivilegeEscalation: false
    maxResources:
      cpu: 1000m
      memory: 2Gi
environments:
  production:
    context: prod-eks
    domains:
      public:
        baseDomain: example.com
        default: true            # used when a component names no domain
        tls:
          mode: certManager
          issuer: letsencrypt-prod
      internal:
        baseDomain: internal.corp
        tls:
          mode: certManager
          issuer: letsencrypt-prod
    storageClasses:
      fast:
        className: fast-ssd
      standard:
        className: gp3
  local:
    context: kind-deployah
    domains:
      public:
        baseDomain: 127.0.0.1.nip.io
        tls:
          mode: selfSigned
```

A component's expose block resolves against the active environment's
`domains` map:

- `expose: true` (or an empty object) uses the environment's **default
  domain** and the **component name** as the subdomain: component `web` on
  `example.com` becomes `web.example.com`.
- The default domain is the environment's only domain, or the one marked
  `default: true` when there are several. Naming several domains without a
  default and omitting `expose.domain` is an error that lists the keys.
- `expose.subdomain: api` overrides the label: `api.example.com`.
- `expose.apex: true` uses the bare domain (`example.com`) instead of a
  subdomain. Only one component can hold the apex per domain.
- When an environment name is matched by wildcard prefix (e.g. `review`
  matching `review/pr-123`), a static, non-templated `expose.subdomain` warns
  by default, since every wildcard match would collide on the same hostname.
  Set `allowStaticSubdomain: true` on that platform environment to allow it.

## TLS modes

| Mode | Meaning |
|---|---|
| `selfSigned` | Deployah generates and manages a self-signed certificate. Used by the local cluster. |
| `secretName` | Use a pre-existing Kubernetes TLS secret in the target namespace. Set `secretName` to its name. |
| `certManager` | Provision the certificate through [cert-manager](https://cert-manager.io/). Set `issuer` to a `ClusterIssuer` or `Issuer` name. |

## Storage classes

Each environment can declare a `storageClasses` map: logical names that map to
real Kubernetes [StorageClass](https://kubernetes.io/docs/concepts/storage/storage-classes/)
names. This is the same idea as `domains`: the platform file owns the cluster
details; a component or profile picks a logical name instead of a
cluster-specific class string.

| Field | Notes |
|---|---|
| `storageClasses.<name>` | Logical name you choose (for example `fast` or `standard`). |
| `storageClasses.<name>.className` | The Kubernetes StorageClass name in that cluster (required). |

```yaml
environments:
  production:
    storageClasses:
      fast:
        className: fast-ssd
      standard:
        className: gp3
```

Profiles can set `storageClass` to a logical key from this map. A component
may override with `persistence.storageClass`. See
[Stateful workloads](workloads.md#stateful-workloads).

## Profiles

Profiles are org-wide workload policies defined at the **root** of
`deployah.platform.yaml` (not under an environment). A component selects one
or more by name:

```yaml
# deployah.yaml (developer)
components:
  web:
    image: ghcr.io/acme/web:1.0.0
    port: 80
    environments: [production]
    expose: true
    resourcePreset: small
    profiles: [public-web, high-security]
  api:
    image: ghcr.io/acme/api:1.0.0
    port: 8080
    environments: [production]
    # profiles omitted -> default profile applied when defined
```

```yaml
# deployah.platform.yaml (platform team)
apiVersion: platform/v1-alpha.3
profiles:
  default:
    nodeSelector:
      workload: general
  public-web:
    nodeSelector:
      workload: general
    podLabels:
      tier: web
    allowedDomains: [public]
  high-security:
    securityContext:
      runAsNonRoot: true
    containerSecurityContext:
      readOnlyRootFilesystem: true
      allowPrivilegeEscalation: false
    maxResources:
      cpu: 1000m
      memory: 2Gi
  gpu-inference:
    nodeSelector:
      accelerator: nvidia
    tolerations:
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
    storageClass: fast
environments:
  production:
    context: prod-eks
    domains:
      public:
        baseDomain: example.com
        tls:
          mode: certManager
          issuer: letsencrypt-prod
    storageClasses:
      fast:
        className: fast-ssd
```

### Profile fields

| Field | Type | Notes |
|---|---|---|
| `nodeSelector` | map of string | Kubernetes nodeSelector labels. |
| `tolerations` | list | Kubernetes tolerations (`key`, `operator`, `value`, `effect`). |
| `podLabels` | map of string | Extra labels on pods. |
| `podAnnotations` | map of string | Extra annotations on pods. |
| `securityContext` | object | Pod-level SecurityContext (passed through to the chart). |
| `containerSecurityContext` | object | Container SecurityContext applied to all containers. |
| `storageClass` | string | Logical key from the target environment's `storageClasses` map. |
| `allowedDomains` | list of string | Logical domain keys the component may expose on. Omitted (or null) means no constraint. An empty list (`[]`) is deny-all: no domain is allowed. |
| `maxResources` | object | Ceiling on component resource **requests** (`cpu`, `memory`). Exceeding it is an error. |
| `metrics` | object | Platform Prometheus policy. See [Metrics](workloads.md#metrics). Fields: `monitorLabels` (required when a component enables metrics), `monitorNamespace`, `interval`, `scrapeTimeout`, `jobLabel`, `honorLabels`, `annotations`, `relabelings`, `metricRelabelings`. |

### Merge rules

When a component lists several profiles, Deployah merges them **left to
right** (after prepending `default` when that profile exists):

| Field type | Fields | Rule |
|---|---|---|
| Maps | `nodeSelector`, `podLabels`, `podAnnotations`, `metrics.monitorLabels`, `metrics.annotations`, security contexts | Deep merge; last wins on key conflict |
| Arrays | `tolerations`, `metrics.relabelings`, `metrics.metricRelabelings` | Concatenate; identical `tolerations` entries are deduplicated |
| Scalars | `storageClass`, `metrics.monitorNamespace`, `metrics.interval`, `metrics.scrapeTimeout`, `metrics.jobLabel` | Last non-empty wins |
| Bools | `metrics.honorLabels` | Last non-nil wins |
| Domains | `allowedDomains` | Intersection of profiles that set a list; omitted means no constraint; empty list is deny-all |
| Ceilings | `maxResources` | Minimum (strictest) wins per resource |

### Default profile and opt-out

- If the platform defines a profile named `default`, Deployah always prepends
  it when the component omits `profiles` or lists other names.
- `profiles: []` means "no profiles". That is an error when a `default`
  profile exists (you cannot opt out of the org default).
- Setting `profiles` when the platform file has no `profiles` section is an
  error.

### Interaction with resources and admission

- `resourcePreset` / `resources` still set the component's requests. A
  profile's `maxResources` is only a ceiling; it does not inject defaults.
- Profiles are complementary to cluster admission policies (Pod Security
  Admission, Gatekeeper, and similar). Deployah does not integrate with those
  controllers; use both when your org needs them.

`deployah resolve` and `deployah plan` show the merged profile for each
component (names and key fields such as `nodeSelector`).

## Where the platform file comes from

- `deployah init` creates `deployah.yaml` and a platform file. If the
  platform file is missing, it writes one with every environment you
  selected: `local` gets a full Kind entry, the others are empty. If the
  file already exists, init inserts only missing environment keys and
  leaves existing keys unchanged, including an empty `local: {}`. Merge
  rewrites the file and drops hand-written comments.
- `deployah cluster up` creates `deployah.platform.yaml` when it is
  missing, with a `local` environment pointed at the local cluster. If
  the file already exists, cluster up never mutates it: it prints a
  snippet to add when `local` is absent, so comments stay intact.
- Deployah looks for the platform file in this order: `--platform-file`, the
  `DEPLOYAH_PLATFORM_FILE` environment variable, then the same directory as
  the spec file.

If a component uses `expose` and no platform file can be found, `deployah
deploy` and `deployah validate <environment>` stop with an error rather than
guessing. Use `deployah resolve <environment>` to preview the fully resolved
hostname, TLS mode, and context without touching a cluster:

```sh
deployah resolve production
deployah resolve production --output json
```

## Hostname guard

Once a component has been deployed with a resolved hostname, changing the
domain or subdomain on the next deploy is blocked by default, since it can
silently drop traffic. Pass `--force-hostname-change` to `deployah deploy` to
allow it.

See the [README](../README.md) for the project overview and the other guides.
