# Custom manifests and CRDs

Deployah can ship raw Kubernetes YAML next to the generated Helm chart. Use
this for resources Deployah does not generate (for example a `PrometheusRule`,
a `NetworkPolicy`, or a CRD your app needs): drop manifests into
`.deployah/manifests/` and CRDs into `.deployah/crds/`.

Extra **manifests** join the same Helm release as your generated resources
(via a Helm post-renderer). Extra **CRDs** are copied literally into the
generated chart's `crds/` directory. Deployah parses each YAML document
only for presentation identity (`kind` and `metadata.name`) so plan
output can name the CRD. It does not inspect CRD spec, infer
custom-resource scope, or decide API availability from CRD contents.
Raw file bytes stay unchanged for Helm. Helm and Kubernetes process the
files at runtime. Helm installs them on a fresh release, before ordinary
resources, unless you skip install-time processing. Deployah does not
apply, patch, or delete those CRDs itself. Plan identity parsing is not
CRD lifecycle ownership.

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
  `manifests/`.

## Literal YAML

Extra manifests and CRDs are loaded **literally**. There is no Helm
templating, no Sprig, and no environment-variable substitution. Content such
as `{{ $labels.instance }}` in a PrometheusRule is left untouched.

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
deployah deploy prod -y     # Helm install processes chart CRDs, then the release
```

## Labels and annotations

Deployah merges identity metadata into `metadata.labels` /
`metadata.annotations` only (never into selectors or pod templates), and never
rewrites object names:

| Object | Labels | Annotations |
|---|---|---|
| Generated component | `deployah.dev/project`, `deployah.dev/environment`, `deployah.dev/component`, `deployah.dev/instance` | `deployah.dev/source=spec`, `deployah.dev/project`, `deployah.dev/environment-instance` |
| Generated task | same as a component, plus `deployah.dev/task` (the task name). Components never carry this key. | `deployah.dev/source=spec`, `deployah.dev/project`, `deployah.dev/environment-instance` |
| Extra manifests | `deployah.dev/project`, `deployah.dev/environment`, `deployah.dev/instance` | `deployah.dev/source=manifests`, `deployah.dev/project`, `deployah.dev/environment-instance` |
| Extra CRDs | none injected | none injected |

Chart CRDs stay semantically yours. Deployah does not add `deployah.dev/*`
keys, and it does not strip user `deployah.dev/*` keys that you wrote on the
CRD. Origin is tracked internally from `.deployah/crds/`, not from object
metadata.

`deployah.dev/environment` is the logical environment (`review`). Concrete
wildcard instances such as `review/pr-123` are stored in the
`deployah.dev/environment-instance` annotation (annotations may contain `/`).
`deployah.dev/instance` is the Helm-safe exact deployment identity (the
release name). Helm still sets `app.kubernetes.io/instance` on generated
chart resources. Extra manifests do not take ownership of that conventional
label: if you set it, Deployah leaves it alone.

Reserved `deployah.dev/*` keys that Deployah does not own are stripped from
extra **manifests** so they cannot impersonate managed metadata. Your other
labels and annotations on manifests are kept.

Empty `metadata.namespace` on namespaced extra **manifests** is filled with
the release namespace. A different namespace is an error. Cluster-scoped
manifests must omit namespace.

CRD YAML keeps `metadata.namespace` if you set it. Deployah does not reject
or clear that field. Kubernetes or Helm may still refuse a namespaced CRD
later.

## Validation and collisions

- Each extra **manifest** document needs `apiVersion`, `kind`, and
  `metadata.name`.
- Duplicate extra **manifests** (same apiVersion/kind/namespace/name) fail
  the load.
- Each extra **CRD** document needs `kind: CustomResourceDefinition` and
  `metadata.name`. YAML must be parseable enough to read those fields.
  Empty YAML documents are ignored. Deployah does not require a CRD
  `apiVersion`, does not inspect `spec`, and does not reject duplicate
  CRD names. Helm and Kubernetes process the files at runtime and may
  reject invalid or unsupported content.
- An extra that collides with a generated chart object fails the render
  (Deployah will not overwrite chart resources).
- Custom resource kinds must be known to Deployah's built-in table or
  live cluster discovery. A small offline allowlist covers common
  operator APIs (cert-manager and prometheus-operator). Deployah does
  not read `.deployah/crds/` to learn a custom resource's type or
  scope. With `deployah plan --offline`, unknown kinds are allowed so
  you can still preview; scope defaults to namespaced.
- Helm hook annotations (`helm.sh/hook`, `helm.sh/hook-weight`,
  `helm.sh/hook-delete-policy`) are not supported on custom manifests. Use a
  Deployah `preDeploy` or `postDeploy` task for deploy hooks.

## Plan vs deploy

- `deployah plan` includes extra manifests in the rendered diff. It does
  not apply CRDs. Chart CRDs appear as lifecycle entries with `kind` and
  `metadata.name`. On a fresh install the plan shows each CRD document
  Helm will process. It does not claim Kubernetes will create versus
  apply the object. On upgrade, CRDs are listed as present in the chart
  but not processed.
- `deployah deploy` copies those CRD files into the generated chart, then
  runs Helm. On a fresh install Helm processes `crds/` before ordinary
  resources. On upgrade Helm leaves chart CRDs alone, including CRDs added
  after the first install.

## Helm CRD lifecycle

```sh
deployah deploy prod                  # fresh install: Helm installs chart CRDs
deployah deploy prod --skip-crds      # fresh install: Helm skips chart CRDs
```

`--skip-crds` maps to Helm `Install.SkipCRDs`. The files stay in the chart.
Skip is not a Deployah CRD writer, and it does not apply on upgrade.

Helm 4.3 install-time CRD handling, with server-side apply enabled, works
like this:

- Fresh install: Helm processes chart CRDs unless you pass `--skip-crds`.
- Existing chart CRD: the install path may apply changes to it. Helm's
  Create call uses server-side apply, so an already-present CRD can be
  updated during that first install.
- Upgrade: Helm does not process chart CRDs.
- Rollback: chart CRDs are not reprocessed.
- Uninstall: chart CRDs remain.
- CRD added after the first install: an ordinary Upgrade does not install
  it.

Extra manifests leave with the release.

If a Helm release already exists and you later add a new CRD file under
`.deployah/crds/`, a normal `deployah deploy` performs a Helm Upgrade and
that new CRD is not installed.

If you skip CRDs on the first install, later ordinary upgrades will not
install them either. Use a first install without `--skip-crds` when the
release needs those APIs.

See Helm's [Limitations on CRDs](https://helm.sh/docs/topics/charts/#limitations-on-crds).

## Failure modes

Helm's install-time CRD step and the rest of the release are **not** one
atomic operation. Helm installs chart CRDs first (unless skipped), waits for
each to become `Established`, then applies ordinary resources.

After a successful install, later ordinary deploys are upgrades. Helm
upgrade does not process chart CRDs. A CRD added after that successful
install is not installed by an ordinary deploy.

If the CRD phase fails during a first install, the Helm release was not
successfully established. After you fix the CRD, retry `deployah deploy`.
Treat that retry according to the remaining Helm release history, not as
an automatic upgrade.

If CRDs succeed but later ordinary install resources fail, those CRDs can
remain in the cluster. Deployah uses Helm 4.3 `RollbackOnFailure` on
install, so the failed install may be uninstalled and a retry can be a
fresh install. If release history remains, the next operation follows
that history. Re-run `deployah deploy` after you fix the failure.

See the [README](../README.md) for the project overview and the other guides.
