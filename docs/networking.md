# Networking

How to reach your app after a deploy, and how traffic gets from `localhost` into
the local cluster. For certificates, see
[TLS modes](platform.md#tls-modes).

## Accessing your app

To reach a component over HTTP or HTTPS, give it `expose: true`. Its hostname
comes from the component name plus the platform file's domain:

```yaml
# deployah.yaml
components:
  web:
    image: nginx:latest
    port: 80
    environments: [local]
    expose: true
```

```yaml
# deployah.platform.yaml (created for you by 'deployah cluster up')
environments:
  local:
    context: kind-deployah
    domains:
      public:
        baseDomain: 127.0.0.1.nip.io
        tls:
          mode: selfSigned
```

On the local cluster, run `deployah cluster status` to see the resolved URL
and port for your app. Open that URL in your browser; nip.io resolves to
`127.0.0.1` for you, so you do not need extra setup or `/etc/hosts` entries.

## Local cluster networking

The local cluster runs [Kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker)
with [cloud-provider-kind](https://github.com/kubernetes-sigs/cloud-provider-kind)
for LoadBalancer, Ingress, and Gateway API support.

On Linux and macOS, services are reachable on `localhost` through Docker port
mapping. The path traffic takes is:

```text
localhost:<port>
  -> Docker port mapping
    -> Envoy gateway container
      -> Kind cluster pod
```

On macOS and other Docker-in-VM setups (Lima, Colima, Docker Desktop, OrbStack),
there is one more layer. Your Docker runtime forwards the VM port to the host
automatically, so you do not configure anything:

```text
macOS localhost:<port>
  -> VM port forwarding (automatic)
    -> Docker port mapping
      -> Envoy gateway container
        -> Kind cluster pod
```

> [!NOTE]
> LoadBalancer, Ingress, and Gateway API need a rootful Docker daemon.
> Rootless Docker cannot mount the Docker socket into the `cloud-provider-kind`
> container, so it cannot manage LoadBalancer resources.

Run `deployah cluster status` at any time to see the assigned ports and URLs for
all Ingress and LoadBalancer resources.

See the [README](../README.md) for the project overview and the other guides.
