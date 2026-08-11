# Workloads

How Deployah turns a component into a Kubernetes workload: stateful sets and
volumes, background workers, health checks, and Prometheus metrics.

## Stateful workloads

Use `kind: stateful` when a component needs **stable network identity**
(ordinal hostnames, headless DNS, ordered start/scale), and optionally a
**per-pod volume**: single-writer stores, disk-backed caches or queues, or
any process that must remount the same PVC after a restart.

Deployah models this as a Kubernetes StatefulSet, a ClusterIP Service, and a
headless Service (`...-headless`). When you set `persistence`, each replica
gets its own `volumeClaimTemplates` PVC. For highly available databases with
operator-managed failover, prefer a dedicated operator or managed service.
Deployah gives you a solid StatefulSet (and PVCs when requested); it does not
replace PostgresOperator, CloudNativePG, or similar.

Stateful components **with persistence** require **Kubernetes 1.32 or
newer**. Deployah checks the cluster API version before deploy and fails fast
on older clusters. That floor matches stable `ReadWriteOncePod` and PVC
retention support. Identity-only stateful components (no `persistence`) do
not require that floor.

### Persistence and replicas

`persistence` is optional. Omit it for identity-only StatefulSets. When set,
`size` and `mountPath` are required:

```yaml
apiVersion: v1-alpha.4
project: shop
components:
  # Identity only: stable DNS / ordinals, no PVC
  peer:
    kind: stateful
    image: ghcr.io/acme/peer:1.0.0
    port: 8080
    resourcePreset: nano
    environments: [dev]
    replicas: 3
  # Per-pod durable volume
  cache:
    kind: stateful
    image: redis:7-alpine
    port: 6379
    resourcePreset: nano
    environments: [dev]
    replicas: 1
    persistence:
      size: 20Gi
      mountPath: /data
      # storageClass: fast   # optional logical key; see Storage classes
environments:
  dev: {}
```

| Field | Required | Notes |
|---|---|---|
| `persistence.size` | When `persistence` is set | Kubernetes quantity (`1Gi`, `20Gi`, ...). |
| `persistence.mountPath` | When `persistence` is set | Absolute path inside the container. |
| `persistence.storageClass` | No | Logical key from the platform environment `storageClasses` map. Overrides the profile `storageClass` when set. |
| `replicas` | No | Desired StatefulSet replicas (default `1`). Cannot be set together with `autoscaling.enabled`. |

Autoscaling is allowed on stateful components when you omit `replicas`. Plan
and deploy warn that scale-down may leave PVCs behind when retention is
`Retain` (the default) and persistence is enabled.

You can also set `persistence` on `kind: stateless`. Deployah uses a shared
PVC and forces the Deployment strategy to `Recreate`. It rejects `replicas`
greater than `1` and rejects enabled HPA for that component.

### Storage class resolution

Order of precedence for the Kubernetes StorageClass name:

1. Component `persistence.storageClass` (logical key)
2. Merged platform profile `storageClass` (logical key)
3. Cluster default (empty `storageClassName`) when neither sets a key

If a logical key is set and the target environment has no `storageClasses`
map, or the key is missing from that map, resolve fails with a hard error.
See [Storage classes](platform.md#storage-classes) for the platform map shape.

### Access mode and PVC retention

Stateful volume claim templates default to `ReadWriteOncePod`. That access
mode keeps a single pod attached to each volume, which matches StatefulSet
identity. If your StorageClass or CSI driver cannot provide RWOP, the PVC
stays Pending and the StatefulSet will not become ready.

Chart defaults keep volumes when the StatefulSet is deleted or scaled down
(`Retain` / `Retain`). A platform profile may override with
`pvcRetentionPolicy`:

```yaml
profiles:
  ephemeral-data:
    pvcRetentionPolicy:
      whenDeleted: Delete
      whenScaled: Delete
```

Values are `Retain` or `Delete` for each of `whenDeleted` and `whenScaled`.

### Services and Ingress

Each stateful service component gets:

- A normal ClusterIP Service named like the component release
  (`{{ project }}-{{ env }}-{{ component }}`)
- A headless Service named `...-headless` (`clusterIP: None`) for stable
  DNS (`pod-0.{{ headless }}...`)

Exposing a multi-replica stateful component through Ingress still points at
the ClusterIP Service. Clients that need sticky per-pod routing should use
headless DNS or an application-aware proxy; a single Ingress backend does not
fan out to a specific ordinal.

### Growing volumes

Decreasing `persistence.size` is rejected. Increasing size needs an explicit
`--resize-volumes` opt-in because Deployah must expand live PVCs. For
stateful components it also orphan-deletes the StatefulSet controller so Helm
can re-apply `volumeClaimTemplates`. Stateless shared PVCs are patched in
place (no orphan-delete).

1. Confirm the StorageClass has `allowVolumeExpansion: true`.
1. Raise `persistence.size` in `deployah.yaml` (for example `20Gi` to `40Gi`).
1. Deploy with the flag:

   ```sh
   deployah deploy <environment> --resize-volumes --yes
   ```

1. Deployah then:
   - Patches each matching PVC `spec.resources.requests.storage`
   - Waits for expansion to progress
   - For stateful: orphan-deletes the StatefulSet (pods and PVCs keep running)
   - Runs the normal Helm upgrade

If resize fails after an orphan-delete, pods and PVCs should still be
running. Fix the cause (expansion support, permissions, quota) and re-run
`deployah deploy ... --resize-volumes`. Without the flag, a size increase
stops with an error that tells you to pass `--resize-volumes`.

### Changing a component's kind, and other guards

Deployah rejects changing a component between `stateless` and `stateful` on
an existing release (delete and redeploy instead). It also rejects adding or
removing `persistence` on an existing StatefulSet (`volumeClaimTemplates` are
immutable). It warns when you combine stateful + persistence with HPA,
Ingress with replicas > 1, or a changed `mountPath` after the first deploy.

## Worker components

A `role: worker` component is a long-running process that does not serve
inbound traffic. Deployah still runs it as a Deployment (`kind: stateless`)
or StatefulSet (`kind: stateful`), with HPA and persistence available the
same way as for services.

Workers must not set:

- `port` (use `metrics.port` when you need a scrape endpoint)
- `expose` (no Ingress)
- `health.ready` (there is no traffic gate)
- `health.alive.path` (HTTP probes need an app port)

By default, Kubernetes restarts a worker when the process exits. Optional
`health.alive.exec` adds a command-based liveness probe:

```yaml
components:
  worker:
    role: worker
    image: ghcr.io/acme/worker:1.0.0
    environments: [production]
    resourcePreset: small
    shutdownTimeout: 60s
    health:
      alive:
        exec: ["/bin/grpc_health_probe", "-addr=:50051"]
```

Stateful workers get a headless Service with a synthetic `identity` port
(`9`) so peer DNS and stable network identity work without an app listen
port. Stateless workers with no metrics get no Service.

`shutdownTimeout` defaults to `60s` for workers (vs `30s` for services) and
maps to `terminationGracePeriodSeconds`. Give workers enough time to finish
in-flight work before the kubelet sends `SIGKILL`.

Changing a component's `role` between `service` and `worker` on an existing
release is not supported. Delete the release and redeploy.

## Health checks

Deployah checks that your app is running and ready for traffic. For every
`service` component with a `port`, Deployah adds three checks automatically:

- **Startup check.** Waits up to 3 minutes for your app to accept connections
  on its port. New pods do not receive traffic until this passes. If the app
  takes longer than 3 minutes to start, the pod is killed and restarted.
- **Ready check.** Runs every 5 seconds. If your app stops accepting
  connections for 15 seconds, traffic is routed to other pods until it recovers.
- **Alive check.** Runs every 10 seconds. If your app is unresponsive for 60
  seconds, the pod is restarted.

With no configuration, all three checks connect to your app's port (TCP). This
works for any app. You can make the checks smarter by giving Deployah an HTTP
endpoint to call.

**Zero config.** All checks run automatically. No `health` block needed.

```yaml
components:
  api:
    image: my-app:1.0.0
    port: 8080
```

**Add a readiness endpoint.** Tell Deployah where to check if your app is ready
for traffic. This also upgrades the startup check to the same endpoint.

```yaml
components:
  api:
    image: my-app:1.0.0
    port: 8080
    health:
      ready:
        path: /health
```

Your `/health` endpoint should return a `2xx` status code when your app can
handle requests. Return `4xx` or `5xx` when it cannot, for example if it is
still connecting to the database.

**Add a separate restart endpoint.** If your app can get stuck in a way that a
restart fixes, give Deployah a separate endpoint to check. If this endpoint
fails for long enough, the pod is restarted.

```yaml
components:
  api:
    image: my-app:1.0.0
    port: 8080
    health:
      ready:
        path: /health
      alive:
        path: /livez
        interval: 10s      # how often to check (default: 10s)
        restartAfter: 60s  # how long to fail before restart (default: 60s)
```

Your `/livez` endpoint should check only whether the process itself is
responsive. Do not check external dependencies (databases, caches) here. If a
dependency is down, let the ready endpoint return an error instead. That stops
traffic without restarting the pod.

**Disable checks.** For a raw TCP service or an app where checks cause
problems, you can disable them individually.

```yaml
components:
  game-server:
    image: my-game:1.0.0
    port: 9000
    health:
      ready: false
      alive: false
```

**Exec alive probe.** Use a command instead of HTTP when your process has
no listen port, or when an in-container check is a better signal. `path` and
`exec` are mutually exclusive. Services may use either; workers may use
`exec` only (see [Worker components](#worker-components)).

```yaml
components:
  api:
    image: my-app:1.0.0
    port: 8080
    health:
      alive:
        exec: ["/bin/grpc_health_probe", "-addr=:8080"]
        interval: 10s
        restartAfter: 60s
```

## Metrics

Deployah can emit Prometheus Operator scrape configs when a component
enables `metrics`. Services produce a `ServiceMonitor`; workers produce a
`PodMonitor`. Your cluster must have the Prometheus Operator CRDs
(`monitoring.coreos.com/v1`). `deployah plan` and `deployah deploy` check
for that API group when metrics are enabled.

Shape:

| Form | Meaning |
|---|---|
| `metrics: true` | Enable scraping. Services scrape the app port at `/metrics`. Workers must set `port` instead. |
| `metrics: false` | Explicitly off. |
| `metrics: { ... }` | Object form with `enabled?`, `port`, `path`, `interval?`, `scrapeTimeout?`. |

Defaults when enabled: `path` is `/metrics`. For services, omitted `port`
uses the component port (ServiceMonitor endpoint port name `http`). A
dedicated metrics port adds a container and Service port named `metrics`.
Workers always require `metrics.port` and scrape via PodMonitor port name
`metrics`.

Platform profiles own discovery labels and scrape policy under
`metrics:`. When component metrics are enabled, the merged profile must set
`metrics.monitorLabels` (for example `release: prometheus` for the
kube-prometheus-stack selector). Other profile metrics fields:
`monitorNamespace`, `interval`, `scrapeTimeout`, `jobLabel`, `honorLabels`,
`annotations`, `relabelings`, `metricRelabelings`.

```yaml
# deployah.yaml
components:
  api:
    image: ghcr.io/acme/api:1.0.0
    port: 8080
    environments: [production]
    profiles: [observability]
    metrics: true
  worker:
    role: worker
    image: ghcr.io/acme/worker:1.0.0
    environments: [production]
    profiles: [observability]
    metrics:
      port: 9090
      path: /metrics
```

```yaml
# deployah.platform.yaml
apiVersion: platform/v1-alpha.3
profiles:
  observability:
    metrics:
      monitorLabels:
        release: prometheus
      interval: 30s
environments:
  production:
    context: prod
```

See the [README](../README.md) for the project overview and the other guides.
