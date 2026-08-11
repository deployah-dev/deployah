# Deployah

[![codecov](https://codecov.io/gh/deployah-dev/deployah/graph/badge.svg)](https://codecov.io/gh/deployah-dev/deployah)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13862/badge)](https://www.bestpractices.dev/projects/13862)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/deployah-dev/deployah/badge)](https://scorecard.dev/viewer/?uri=github.com/deployah-dev/deployah)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/deployah-dev/deployah)

<!-- markdownlint-disable MD033 -->
<p align="center">
  <img src="https://deployah.dev/demos/nginx.gif" alt="Deployah: write a short nginx spec, deploy to a local cluster, get a URL" width="800">
</p>
<!-- markdownlint-enable MD033 -->

**No chart to maintain. Zero cluster-side setup. One binary.**

Deployah is a CLI that deploys apps to Kubernetes. It sits in the gap between
tools that still ask you to write Helm and tools that need a heavy in-cluster
platform. It uses Helm under the hood, embeds the Helm and Kubernetes clients
in one binary, and installs nothing in your cluster.

You write a short **spec**. Deployah turns it into a running **release** on
Kubernetes. We call this **Spec-to-Release**. It is like Source-to-Image (S2I),
but for the deploy step: S2I builds your image, and Deployah runs your release.

## Who this is for

Deployah is for teams that already use Kubernetes and want a clear ownership
boundary between the app and the platform.

- **App / developer** owns `deployah.yaml`: image, port, resources, expose
  intent, scaling, persistence, workers. No Kubernetes context. No real domain
  names.
- **Platform** owns `deployah.platform.yaml`: cluster contexts, domains, TLS,
  and profiles (placement, security, monitor labels, and related policy).

You still need a cluster you can reach (kubeconfig). Deployah talks to that
cluster itself.

### Use Deployah when

- You want multi-environment app deploys without maintaining a chart per app
- Platform policy (domains, TLS, profiles) should change without editing every
  app spec
- The common shapes are enough: Deployment/StatefulSet, Service, Ingress, HPA,
  PVC, workers, ServiceMonitor/PodMonitor

### Do not use Deployah when

- You already have a Helm chart and release process you trust for that app
- You only need a one-shot (`kubectl create deployment` or `helm install` of a
  third-party chart)
- You need an operator-managed database or other specialized controllers

This is **not** "Kubernetes for people who do not know Kubernetes." It is
Spec-to-Release: a short app spec plus a platform map, instead of each team
owning a chart.

For how Deployah compares to DevSpace, Werf, Score, Epinio, and Kubero, see
[docs/comparison.md](docs/comparison.md).

## Features

- **Spec-to-Release** - short `deployah.yaml` becomes a Helm release; no chart
  to maintain
- **Ownership split** - app owns the spec; platform owns contexts, domains,
  TLS, and profiles
- **Any reachable cluster** - uses your kubeconfig; installs nothing in the
  cluster
- **One binary** - embeds Helm and the Kubernetes client (no separate `helm` /
  `kubectl` required)
- **Services and workers** - HTTP services with Ingress, or `role: worker`
  without Service/Ingress
- **Stateless and stateful** - Deployments or StatefulSets, optional PVCs,
  volume resize
- **Custom environments** - name your own environments (`local`, `staging`,
  `prod`), plus prefix-matched targets like `review/pr-123`; platform maps each
  one to a cluster, domains, and TLS
- **Profiles** - platform policy for placement, security, resources, and scrape
  labels
- **Autoscaling and health** - HPA, ready/alive checks (HTTP/TCP or exec for
  workers)
- **Metrics** - ServiceMonitor / PodMonitor when Prometheus Operator is present
- **Extras** - custom manifests and CRDs under `.deployah/`
- **Plan before apply** - `deployah plan` / `deployah deploy` with validation
  and deploy guards

## Current limitations

Know these before you invest time:

- **`env` is not applied yet.** The `env` field on a component passes schema
  validation but does not reach the running container. Put runtime values in
  your image or your app's own config for now. See
  [Two kinds of variables](docs/configuration.md#two-kinds-of-variables).
- **`role: job` is not deployable yet.** It exists in the schema; only
  `service` and `worker` deploy today.
- **Deployah does not build images.** Give it an image that already exists in a
  registry your cluster can pull from.
- **Stateful with persistence needs Kubernetes 1.32 or newer.** Deployah checks
  the API version and fails fast on older clusters. Identity-only stateful
  components have no such floor.
- **The schemas are alpha.** App manifests are at `v1-alpha.4` and platform
  files at `platform/v1-alpha.3`; expect breaking changes between releases.

## Contents

- **Get it running**
  - [Installation](#installation)
  - [Requirements](#requirements)
  - [Quick start](#quick-start)
  - [Examples](#examples)
- **Understand the model**
  - [How Deployah works](#how-deployah-works)
  - [Concepts](#concepts)
- **Look things up**
  - [Guides](#guides)
  - [Commands](#commands)
  - [Schema reference](#schema-reference)
- **Take part**
  - [Community](#community)
  - [Development](#development)
  - [License](#license)

## Installation

### Homebrew

```sh
brew install deployah-dev/tap/deployah
```

### Using Nix (recommended)

If you have [Nix](https://nixos.org/download.html) installed:

```sh
# Run without installing
nix run github:deployah-dev/deployah

# Or add it to your flake.nix
inputs.deployah.url = "github:deployah-dev/deployah";
```

### Install with Go

```sh
go install deployah.dev/deployah@latest
```

## Requirements

Deployah is a single binary. You do **not** need the `helm`, `kubectl`, or `kind`
command-line tools. Deployah includes Helm, the Kubernetes client, and Kind as
libraries, so it talks to your cluster by itself. That removes a whole class of
"works on my machine" problems caused by missing or mismatched CLI tools.

- **Deploy to a cluster you already have:** you only need access to it (a
  kubeconfig). No container runtime is required.
- **Use the built-in local cluster** (`deployah cluster up`): you need a
  container runtime, either **Docker** or **Podman**. This is the only extra
  tool, and it is needed only for the local cluster.

## Quick start

This walks you through one full deploy on your own machine. It takes about five
minutes. For the local cluster you need Docker or Podman running (see
[Requirements](#requirements)).

You do not need an existing Kubernetes cluster. Deployah can make a local one
for you.

Deployah reads two files, with two different owners (see
[Who this is for](#who-this-is-for)):

- `deployah.yaml` - what to run. You write this one.
- `deployah.platform.yaml` - where it runs. In this walkthrough,
  `deployah cluster up` writes it for you. On a real cluster, a platform team
  writes it once.

### 1. Start a local cluster

```sh
deployah cluster up
```

This creates a small local Kubernetes cluster (using Kind) and gives it the
context name `kind-deployah`.

### 2. Create a spec

Save this as `deployah.yaml` in an empty folder. It runs the public `nginx`
image, so you do not need to build anything.

```yaml
apiVersion: v1-alpha.4
project: my-first-app
components:
  web:
    image: nginx:latest
    port: 80
    environments: [local]   # deploy this component in the "local" environment
    expose: true
```

`expose: true` gives the component a hostname made from its name (here
`web.127.0.0.1.nip.io`) with HTTPS, all decided by the platform file.

You do not write `deployah.platform.yaml` here: `deployah cluster up` already
created it and registered the `local` environment with a domain and a
Kubernetes context. See the [platform file guide](docs/platform.md) for its
shape.

### 3. Deploy

```sh
deployah deploy local
```

### 4. See it running

```sh
# Show the status of your project
deployah status my-first-app

# Show the local cluster and the URLs you can open
deployah cluster status
```

`deployah cluster status` prints a ready-to-use URL for your app. Open it in
your browser to see the nginx welcome page.

You can also stream the logs:

```sh
deployah logs my-first-app
```

### 5. Clean up

```sh
# Remove the app
deployah delete my-first-app local

# Stop and delete the local cluster
deployah cluster down
```

Next, read [Concepts](#concepts) for the vocabulary, then pick the guide you
need from [Guides](#guides).

## Examples

Runnable specs live under [`examples/`](examples/). Start with
[`examples/nginx`](examples/nginx): the same flow as the quick start, ready to
copy.

## How Deployah works

Deployah turns your `deployah.yaml` spec into a running Kubernetes deployment in
three steps.

```mermaid
flowchart LR
    subgraph phase1["1. Read the spec"]
        direction TB
        A["YAML spec"] --> B["Parse"] --> C["Validate"]
    end
    subgraph phase2["2. Resolve config"]
        direction TB
        D["Pick environment"] --> E["Apply variables"] --> F["Fill defaults"]
    end
    subgraph phase3["3. Deploy"]
        direction TB
        G["Build Helm values"] --> H["Install release"]
    end
    phase1 --> phase2 --> phase3
```

1. **Read the spec.** Deployah reads your `deployah.yaml` and checks it against a
   JSON Schema, so mistakes are caught early with clear messages.
2. **Resolve config.** Deployah picks the environment you asked for, substitutes
   your variables, and fills in sensible defaults.
3. **Deploy.** Deployah builds Helm values from your spec and installs a Helm
   release on your cluster. You never write a Helm chart yourself.

For how Deployah compares to similar tools (DevSpace, Werf, Score, Epinio,
Kubero), see [docs/comparison.md](docs/comparison.md).

## Concepts

A few words you will see often.

- **Project.** One app, with a name. The name prefixes the Kubernetes resources
  Deployah creates. It is the `project` field in your spec.
- **Component.** One deployable part of your project, such as a web service or a
  background worker. A project has one or more components.
- **Role.** What a component is for:
  - `service`: it serves traffic and can be exposed (the default).
  - `worker`: a long-running background task, not exposed.
  - `job`: a one-off task that runs and then stops.
- **Kind.** The component's `kind` field: `stateless` (the default, easy to
  scale) or `stateful` (StatefulSet with stable identity; optional per-pod
  volumes). This field has nothing to do with Kind, the tool that runs the
  optional local cluster. See
  [Stateful workloads](docs/workloads.md#stateful-workloads) and
  [Storage classes](docs/platform.md#storage-classes).
- **Workload matrix.** `role` and `kind` combine independently. `job` is in
  the schema but not deployable yet.

  | Capability          | service+stateless | service+stateful | worker+stateless | worker+stateful |
  |---------------------|:-----------------:|:----------------:|:----------------:|:---------------:|
  | Deployment          | Y | - | Y | - |
  | StatefulSet         | - | Y | - | Y |
  | ClusterIP Service   | Y | Y | - | - |
  | Headless Service    | - | Y | - | Y |
  | Ingress             | Y | Y | - | - |
  | HPA                 | Y | Y | Y | Y |
  | Persistence         | Y | Y | Y | Y |
  | Health (TCP/HTTP)   | Y | Y | - | - |
  | Health (exec)       | Y | Y | Y | Y |
  | ServiceMonitor      | Y | Y | - | - |
  | PodMonitor          | - | - | Y | Y |

- **Environment.** A target such as `dev`, `staging`, or `prod`. Each
  environment can use a different cluster, different files, and different
  variables.

  The word `environments` appears in three places, and each one does a
  different job:

  | Where | This guide calls it | What it does |
  |---|---|---|
  | `environments` in the platform file | environment **registry** | Declares which environment names exist, and maps each to a cluster context, domains, and TLS |
  | `environments` at the top of `deployah.yaml` | environment **overrides** | Optional per-environment substitution variables and env file |
  | `environments` inside a component | environment **filter** | Restricts that component to the listed environments |

- **Resource preset.** A quick way to set CPU and memory without knowing
  Kubernetes units. Use `resourcePreset: small` instead of writing exact values.
  This is not the same as a [profile](docs/platform.md#profiles).
- **Profile.** A named deployment policy owned by the platform team (node
  placement, security context, domain and resource ceilings, monitor labels,
  and more). Components select one or more with `profiles: [...]`. See
  [Profiles](docs/platform.md#profiles).
- **Health checks.** Services get automatic TCP/HTTP ready and alive checks.
  Workers use process-exit by default, or optional `health.alive.exec`. See
  [Health checks](docs/workloads.md#health-checks) and
  [Worker components](docs/workloads.md#worker-components).
- **Bring your own image.** Deployah does not build images. You give it an image
  that already exists in a registry your cluster can pull from. Build your image
  in CI (or locally), then let Deployah deploy it.

## Guides

The README covers the shape of the tool. The details live in `docs/`:

| Guide | What is in it |
|---|---|
| [Spec reference](docs/spec-reference.md) | Every `deployah.yaml` field, value rules, resource presets, and full examples. |
| [Platform file](docs/platform.md) | Contexts, domains, TLS modes, storage classes, and profiles. |
| [Workloads](docs/workloads.md) | Stateful components and volumes, workers, health checks, metrics. |
| [Configuration](docs/configuration.md) | Environment selection, variables, `.env` files, precedence rules. |
| [Networking](docs/networking.md) | Reaching your app, and how the local cluster resolves hostnames. |
| [Custom manifests and CRDs](docs/custom-manifests-and-crds.md) | Ship plain Kubernetes YAML alongside the release. |
| [Troubleshooting](docs/troubleshooting.md) | What to do when a deploy or a hostname does not work. |
| [Deployah vs. similar tools](docs/comparison.md) | Honest comparison with DevSpace, Werf, Score, Epinio, and Kubero. |
| [CLI reference](docs/cli/deployah.md) | Generated documentation for every command and flag. |

## Commands

Run `deployah <command> --help` for the full details of any command. A complete,
generated reference for every command and flag is in
[docs/cli/](docs/cli/deployah.md).

Deployah can also generate a shell completion script: run `deployah completion`
(use `-o` to write it to a file). See `deployah completion --help` for details.

### Global flags

These work with every command:

| Flag | Short | Meaning |
|---|---|---|
| `--spec` | `-s` | Path to the spec file (default: `deployah.yaml`). |
| `--platform-file` | | Path to the platform config file (overrides `DEPLOYAH_PLATFORM_FILE` and the default same-directory lookup). |
| `--namespace` | `-n` | Kubernetes namespace to use. |
| `--context` | | Kubernetes context to use (overrides the platform file's context). |
| `--kubeconfig` | `-k` | Path to your kubeconfig file. |
| `--timeout` | `-t` | Timeout for operations (default: 10m). |
| `--debug` | `-d` | Verbose logging, and keep temporary files. |

### Working with apps

| Command | What it does |
|---|---|
| `deployah init` | Create a new spec and platform file by answering a few questions. Also scaffolds `.deployah/manifests/` and `.deployah/crds/` for [custom manifests and CRDs](docs/custom-manifests-and-crds.md). Use `-o` to set the output file, `--force` to overwrite an existing one, or `--dry-run` to preview. Non-interactive: `--project`, `--environments`, `--set key=value`, or `--defaults` to skip every prompt. |
| `deployah validate` | Check the manifest schema (offline). When a platform file exists, also cross-check `expose.domain` keys and environment names against it. |
| `deployah validate <environment>` | Also load the platform file and check the resolved configuration for that environment. |
| `deployah resolve <environment>` | Preview the fully resolved hostname, TLS mode, and context, offline. Use `--output json` for machine-readable output. |
| `deployah resolve --environments` | List every environment from both files: where it is registered, its context (or the kubeconfig fallback), domains, and overrides. |
| `deployah plan <environment>` | Preview what a deploy would change, without applying anything. Extra manifests from `.deployah/manifests/` appear in the diff; pending CRDs are reported but not applied. Use `--offline` to render with no cluster access, `--drift` to also compare against live cluster state, or `--output json` for CI. |
| `deployah deploy <environment>` | Deploy your project. Shows the plan and asks for confirmation before applying; use `-y`/`--yes` to skip the prompt, `--reapply` to upgrade even with no changes, `--crds` for [CRD install policy](docs/custom-manifests-and-crds.md#crd-policy) (`create` or `create-replace`), `--explain` to print the resolution report first, `--force-hostname-change` to bypass the hostname guard, or `--resize-volumes` to grow [persistence](docs/workloads.md#growing-volumes) sizes. |
| `deployah status <project>` | Show the status of a deployed project. Use `--detailed` for pod details, `-e` for an environment. |
| `deployah logs <project>` | Stream logs. Filter with `--component`, `-e`, `--container`, `--since`, `--tail`. Use `--no-follow` for a one-off read. |
| `deployah shell <project>` | Open a shell in a running container. Choose with `--component` and `--container`. |
| `deployah list` | List deployed projects. Filter with `-p` (project) and `-e` (environment). |
| `deployah delete <project> <environment>` | Remove a deployment. Fails if no platform file is found, unless you pass `--allow-missing-platform`. Use `-y`/`--yes` to skip the prompt, `--dry-run` or `--show-resources` to preview, and `--wait` to block until resources are gone. |

### Working with the local cluster

| Command | What it does |
|---|---|
| `deployah cluster up` | Create the local cluster, start the cloud provider, and create or update `deployah.platform.yaml` with a `local` environment. |
| `deployah cluster status` | Show the cluster status and the URLs you can open. |
| `deployah cluster down` | Delete the local cluster. Use `--force` to skip the prompt. |
| `deployah cluster kubeconfig` | Print the local cluster kubeconfig path. Use `--raw` for its contents. |

## Schema reference

Deployah validates your spec and platform file with JSON Schema.

- **Manifest schema version:** v1-alpha.4
- **Manifest schema:** `internal/spec/schema/v1-alpha.4/manifest.json`
- **Manifest environments schema:** `internal/spec/schema/v1-alpha.4/environments.json`
- **Platform schema version:** platform/v1-alpha.3
- **Platform schema:** `internal/spec/schema/platform/v1-alpha.3/platform.json`

For the latest schema and examples, see the
[schema directory](internal/spec/schema/) in the repository.

## Community

Use [GitHub Discussions](https://github.com/deployah-dev/deployah/discussions)
for questions, early ideas, and showcases. Use
[Issues](https://github.com/deployah-dev/deployah/issues) for bugs, feature
requests, and design proposals. See [CONTRIBUTING.md](CONTRIBUTING.md) for how
to contribute code and docs. Report security issues privately using
[SECURITY.md](SECURITY.md).

- **Questions:** [Q&A](https://github.com/deployah-dev/deployah/discussions/categories/q-a)
- **Ideas:** [Ideas](https://github.com/deployah-dev/deployah/discussions/categories/ideas)
- **Start here:** [Welcome to Deployah Discussions](https://github.com/deployah-dev/deployah/discussions/36)
- **Contributing:** [CONTRIBUTING.md](CONTRIBUTING.md)
- **Security:** [SECURITY.md](SECURITY.md)
- **Code of conduct:** [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

## Development

The Nix flake is the main dev and CI interface. With
[direnv](https://direnv.net/) (the `.envrc` uses `use flake`), the tools load
automatically when you enter the repo.

```sh
nix develop
```

### Format, lint, and tidy

```sh
nix run .#fmt                  # format Go (gofumpt + gci)
nix run .#lint                 # golangci-lint
nix run .#lint-md              # markdownlint
nix run .#tidy                 # go mod tidy
nix run .#update-vendor-hash   # refresh vendorHash after go.sum changes
```

### Tests

Unit and integration tests are split by build tag. Plain `go test ./...` skips
the integration tests.

```sh
nix run .#test-unit          # unit tests with the race detector
nix run .#test-integration   # scenario tests in internal/testing
```

Coverage profiles are written to `coverage-unit.out` and
`coverage-integration.out`.

### Running e2e tests locally

Requires Docker. Creates and destroys a Kind cluster named `deployah`.

```sh
DEPLOYAH_E2E_FORCE=1 nix run .#test-e2e
```

Skips automatically when no container engine is found (unless `CI=true`).
Set `DEPLOYAH_E2E_DUMP=1` with `-v` to print live objects when adding a scenario.

### Build and run

```sh
nix build              # build the deployah binary
nix run . -- --help    # run without installing
nix run .#demo         # render docs/demo/tapes into docs/assets/ (see docs/demo)
nix run .#publish-demo # sync docs/assets/ to R2 (needs R2_* env vars)
```

### CI

GitHub Actions runs flake validation, lint/fmt/tidy checks, `nix run .#test-unit`,
`nix run .#test-integration`, and `nix run .#test-e2e` on every pull request and
push to `main`.
Scenario fixtures under `scenarios/` and e2e fixtures under
`internal/e2e/testdata/` (including `deployah.yaml`, `deployah.platform.yaml`,
and `.deployah/`) are tracked so tests can run on a clean checkout.

```sh
nix flake check   # runs the pre-commit hooks (lint, markdownlint, links, tidy, nixfmt)
```

Format Nix files with `nix fmt`.

## License

Deployah is licensed under the [Apache License 2.0](LICENSE).
