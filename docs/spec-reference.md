# Spec reference

Your spec is a file named `deployah.yaml`. It has three required parts:
`apiVersion`, `project`, and `components`. Optional `tasks` hold
run-to-completion work. This page is the full field reference, the value
rules the validator enforces, the resource presets, and a set of complete
examples.

`deployah.yaml` is the developer's half of the configuration: what to run, never
where it runs. The other half is
[`deployah.platform.yaml`](platform.md), owned by the platform team. The
[README](../README.md#who-this-is-for) describes that boundary.

Here is a full example that shows the common fields. You do not need all of
them; most have defaults.

```yaml
apiVersion: v1-alpha.5             # required: the schema version
project: shop                      # required: your project name

components:                        # required: one or more components
  api:
    image: ghcr.io/acme/shop-api:${TAG}  # tag comes from the environment below
    role: service                  # service | worker (default: service)
    kind: stateless                 # stateless | stateful (default: stateless)
    port: 8080                     # the port your app listens on (default: 8080)
    environments: [staging, prod]  # which environments deploy this component
    command: ["/bin/api"]          # optional: override the image ENTRYPOINT
    args: ["--verbose"]            # optional: override the image CMD
    env:                           # planned: not applied to the container yet
      LOG_LEVEL: info
    resourcePreset: small          # nano|micro|small|medium|large|xlarge|2xlarge
    shutdownTimeout: 30s           # how long Kubernetes waits for graceful stop
    metrics: true                  # scrape /metrics on the app port (needs profile metrics.monitorLabels)
    expose:                        # optional: `expose: true` uses all defaults
      subdomain: api                # optional: defaults to the component name
      # domain: internal            # optional: defaults to the platform's default domain
      # apex: true                  # optional: use the bare domain instead of a subdomain
    autoscaling:                   # optional: scale on CPU or memory
      enabled: true
      minReplicas: 2
      maxReplicas: 5
      metrics:
        - type: cpu                # cpu | memory
          target: 70               # target usage percentage
  worker:
    role: worker                   # no port, expose, or Ingress
    image: ghcr.io/acme/shop-worker:${TAG}
    environments: [staging, prod]
    command: ["/bin/worker"]
    resourcePreset: small
    shutdownTimeout: 60s           # workers default to 60s
    metrics:                       # workers need an explicit scrape port
      port: 9090
      path: /metrics
    health:
      alive:
        exec: ["/bin/grpc_health_probe", "-addr=:50051"]

environments:                      # define your environments (a map, not a list)
  staging:
    variables:
      TAG: 1.4.0-rc                # fills ${TAG} in the image above
  prod:
    variables:
      TAG: 1.4.0                   # fills ${TAG} in the image above
```

Notice there is no `context` field here: the Kubernetes context for each
environment comes from `deployah.platform.yaml`, not from `deployah.yaml`.

Use either `resourcePreset` or `resources`, not both. Presets are the easy
option; `resources` lets you set exact CPU, memory, and ephemeral storage.

## Field reference

Top level:

| Field | Required | Notes |
|---|---|---|
| `apiVersion` | Yes | The schema version. Must be `v1-alpha.5`. |
| `project` | Yes | Lowercase name (DNS-1123). Prefixes your Kubernetes resources. |
| `components` | Yes | A map of component name to component settings. |
| `tasks` | No | A map of task name to run-to-completion work. Names must not collide with component names. See [Tasks](#tasks). |
| `environments` | Yes in practice | Environment **overrides**: a map of environment name to per-environment settings (`variables`, `envFile`). Keys support prefix-based wildcard matching, e.g. a `review` key matches `--environment review/pr-123`. Which environments exist is owned by the platform file's registry. |

Component:

| Field | Default | Notes |
|---|---|---|
| `image` | none | The container image to run. You provide this. |
| `role` | `service` | `service` or `worker`. |
| `kind` | `stateless` | `stateless` or `stateful`. |
| `port` | `8080` (services) | App listen port (1 to 65535). Not allowed on workers. |
| `command` / `args` | none | Override the image ENTRYPOINT and CMD. |
| `env` | none | Environment variables (uppercase keys). |
| `resourcePreset` | none | `nano`, `micro`, `small`, `medium`, `large`, `xlarge`, `2xlarge`. |
| `resources` | none | `cpu`, `memory`, `ephemeralStorage` (Kubernetes units). |
| `expose` | none | Services only. `true` for all defaults, or an object with `domain`, `subdomain`, and `apex`. See [Platform file](platform.md). |
| `replicas` | `1` (chart) | Desired pod count. Cannot combine with `autoscaling.enabled`. |
| `persistence` | none | Optional for `kind: stateful` (`size`, `mountPath`, optional logical `storageClass`). Omit for identity-only. Allowed on stateless (shared PVC, Recreate). See [Stateful workloads](workloads.md#stateful-workloads). |
| `autoscaling` | off | `enabled`, `minReplicas`, `maxReplicas`, `metrics`. |
| `shutdownTimeout` | `30s` (service), `60s` (worker) | Graceful stop window; maps to `terminationGracePeriodSeconds`. |
| `metrics` | off | `true`, `false`, or `{enabled?, port, path, interval?, scrapeTimeout?}`. See [Metrics](workloads.md#metrics). |
| `health` | auto | Ready and alive checks. See [Health checks](workloads.md#health-checks). |
| `environments` | none | Environment **filter**: which environments deploy this component. Omit it to deploy the component everywhere. |
| `profiles` | none | List of platform profile names. Merged left to right. See [Profiles](platform.md#profiles). |

> [!IMPORTANT]
> Component `env`, `envFile`, and `configFile` are not applied to Deployments
> yet. Task `env` **is** inlined onto the Job. Changing `role` between
> `service` and `worker` on an existing release is rejected; delete the
> release and redeploy.

## Tasks

Quote `"on"` in YAML 1.1 so parsers do not treat it as a boolean. `on` is a
single value, not a list. See [Tasks](tasks.md) for how-to examples.

| Field | Default | Notes |
|---|---|---|
| `from` | none | Component to inherit env, environments, profiles, and resources from. Also copies envFile and configFile paths. |
| `image` | from `from` | Replaces the parent image when set. `from` and/or `image` is required. |
| `command` / `args` | none | `command` is required when using the parent image. |
| `"on"` | none (required) | `preDeploy`, `postDeploy`, or `manual`. |
| `after` | none | Task names in the **same** `on` that must finish first. The dependency must be active in every environment the dependent is. Not allowed on `manual`. |
| `env` | inherited | Overlay on the parent map. Inlined onto the Job. |
| `envFile` / `configFile` | inherited | Inherited as fields; not mounted in this release. |
| `environments` | inherited | Replaces the parent filter when set. |
| `profiles` | inherited | Replaces the parent list when set. Applied to the Job pod (node selector, tolerations, security context). |
| `resourcePreset` / `resources` | inherited | Same rules as components. |
| `fanout` | count 1, parallelism 1 | Integer (`fanout: 4`) or `{count, parallelism}`. Applies to every `on`. `parallelism` must be `<= count` and at most 100000 (Kubernetes Indexed Job limit). |
| `timeout` | `5m` for hooks | Duration such as `5m`. Hook timeout must be less than the session `--timeout` at deploy or run time (default `10m`). Raise `--timeout` for a longer hook. No default for `manual`. |
| `backoffLimit` | `3` | Retries before the run is marked failed. |
| `ttlSecondsAfterFinished` | none (CLI runs: 7 days) | Seconds to keep a finished run. |

`deployah run <task> <environment>` creates a Job for any task. It runs only
that task, not the tasks in its `after` list. Hook and CLI Jobs use the
default ServiceAccount in the release namespace. An exported chart overrides
a task the same way as a component (`--set migrate.image.tag=1.2.4`).

Environment:

| Field | Notes |
|---|---|
| `envFile` / `configFile` | Files to load for this environment (see below). |
| `variables` | Values for `${...}` placeholders in your spec. |

There is no `context` field on an environment: it comes from the matching
environment key in `deployah.platform.yaml`.

To check your spec, run `deployah validate`; when a platform file exists it
also cross-checks `expose.domain` keys and environment names against it. To
check the full resolution for a given environment, run
`deployah validate <environment>`.

## Value rules

A few fields have specific formats:

- **`port`**: a number from 1 to 65535.
- **`resources.cpu`**: millicores like `500m`, or whole cores like `1` or `2`.
- **`resources.memory`** and **`resources.ephemeralStorage`**: a number with a
  unit, like `256Mi` or `1Gi`.
- **`env`**: keys are uppercase letters, digits, and underscores, and start with
  a letter or underscore (for example `LOG_LEVEL`). Values are a string, number,
  or boolean.
- **`expose`**: `true`, `false`, or an object. `true` means all defaults.
- **`expose.domain`**: a key that must exist in the target environment's
  `domains` map in the platform file. Omit it to use the environment's only
  domain, or the one marked `default: true` there.
- **`expose.subdomain`**: a DNS-1123 label, like `api` or `www`. Omit it to
  use the component name. Cannot be combined with `apex`.
- **`expose.apex`**: set `true` to expose the component at the bare domain
  (e.g. `example.com`) instead of a subdomain.
- **`profiles`**: a list of non-empty strings naming entries in the platform
  file's root-level `profiles` map. Multiple names merge left to right.
  Omit the field to pick up the platform `default` profile when one exists.
  An empty list (`profiles: []`) opts out of every profile, but is rejected
  when a `default` profile is defined.
- **`autoscaling`**: needs `enabled`, `minReplicas`, and `maxReplicas`. Each
  metric has a `type` (`cpu` or `memory`) and a `target` percentage.
- **`health.alive.interval`** and **`health.alive.restartAfter`**: a positive integer
  followed by a unit: `s` (seconds), `m` (minutes), or `h` (hours). For example
  `10s`, `2m`, `1h`. The effective restart time rounds up to the nearest multiple
  of `interval`.
- **Names** (`project`, component names, environment names): lowercase
  letters, digits, and dashes (`-`), and cannot start or end with a dash.
  `project` must be at least 3 characters; component and environment names
  must be at least 2.

## Resource presets

A preset sets CPU and memory for you, so you do not need to know Kubernetes
units. Use `resourcePreset: <name>` on a component instead of writing `resources`.
These are the current values (request / limit):

| Preset | CPU (request / limit) | Memory (request / limit) |
|---|---|---|
| `nano` | 100m / 150m | 128Mi / 192Mi |
| `micro` | 250m / 375m | 256Mi / 384Mi |
| `small` | 500m / 750m | 512Mi / 768Mi |
| `medium` | 500m / 750m | 1024Mi / 1536Mi |
| `large` | 1000m / 1500m | 2048Mi / 3072Mi |
| `xlarge` | 1000m / 3000m | 3072Mi / 6144Mi |
| `2xlarge` | 1000m / 6000m | 3072Mi / 12288Mi |

All presets use the same ephemeral storage: 50Mi request, 2Gi limit.

> [!NOTE]
> Only the request values are applied to the container today. The limit
> values above are defined for future use but are not yet set on the
> Kubernetes resource spec, for presets or for manual `resources`.

## Spec examples

Every example below is complete and valid. Copy one and change the values.

**Smallest spec.** One service, one environment.

```yaml
apiVersion: v1-alpha.5
project: hello
components:
  web:
    image: nginx:latest
    environments: [dev]
environments:
  dev: {}
```

**Two components.** A web app and an API in one project.

```yaml
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: ghcr.io/acme/web:1.0.0
    port: 80
    environments: [prod]
  api:
    image: ghcr.io/acme/api:1.0.0
    port: 8080
    environments: [prod]
environments:
  prod: {}
```

**Several environments.** Each one has its own image tag. The cluster comes
from the platform file, not from here.

```yaml
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: ghcr.io/acme/web:${TAG}
    port: 80
    environments: [staging, prod]
environments:
  staging:
    variables:
      TAG: 1.0.0-rc
  prod:
    variables:
      TAG: 1.0.0
```

**Expose it over HTTPS.** `expose: true` is all you need: the hostname is
`web.<baseDomain>` (the component name plus the platform's domain), and TLS
comes from the platform file. Set `subdomain` only when you want a different
label, and `apex: true` for the bare domain.

```yaml
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: ghcr.io/acme/web:1.0.0
    port: 80
    environments: [prod]
    expose: true
```

**Set exact resources.** Use `resources` instead of a preset.

```yaml
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: ghcr.io/acme/web:1.0.0
    port: 80
    environments: [prod]
    resources:
      cpu: 500m
      memory: 512Mi
environments:
  prod: {}
```

**Autoscale on CPU.** Scale between 2 and 6 replicas at 70% CPU.

```yaml
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: ghcr.io/acme/web:1.0.0
    port: 80
    environments: [prod]
    autoscaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 6
      metrics:
        - type: cpu
          target: 70
environments:
  prod: {}
```

See the [README](../README.md) for the project overview and the other guides.
