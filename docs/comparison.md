# Deployah vs. Similar Tools

Deployah is not the only way to deploy apps to Kubernetes without writing Helm
charts or Kubernetes YAML. This page is an honest comparison with five **live**
tools: **DevSpace**, **Werf**, **Score**, **Epinio**, and **Kubero**.

For Deployah, one question matters most:

> **How much Helm and Kubernetes knowledge do you need?**

Deployah's goal is **none**, with **nothing to install** in your cluster.

## The Helm-knowledge question

There are really two parts to this:

1. Does the **developer** need to know Helm?
2. Does **someone else** (a platform team or an admin) still need Helm and
   Kubernetes knowledge to set the tool up?

| Tool | Developer needs Helm? | Setup needs a platform/admin? | You still get a Helm release? |
|---|---|---|---|
| **Deployah** | **No** | **No** (stateless CLI, nothing to install) | **Yes** |
| **DevSpace** | **Some** (you write Helm *values*) | No (stateless CLI) | Yes |
| **Werf** | **Yes** (you write the whole chart) | No (stateless CLI) | Yes |
| **Score** | No | Yes (a platform team builds the apply pipeline) | No (it makes raw YAML) |
| **Epinio** | No | Yes (an operator installs a server, registry, storage, UI) | Yes (hidden) |
| **Kubero** | No | Yes (an admin runs an operator and a web UI) | Add-ons only |

The key point: Deployah is the only tool here where the developer needs **no Helm
knowledge**, there is **no platform to install**, and you still get a real Helm
release. The other "no Helm for the developer" tools do not remove the Helm and
Kubernetes work. They **move it** to a platform team or to a server that runs
inside your cluster.

> **An honest note:** you do not need Helm to *use* Deployah. But the output is a
> real Helm release, so a little Helm knowledge helps if you want to debug deeply.
> For example, you can run `helm history` or `helm get` on what Deployah installed.

## How much Helm you need (from most to least)

```text
Most Helm knowledge                                          Least
   Werf   >   DevSpace   >   Score / Epinio / Kubero   >   Deployah
```

- **Werf:** you write the whole chart.
- **DevSpace:** you write Helm values.
- **Score, Epinio, Kubero:** no Helm for the developer, but a platform or admin
  sets things up.
- **Deployah:** no Helm, and nothing to install.

## Who creates the Helm chart?

This is the heart of the difference. Tools hide Helm in very different ways.

| Tool | Who creates the chart? | What the developer writes |
|---|---|---|
| **Deployah** | Deployah (a hidden umbrella chart) | A short spec (`deployah.yaml`) |
| **DevSpace** | DevSpace (a built-in "component chart") | Helm **values** in `devspace.yaml` |
| **Werf** | **You** (the developer) | A full chart: `Chart.yaml`, `values.yaml`, templates |
| **Score** | No chart (it makes raw YAML) | A short spec (`score.yaml`) |
| **Epinio** | Epinio (a generic app chart) | Nothing (you push source code) |
| **Kubero** | No app chart (an operator builds the objects) | A web UI, a CRD, or a Git repo |

Deployah and DevSpace both **own a hidden chart**. The difference is the input.
DevSpace asks for **Helm values**, which is closer to Helm. Deployah asks for a
**simple spec**, which is further from Helm. Werf is the other end: **you** write
the chart yourself.

## Deployah can also start a local cluster

Most of these tools need a Kubernetes cluster before you begin. Deployah can make
one for you. Run `deployah cluster up` and Deployah starts a local cluster (using
kind) on your machine. So you can go from zero to a running app with one tool,
even if you have no cluster yet.

This fits Deployah's goal: no Kubernetes knowledge, and nothing to set up first.
The other tools assume you already have a cluster (DevSpace, Werf, Score), or you
must install a platform into a cluster first (Epinio, Kubero).

## Full comparison

| | **Deployah** | **DevSpace** | **Werf** | **Score** | **Epinio** | **Kubero** |
|---|---|---|---|---|---|---|
| **What it is** | Stateless deploy CLI | Stateless dev and deploy CLI | Stateless build and deploy CLI (GitOps/CI) | Spec and generator CLI | In-cluster PaaS server | Self-hosted PaaS (operator and UI) |
| **Helm knowledge (developer)** | **None** | Some (values) | **Full** (writes charts) | None | None | None |
| **Who writes the chart** | Deployah (hidden) | DevSpace (component chart) | **You** | No chart (raw YAML) | Epinio (generic chart) | No app chart (operator) |
| **Footprint / setup** | **Nothing** | Nothing | Nothing | Nothing, but needs a platform pipeline | Server, registry, storage, UI | Operator, UI, CRDs |
| **Local cluster included?** | **Yes** (`deployah cluster up`, kind) | No | No | No | No | No |
| **Builds your image?** | No (you bring it) | Yes (and a dev loop) | Yes (Dockerfile/Stapel) | No (you bring it) | Yes (buildpacks) | Yes (buildpacks) |
| **Output** | **Helm release** | Helm release | Helm release (via Nelm) | Raw YAML (no install) | Helm release (hidden) | K8s objects via operator |
| **Multi-component** (service/worker/job, stateless/stateful) | **Yes, named** | Partial | No (you template each) | No (one workload per file) | No (web apps) | web/worker/cron, plus DB add-ons |
| **Multiple environments** | **Yes** (own context, config, env, vars) | Partial (profiles and vars) | Yes (env name; you template the diffs) | No (the platform decides) | Namespaces only | Pipelines (up to 4 stages) |
| **Installs and day-2** | **Yes** | Yes (and dev mode) | Yes (converge/plan/dismiss/status/logs) | No (you run `kubectl apply`) | Yes (and UI) | Yes (and UI) |
| **Maturity (mid-2026)** | Early, independent | Mature, CNCF, ~4.9k★ | Mature, CNCF, Flant, ~4.7k★ | Mature spec, CNCF | Active, ~585★ | Active, ~4.3k★ |

## Tool by tool

### DevSpace, the closest in design

DevSpace is the most similar tool to Deployah. It is also a small CLI with no
server, and it also installs a real Helm release. It even has a built-in chart
(the "component chart"), so you do not write templates.

The difference is the input. With DevSpace you write Helm **values** yourself, so
you need some Helm knowledge. DevSpace is also a tool for daily development: it
builds your image and gives you live reload (file sync, port-forward). Deployah
does not build images and has no dev loop. You give Deployah a simple spec, and you
need no Helm knowledge.

### Werf, the same shape but you write the chart

Werf is also a small CLI that ends with a Helm release. But with Werf **you write
the chart**: `werf.yaml`, `Chart.yaml`, `values.yaml`, and the Kubernetes files as
Helm templates. Werf also builds your image from source, and build and deploy go
together.

So Werf needs **full Helm and Kubernetes knowledge**. Deployah hides the chart and
needs none. This is the clearest difference on the Helm question. Choose Werf when
you know Helm well and want one tool for the whole pipeline.

### Score, a simple spec but it only generates

Score's small `score.yaml` is the most like Deployah's spec. But Score does not
install anything. It only **makes** Kubernetes YAML, and then you run
`kubectl apply` yourself. It has no status, logs, or delete commands.

Score also describes one workload per file and leaves environments to the
platform. It does have a nice model for dependencies: you ask for a Postgres, and
the platform creates it. In short, Score generates and stops; Deployah installs and
then manages the app for you.

### Epinio, the same idea but a full platform

Epinio also turns an app into a hidden Helm release. But Epinio is a heavy
platform that you install into the cluster: a server, a container registry,
storage, and a web UI. It builds your code from source and gives every app a URL.

This is great if you want a "push your code, get a URL" platform and you have a
team to run it. Deployah installs nothing, takes your pre-built image, and lets you
model many components across many clusters.

### Kubero, a Heroku-style platform

Kubero gives you a nice web UI, Git push to deploy, preview apps for pull requests,
and one-click databases. But it runs an operator, a UI, and its own state inside
your cluster.

Both tools hide Kubernetes from the developer. The difference is size: Kubero is a
full platform with a server and a UI, while Deployah is a small CLI with no server.

## When to choose Deployah

Choose **Deployah** if you want:

- To deploy with **no Helm or Kubernetes knowledge**, and **nothing to install**.
- To **start a local cluster** with one command and try things fast.
- A **short spec** for a project with **many components** (service, worker, job)
  across **many environments** (each with its own cluster and settings).
- A real **Helm release** at the end, which works well with GitOps and `helm`.
- You already **build your images in CI** and just want to ship them.

Choose another tool if you want:

- **DevSpace** for a live development loop and image building.
- **Werf** for one tool that builds and deploys, with full control of the chart,
  if you already know Helm and Kubernetes.
- **Score** for one spec that targets many platforms (Kubernetes and Docker
  Compose), run by a platform team.
- **Epinio** for a "push code, get a URL" platform with a web UI.
- **Kubero** for a full Heroku-like platform with a UI, Git push, and databases.

## Where Deployah is weaker (honest)

- It is **younger** and has **no big company or CNCF backing** yet. DevSpace, Werf,
  and Score have that support and many more stars.
- It does **not** build images. It has **no** dev loop, **no** web UI, **no**
  database add-ons, and **no** Git or pull-request preview apps.
- It has **no dependency model**. (Score's strength is "ask for a Postgres, the
  platform creates it.")
- Werf and DevSpace give **more control** for people who already know Helm.

## Sources

- DevSpace: <https://www.devspace.sh/>, <https://github.com/devspace-sh/devspace>
- Werf: <https://werf.io/>, <https://github.com/werf/werf>, <https://github.com/werf/nelm>
- Score: <https://score.dev/>, <https://github.com/score-spec/spec>
- Epinio: <https://epinio.io/>, <https://github.com/epinio/epinio>
- Kubero: <https://www.kubero.dev/>, <https://github.com/kubero-dev/kubero>
