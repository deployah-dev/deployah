# Deployah vs. Similar Tools

Deployah is not the only way to deploy applications to Kubernetes without writing
Helm charts or Kubernetes YAML. This page is an honest, deep comparison with five
**live** tools: **DevSpace**, **Werf**, **Score**, **Epinio**, and **Kubero**.

The most important question for Deployah is simple:

> **How much Helm and Kubernetes knowledge do you need?**

Deployah's goal is **none** — and with **nothing to install** in your cluster.

## The Helm-knowledge lens

There are really two questions:

1. Does the **developer** need to know Helm?
2. Does **someone else** (a platform team or an admin) still need Helm/Kubernetes
   knowledge to set the tool up?

| Tool | Developer needs Helm? | Setup needs a platform/admin? | You still get a Helm release? |
|---|---|---|---|
| **Deployah** | **No** | **No** — stateless CLI, nothing to install | **Yes** |
| **DevSpace** | **Some** — you write Helm *values* | No — stateless CLI | Yes |
| **Werf** | **Yes** — you write the whole chart | No — stateless CLI | Yes |
| **Score** | No | Yes — a platform team builds the apply pipeline | No — it makes raw YAML |
| **Epinio** | No | Yes — an operator installs a server, registry, storage, UI | Yes (hidden) |
| **Kubero** | No | Yes — an admin runs an operator + web UI | Add-ons only |

**The key point:** Deployah is the only tool here where the developer needs **no
Helm knowledge**, *and* there is **no platform to install**, *and* you still get a
real Helm release. The other "no Helm for the developer" tools do not remove the
Helm/Kubernetes work — they **move it** to a platform team or to a server that runs
inside your cluster.

> **Honest note:** you do not need Helm to *use* Deployah. But because the output
> is a real Helm release, some Helm knowledge helps if you want to debug deeply —
> for example, running `helm history` or `helm get` on what Deployah installed.

## The Helm-knowledge spectrum

```text
MORE Helm knowledge needed  <───────────────────────────────>  LESS

  Werf            DevSpace          Score / Epinio / Kubero        Deployah
  (you write      (you write        (no Helm for the dev,          (no Helm,
   the chart)      Helm values)      but a platform/admin pays)     nothing to install)
```

## Who creates the Helm chart?

This is the heart of the difference. A tool can hide Helm in very different ways.

| Tool | Who creates the chart? | What the developer writes |
|---|---|---|
| **Deployah** | Deployah (a hidden umbrella chart) | A compact spec (`deployah.yaml`) |
| **DevSpace** | DevSpace (a built-in "component chart") | Helm **values** in `devspace.yaml` |
| **Werf** | **You** (the developer) | A full chart: `Chart.yaml`, `values.yaml`, templates |
| **Score** | No chart — it generates raw YAML | A compact spec (`score.yaml`) |
| **Epinio** | Epinio (a generic app chart) | Nothing — you push source code |
| **Kubero** | No app chart — an operator builds the objects | Web UI, a CRD, or a Git repo |

Deployah and DevSpace both **own a hidden chart**. The difference is the input:
DevSpace asks for **Helm values** (closer to Helm); Deployah asks for a
**higher-level spec** (further from Helm). Werf is the opposite end — **you** write
the chart yourself.

## Full comparison

| | **Deployah** | **DevSpace** | **Werf** | **Score** | **Epinio** | **Kubero** |
|---|---|---|---|---|---|---|
| **What it is** | Stateless deploy CLI | Stateless dev + deploy CLI | Stateless build + deploy CLI (GitOps/CI) | Spec + generator CLI | In-cluster PaaS server | Self-hosted PaaS (operator + UI) |
| **Helm knowledge (developer)** | **None** | Some (values) | **Full** (writes charts) | None | None | None |
| **Who writes the chart** | Deployah (hidden) | DevSpace (component chart) | **You** | No chart (raw YAML) | Epinio (generic chart) | No app chart (operator) |
| **Footprint / setup** | **Nothing** | Nothing | Nothing | Nothing, but needs a platform pipeline | Server, registry, storage, UI | Operator + UI + CRDs |
| **Builds your image?** | No (BYO) | Yes (+ dev loop) | Yes (Dockerfile/Stapel) | No (BYO) | Yes (buildpacks) | Yes (buildpacks) |
| **Output** | **Helm release** | Helm release | Helm release (via Nelm) | Raw YAML (no install) | Helm release (hidden) | K8s objects via operator |
| **Multi-component** (service/worker/job, stateless/stateful) | **Yes, named** | Partial | No (you template each) | No (1 workload/file) | No (web apps) | web/worker/cron + DB add-ons |
| **Multiple environments** | **Yes** (own context, config, env, vars) | Partial (profiles + vars) | Yes (env string; you template diffs) | No (platform decides) | Namespaces only | Pipelines (max 4 stages) |
| **Installs + day-2** | **Yes** | Yes (+ dev mode) | Yes (converge/plan/dismiss/status/logs) | No (you run `kubectl apply`) | Yes (+ UI) | Yes (+ UI) |
| **Maturity (mid-2026)** | Early, independent | Mature, CNCF, ~4.9k★ | Mature, CNCF, Flant, ~4.7k★ | Mature spec, CNCF | Active, ~585★ | Active, ~4.3k★ |

## Tool by tool

### DevSpace — the closest in shape

**What it is:** a stateless Go CLI for development *and* deployment (CNCF Sandbox,
by Loft Labs). **Chart:** DevSpace provides a built-in generic "component chart",
so you do not write templates — but you write its Helm **values** (containers,
service, ports, volumes), and you can also point it at your own chart, Kustomize,
or raw manifests. **Build:** yes — building images and a live dev loop (file sync,
port-forward, hot reload) is its main job. **Output:** a real Helm release.
**Environments:** profiles + variables, not first-class named environments.
**vs Deployah:** same shape (stateless CLI + hidden chart → Helm release), but
DevSpace is a *dev-loop* tool that builds images, and you write Helm **values**
(some Helm knowledge). Deployah writes a higher-level spec, needs no Helm, and does
not build.

### Werf — same shape, but you write the chart

**What it is:** a stateless Go CLI for full CI/CD — build and deploy, with Git as
the source of truth (CNCF Sandbox, by Flant). **Chart:** **you write it** —
`werf.yaml`, `Chart.yaml`, `values.yaml`, and the Kubernetes manifests as Helm
templates; Werf only injects the built image references. **Build:** yes — builds
images (Dockerfile/Stapel via Buildah), tied to deploy. **Output:** a Helm release
through its Helm-compatible engine, Nelm. **Environments:** an env *string*; you
template the per-environment differences yourself and manage kube-context
yourself. **vs Deployah:** same stateless shape and Helm-release output, but Werf
needs **full Helm and Kubernetes knowledge** (you author the chart) and builds
images. Deployah hides the chart and needs no Helm. This is the clearest contrast
on the Helm-knowledge lens.

### Score — a compact spec, but it only generates

**What it is:** a specification plus generator CLIs (`score-k8s`,
`score-compose`); CNCF Sandbox, started by Humanitec. **Chart:** none —
`score-k8s` generates raw Kubernetes YAML (`score-helm` is deprecated).
**Output:** a `manifests.yaml` that you apply yourself with `kubectl` — it does
**not** install. **Environments:** left out of the file by design; the platform
decides. **Multi-component:** one workload per file, but it has a rich
resources/dependencies model (ask for a Postgres and the platform creates it).
**Day-2:** none (no status/logs/delete). **vs Deployah:** closest on **input** (a
small spec), but Score **generates and stops** while Deployah **installs and
operates**. Score needs a platform team to be useful end-to-end; Deployah works on
its own.

### Epinio — same idea, but a full in-cluster platform

**What it is:** a PaaS you install into the cluster — server, container registry,
object storage, and a web UI (Apache-2.0, now maintained by Krumware after SUSE).
**Chart:** Epinio uses a generic app chart (custom charts can be registered).
**Build:** yes (Paketo buildpacks); it can also take a pre-built image.
**Output:** a Helm release per app (hidden). **Environments:** namespaces only, on
one cluster. **Multi-component:** web apps (every app gets a URL); no native
worker/job/stateful. **vs Deployah:** same hidden-Helm idea, but Epinio is a heavy
**platform** with a server to operate and builds from source. Deployah installs
nothing, is bring-your-own-image, and models components and per-cluster
environments.

### Kubero — a Heroku-style PaaS

**What it is:** a self-hosted PaaS — an operator + web UI + its own state (CRDs)
in the cluster (MIT). **Chart:** no app chart; the operator builds the Kubernetes
objects (Helm is used for add-ons only). **Build:** yes
(buildpacks/Nixpacks/Dockerfile) plus bring-your-own-image. **Environments:**
Heroku-style pipelines (up to 4 stages) with PR/preview apps. **Multi-component:**
web/worker/cron, plus one-click database add-ons. **Day-2:** UI dashboards, logs,
metrics, scaling, web console, notifications. **vs Deployah:** both hide
Kubernetes from the developer, but Kubero is a full platform with a server, UI,
and Git automation. Deployah is a light, stateless CLI with no server and no UI.

## When to choose Deployah

Choose **Deployah** if you want:

- To deploy with **no Helm or Kubernetes knowledge** and **nothing to install**.
- A **compact spec** for a project with **many components** (service/worker/job)
  across **many environments** (each with its own cluster/context).
- A real **Helm release** as the result, friendly to GitOps and `helm` tooling.
- You already **build your images in CI** and just want to ship them.

Choose another tool if you want:

- **DevSpace** — a live development loop and image building.
- **Werf** — one tool to own the whole pipeline (build + deploy) and full control
  over the chart, if you already know Helm and Kubernetes.
- **Score** — one spec for many platforms (Kubernetes *and* Docker Compose), run
  by a platform team.
- **Epinio** — a source-to-URL PaaS with a web UI.
- **Kubero** — a full Heroku-like platform with a UI, Git push, and databases.

## Where Deployah is weaker (honest)

- It is **younger** and has **no big company or CNCF backing** yet (DevSpace, Werf,
  Score all have CNCF/company support and far more stars).
- It does **not** build images, has **no** dev loop, **no** web UI, **no**
  database add-ons, and **no** Git/PR preview apps.
- It has **no resource/dependency model** (Score's strength: "ask for a Postgres,
  the platform creates it").
- Werf and DevSpace give **more control and customization** for users who *do*
  know Helm.

## Sources

- DevSpace — <https://www.devspace.sh/>, <https://github.com/devspace-sh/devspace>
- Werf — <https://werf.io/>, <https://github.com/werf/werf>, <https://github.com/werf/nelm>
- Score — <https://score.dev/>, <https://github.com/score-spec/spec>
- Epinio — <https://epinio.io/>, <https://github.com/epinio/epinio>
- Kubero — <https://www.kubero.dev/>, <https://github.com/kubero-dev/kubero>
