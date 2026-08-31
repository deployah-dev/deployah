# YAML you own

Four ways to ship the same app: nginx 1.26, port 80, one replica, public
HTTPS at `web.example.com`, TLS from cert-manager (`letsencrypt-prod`).

This directory is the example behind the line counts on
[deployah.dev](https://deployah.dev/). It is not a runnable tutorial. For that,
use [examples/nginx](../nginx).

The Kubernetes, Helm, and Kustomize trees match what Deployah applies for
this spec: probes, soft anti-affinity, `revisionHistoryLimit`, small-preset
requests, TLS as a cert-manager Ingress annotation, and the labels and
annotations Deployah sets. Replica count is the schema default (1).

## What each tree is

| Tree | What you keep | Non-blank lines |
| --- | --- | ---: |
| [deployah/](deployah/) | Spec and platform file | 17 |
| [kubernetes/](kubernetes/) | Deployment, Service, Ingress | 135 |
| [kustomize/](kustomize/) | Base workloads plus a production overlay | 145 |
| [helm/](helm/) | Chart, values, and templates | 340 |

A ClusterIssuer named `letsencrypt-prod` is assumed to already exist. That is
platform machinery, not app YAML, so it is not in these trees.

## How lines are counted

Non-blank lines in `*.yaml`, `*.yml`, and `*.tpl`. Comments count. `README.md`
does not.

```sh
./count.sh
```

If you change a tree, run that script and update this table and the landing
page together.

## Keep them in lockstep

The four trees describe the same image, port, host, issuer, and replica
count. If one side gains a probe, resource request, or DNS name, change
the other three in the same PR.
