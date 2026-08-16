# Troubleshooting

Common failures and how to fix them: spec and deployment errors, custom
manifests, HTTPS timeouts, and local cluster networking.

## Spec and deployment

**Spec is missing a required field.**

```sh
error: load spec: ... spec is missing 'apiVersion' field
```

Your spec needs `apiVersion`, `project`, and `components`, and an `environments`
map. Run `deployah validate` to find the problem.

**Environment not found.**

```sh
error: environment "production" not found
```

Check the environment name in your spec, or run `deployah list` to see what is
deployed.

**Variable not found.**

```sh
error: variable ${IMAGE} not found
```

Define the variable in the environment's `variables`, or in your env file or
shell with the `DPY_VAR_` prefix.

**Hook task failed and was kept.**

A failed `preDeploy` or `postDeploy` Job is not deleted. Read it with
`deployah logs <project> --component=<task> --no-follow`, fix the command or
the database, then deploy again. Helm recreates the Job (`before-hook-creation`).

**Deploy timed out while hooks were still running.**

Hook timeout defaults to `5m` and must be less than the `--timeout` used for
that deploy (default `10m`). Increase `--timeout` so it stays above every hook
timeout. A spec may set a hook timeout longer than the default `10m`; deploy
then needs a matching `--timeout`. Deployah does not raise the flag for you.
Serial hooks can add up to more than `--timeout`; plan shows each hook timeout
so you can see the budget.

**A task did not run on deploy.**

`"on": manual` tasks only run via `deployah run`. Hook tasks skipped for this
environment have an `environments` filter that does not match.

**preDeploy cannot reach the database on first install.**

On a first install, `preDeploy` runs before Deployments and Services. The
database must already be reachable. See [Tasks](tasks.md#first-install-and-the-database).

**Cannot connect to Kubernetes.**

```sh
error: unable to connect to Kubernetes cluster
```

Check that your cluster is reachable with `kubectl cluster-info`. For a local
cluster, run `deployah cluster up` and deploy with the `local` environment (or
pass `--context kind-deployah`).

## Custom manifest and CRD errors

**Unknown type / add its CRD under `.deployah/crds/`.**

The kind is not a built-in type, not on the small operator allowlist, and not
declared by an in-repo CRD. Put the `CustomResourceDefinition` under
`.deployah/crds/`, or install that API on the cluster before deploying. See
[Custom manifests and CRDs](custom-manifests-and-crds.md).

**Extra collides with a generated object.**

An object in `.deployah/manifests/` has the same apiVersion, kind, namespace,
and name as something the chart already generates. Rename the extra, or stop
generating that resource from the spec.

**CRD must use `apiVersion: apiextensions.k8s.io/v1`.**

Older `apiextensions.k8s.io/v1beta1` CRDs are rejected. Convert the document to
v1.

## Deploy succeeds but the app returns 503 / times out over HTTPS

**Symptom.** `deployah deploy` completes, the pod is Running and 1/1, but
requests to the app fail:

```text
< HTTP/1.1 503 Service Unavailable
< server: envoy
upstream connect error or disconnect/reset before headers. reset reason: connection timeout
```

`kubectl port-forward svc/<project>-<env>-web 8080:80` works, which confirms
the pod is healthy and only the ingress path is broken.

**Cause.** The local cluster uses cloud-provider-kind to serve ingress. Its
Envoy gateway runs in a container and forwards traffic from the container
network into the cluster. When the host drops that forwarded traffic, Envoy
returns 503 -- even though the pod is fine. This is a host networking issue, not
a Deployah or app problem.

### Linux

The host's iptables `FORWARD` chain defaults to `DROP` (set by Docker, or
re-imposed by firewalld/ufw), which silently drops the gateway's traffic.

Confirm:

```sh
sudo iptables -S FORWARD | head -1
# -P FORWARD DROP   <-- this is the cause
```

Find the Kind bridge interface:

```sh
bridge=br-$(docker network inspect kind -f '{{.Id}}' | cut -c1-12)
```

Then apply one of these fixes:

**firewalld:**

```sh
sudo firewall-cmd --permanent --zone=trusted --change-interface="$bridge"
sudo firewall-cmd --reload
```

**iptables / nftables** (survives Docker restarts without opening the whole
host):

```sh
sudo iptables -I DOCKER-USER -o "$bridge" -j ACCEPT
sudo iptables -I DOCKER-USER -i "$bridge" -j ACCEPT
```

**ufw:** set `DEFAULT_FORWARD_POLICY="ACCEPT"` in `/etc/default/ufw`, then run
`sudo ufw reload`.

Re-run your request afterwards. Avoid `sudo iptables -P FORWARD ACCEPT` -- it
works but opens the entire host to forwarded traffic.

### macOS (Docker Desktop / OrbStack / Podman machine)

The Docker daemon runs inside a Linux VM, so there is no host firewall rule to
change. Instead:

- Always reach the app via `127.0.0.1`, never a `172.x` container address.
  Deployah publishes ingress on `127.0.0.1` by default.
- Recreate the cluster: `deployah cluster down && deployah cluster up`.
- Restart the VM: quit and reopen Docker Desktop, or `orb restart`, or
  `podman machine stop && podman machine start`.
- If the problem persists, see the
  [upstream issue](https://github.com/kubernetes-sigs/cloud-provider-kind/issues/142).

### Reach the app while you fix the above

```sh
kubectl --kubeconfig "$(deployah cluster kubeconfig)" \
  port-forward svc/<project>-<env>-web 8080:80
curl http://localhost:8080
```

## Local cluster networking problems

**Services return "Empty reply from server" on macOS (Lima).**

Lima's VZ driver uses a usernet port forwarder by default, which has a known
issue with the custom Docker network that Kind creates. To fix it, edit your
Lima config:

```sh
limactl stop <instance>
limactl edit <instance>
```

Make sure both settings are present at the top level:

```yaml
ssh:
  overVsock: false

portForwards:
  - guestIPMustBeZero: true
    guestPortRange: [1, 65535]
    hostIP: 127.0.0.1
  - guestSocket: "/var/run/docker.sock"
    hostSocket: "{{.Dir}}/sock/docker.sock"
```

Then restart:

```sh
limactl start <instance>
```

`ssh.overVsock: false` switches Lima to the standard SSH port forwarder. The
`portForwards` rule forwards all guest ports to the host, which is needed for
the dynamic Docker ports.

**"permission denied" in cloud-provider-kind logs.**

The cloud provider needs a rootful Docker daemon. If you use Lima, create a
rootful instance:

```sh
limactl start template:docker-rootful
```

**Firewall blocks gateway ports.**

Gateway ports are bound on all interfaces (`0.0.0.0`). On Linux, allow the
mapped ports in your firewall. On macOS, the Application Firewall may ask for
permission. Allow it when prompted.

## Getting help

Every command explains itself:

```sh
deployah --help
deployah <command> --help
```

If that does not answer it, ask in
[Q&A](https://github.com/deployah-dev/deployah/discussions/categories/q-a). If
Deployah behaves incorrectly, open an
[issue](https://github.com/deployah-dev/deployah/issues) with the command you
ran and the output of `deployah plan <environment>`. Report security problems
privately through [SECURITY.md](../SECURITY.md).

See the [README](../README.md) for the project overview and the other guides.
