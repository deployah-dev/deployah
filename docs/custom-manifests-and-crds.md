# Custom manifests and CRDs

Deployah can ship raw Kubernetes YAML next to the generated Helm chart. Use
this for resources Deployah does not generate (for example a `PrometheusRule`,
a `NetworkPolicy`, or a CRD your app needs): drop manifests into
`.deployah/manifests/` and CRDs into `.deployah/crds/`.

Extra **manifests** join the same Helm release as your generated resources
(via a Helm post-renderer). Extra **CRDs** are applied to the cluster first,
outside the release, then Deployah waits for each CRD to become
`Established` before installing or upgrading the chart.

## Layout

Place files under `.deployah/` next to your `deployah.yaml`. `deployah init`
creates `.deployah/manifests/` and `.deployah/crds/` with short README files:

```text
.deployah/
  manifests/
    common-networkpolicy.yaml   # every environment
    prod/
      extra-ingress.yaml        # only when deploying to prod (or prod/*)
  crds/
    my-crd.yaml                 # shared; no per-environment subdirs
```

Rules:

- Only `*.yaml` / `*.yml` are loaded. `README*` and markdown files are skipped.
  Dotfiles (including `.old.yaml`) are skipped. Any other visible non-YAML file
  is an error.
- Subdirectories under `manifests/` must be declared environment keys. A
  subdirectory named `review` also applies when you deploy `review/pr-123`.
- Unknown directories under `manifests/` fail the deploy.
- Nested directories under a manifests env dir (or under `crds/`) are not
  allowed.
- `CustomResourceDefinition` belongs in `.deployah/crds/`, not under
  `manifests/`. CRDs must use `apiVersion: apiextensions.k8s.io/v1`.

## Literal YAML

Extra manifests are applied **literally**. There is no Helm templating, no
Sprig, and no environment-variable substitution. Content such as
`{{ $labels.instance }}` in a PrometheusRule is left untouched.

## Example: a PrometheusRule

```yaml
# .deployah/manifests/web-alerts.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: web-alerts
spec:
  groups:
    - name: web
      rules:
        - alert: WebDown
          expr: up == 0
          annotations:
            summary: instance {{ $labels.instance }} is down
```

```sh
deployah plan prod          # extras appear in the plan diff
deployah deploy prod -y     # CRDs (if any) first, then the release
```

## Labels and annotations

Deployah merges identity metadata into `metadata.labels` /
`metadata.annotations` only (never into selectors or pod templates), and never
rewrites object names:

| Object | Labels | Annotations |
|---|---|---|
| Generated (from the spec) | `deployah.dev/project`, `deployah.dev/environment`, `deployah.dev/component`, ... | `deployah.dev/source=spec`, `deployah.dev/project` |
| Extra manifests | `deployah.dev/project`, `deployah.dev/environment` | `deployah.dev/source=manifests`, `deployah.dev/project` |
| Extra CRDs | project only (no environment) | `deployah.dev/source=crds`, `deployah.dev/project` |

Reserved `deployah.dev/*` keys that Deployah does not own are stripped from
extras so they cannot impersonate managed metadata. Your other labels and
annotations are kept.

Empty `metadata.namespace` on namespaced extras is filled with the release
namespace. A different namespace is an error. Cluster-scoped objects must omit
namespace.

## Validation and collisions

- Each document needs `apiVersion`, `kind`, and `metadata.name`.
- Duplicate identities (same apiVersion/kind/namespace/name) across extras
  files fail the load.
- An extra that collides with a generated chart object fails the render
  (Deployah will not overwrite chart resources).
- Custom resource kinds must be known: put their CRD under `.deployah/crds/`,
  or have the type installed on the cluster. A small offline allowlist covers
  common operator APIs (cert-manager and prometheus-operator). With
  `deployah plan --offline`, unknown kinds are allowed so you can still
  preview; scope defaults to namespaced unless an in-repo CRD says otherwise.

## Plan vs deploy

- `deployah plan` includes extra manifests in the rendered diff. It does not
  apply CRDs; when `.deployah/crds/` is non-empty it prints how many CRDs are
  pending.
- `deployah deploy` applies CRDs first (see below), then the Helm release
  with extras attached.

## CRD policy

```sh
deployah deploy prod                  # --crds create (default): install if missing
deployah deploy prod --crds create-replace
```

| Policy | Behavior |
|---|---|
| `create` (default) | Create the CRD when it is missing; leave an existing CRD unchanged. |
| `create-replace` | Create when missing, or server-side-apply over an existing CRD (force ownership). |

Deployah waits for each CRD to report `Established` (bounded by `--timeout`),
then applies the Helm release. If the Helm plan has no changes but
`.deployah/crds/` is non-empty, Deployah still applies those CRDs (the chart is
left alone). CRDs are never pruned and are never deleted on uninstall. Extra
manifests leave with the release.

See the [README](../README.md) for the project overview and the other guides.
