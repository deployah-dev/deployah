# Configuration

Every deploy targets one environment. This page explains how the environment is
chosen, the two kinds of variables, where values come from, and the precedence
rules that decide which value wins.

## Environments and variables

Deployah supports multiple environments (for example `dev`, `staging`, `prod`).
The [platform file](platform.md) registers them, and you choose one when you
deploy:

```sh
deployah deploy staging
```

The `environments` section in `deployah.yaml` is optional. Add an entry only
when an environment needs its own substitution values or env file:

```yaml
environments:
  production:
    variables:
      TAG: v1.4.2
```

### How the environment is picked

When you name an environment, Deployah checks it against a registry: the
platform file's environments when that file exists, otherwise the spec's
`environments` keys, if any are defined. A name outside the registry is an
error that lists the valid names. With no registry at all, any name is
accepted. Matching is exact first, then by prefix: a `review` entry matches
`review/pr-123`.

When you do not name one: a single registered environment is selected
automatically, several make Deployah stop and list them, and none means a
built-in `default` environment is used.

### Two kinds of variables

There are two different things, and they do not share sources:

1. **Substitution variables.** These fill `${...}` placeholders in your spec
   before Deployah reads it. Use them to change the spec itself, such as the
   image tag or the ingress host. Values come from `environments.*.variables`
   and from process `DPY_VAR_*` (prefix stripped). Dotenv files are not part
   of this path.
2. **Container environment variables.** These are what your process reads
   inside the pod. They come from dotenv files (`FileValues`, delivered as a
   ConfigMap plus `envFrom`) and from YAML `env:` (`ExplicitValues`, delivered
   as container `env:`). Kubernetes lets `env:` win when a key exists in both.
   Keys in a dotenv file named `DPY_VAR_TAG` stay `DPY_VAR_TAG` in the
   container. They do not fill `${TAG}`.

`envFile` is ConfigMap data. It is not a secret store. Deployah does not treat
names such as `PASSWORD` specially.

### Substitution variables

You can use `${NAME}` placeholders anywhere in your spec. Two forms are
supported:

- `${NAME}` is required. If the variable is not set, Deployah stops with an
  error ("variable not set"). This stops you from deploying with a missing
  value.
- `${NAME:-default}` uses `default` when the variable is not set.

For example:

```yaml
components:
  web:
    image: nginx:${TAG:-latest}   # uses "latest" when TAG is not set
    port: 80
    environments: [prod]
```

Deployah uses [fluxcd/pkg/envsubst](https://github.com/fluxcd/pkg/envsubst) under
the hood, so more shell-style forms work too. The full list is below.

#### All supported forms

These forms come from
[fluxcd/pkg/envsubst](https://github.com/fluxcd/pkg/tree/main/envsubst#supported-functions).
In the table, `var` is your variable name.

| Expression | Meaning |
|---|---|
| `${var}` | The value of `var`. |
| `${#var}` | The length of `var`. |
| `${var^}` | Uppercase the first character. |
| `${var^^}` | Uppercase all characters. |
| `${var,}` | Lowercase the first character. |
| `${var,,}` | Lowercase all characters. |
| `${var:n}` | Start `n` characters in. |
| `${var:n:len}` | Start `n` characters in, take up to `len` characters. |
| `${var#pattern}` | Remove the shortest `pattern` match from the start. |
| `${var##pattern}` | Remove the longest `pattern` match from the start. |
| `${var%pattern}` | Remove the shortest `pattern` match from the end. |
| `${var%%pattern}` | Remove the longest `pattern` match from the end. |
| `${var-default}` | Use `default` if `var` is not set. |
| `${var:-default}` | Use `default` if `var` is not set or is empty. |
| `${var=default}` | Use `default` if `var` is not set. |
| `${var:=default}` | Use `default` if `var` is not set or is empty. |
| `${var/pattern/replacement}` | Replace the first `pattern` match with `replacement`. |
| `${var//pattern/replacement}` | Replace every `pattern` match with `replacement`. |
| `${var/#pattern/replacement}` | Replace a `pattern` match at the start with `replacement`. |
| `${var/%pattern/replacement}` | Replace a `pattern` match at the end with `replacement`. |

Remember: Deployah runs in strict mode. A variable with no default must be set,
or the deploy stops with an error.

### Where substitution values come from

`${...}` looks in two places. The later one wins:

1. **The environment's `variables`** in your spec. Write these with their plain
   name, with no prefix.
2. **Your shell**, with the `DPY_VAR_` prefix. The prefix is removed.

```yaml
# in deployah.yaml (no prefix here)
environments:
  production:
    variables:
      APP_ENV: from-spec
```

```sh
# in your shell (needs the prefix)
export DPY_VAR_APP_ENV=from-shell
```

With both set, `${APP_ENV}` is `from-shell`.

> [!NOTE]
> Shell variables need the `DPY_VAR_` prefix so Deployah can pick them out of
> the rest of your process environment (`HOME`, `CI`, and so on). Those
> unprefixed process keys never enter the pod. The `variables` you write
> inside the spec do not need a prefix.

### Container env files

An env file is a list of `KEY=value` lines. Blank lines and lines that start
with `#` are ignored, and spaces around the key and value are trimmed. Every
key becomes ConfigMap data. Keys must be POSIX names
(`^[A-Za-z_][A-Za-z0-9_]*$`).

Discovery is relative to the directory that contains `deployah.yaml`. Explicit
`envFile` replaces that layer only. Missing implicit files are skipped.
A missing explicit path is an error.

**Environment values** (shared by every component and task in that
environment):

- Explicit `environments.<env>.envFile`, or
- Merge, later wins: `.deployah/.env` then `.env` then
  `.deployah/.env.<env>` then `.env.<env>`

**Entity values** (one component or task):

- Explicit `envFile` on that entity, or
- If the task has `from:`, a copy of the parent component's already-resolved
  entity values (not the parent's file path), or
- Merge, later wins: `.deployah/.env.<name>` then `.env.<name>` then
  `.deployah/.env.<name>.<env>` then `.env.<name>.<env>`

**FileValues** = environment values, then entity values (entity wins on
overlap). **ExplicitValues** = inherited YAML `env:` from `from:`, then this
entity's `env:`.

`deployah resolve <env>` lists both maps with their resolved values. It is
a debugging command; do not treat that output as secret-safe CI output.
`deployah run` does not reuse the Helm `{fullname}-env` ConfigMap. Each run
creates a Job-owned ConfigMap and only detaches after that ownership is set.

### Files: Deployah vs. your app

| File | Used by | Purpose |
|---|---|---|
| `deployah.yaml` | Deployah | Your spec. |
| `.env` / `.env.<env>` / `.env.<component>` | Deployah | Container env (ConfigMap data). Not `${...}` substitution. |
| `config.yaml` / `config.<env>.yaml` | Your app | Your app's own config. Deployah ignores these. |

## Precedence rules

Several settings can come from more than one place. This table shows the
order Deployah checks them in; the first match wins.

| Setting | Order (first match wins) |
|---|---|
| Environment registry (which names you may deploy to) | platform file environments → spec `environments` keys → any name |
| Environment selection (no name given) | the single registered environment → error listing them when there are several → built-in `default` when there are none |
| Kubernetes context | `--context` flag → `context` in the platform file for that environment → your kubeconfig's current context |
| Expose domain | `expose.domain` in the spec → the domain marked `default: true` in the platform file → the environment's only domain |
| Expose hostname label | `expose.apex: true` (bare domain) → `expose.subdomain` in the spec → the component name |
| Profiles | component `profiles` list (with platform `default` prepended when defined) → merged left to right; omitted field applies only `default` when present |
| Substitution variables (`${...}`) | process `DPY_VAR_*` (prefix stripped) → the environment's `variables` in the spec |
| Container FileValues | entity dotenv layer → environment dotenv layer |
| Container ExplicitValues | entity YAML `env:` → inherited parent `env:` |
| Platform file location | `--platform-file` flag → `DEPLOYAH_PLATFORM_FILE` env var → same directory as the spec |

Two context situations print a warning, so a deploy to the wrong cluster is
visible before it happens:

- `--context` overrides the platform file's context for the environment.
  Silence it with `DEPLOYAH_ALLOW_CONTEXT_MISMATCH=1`.
- The environment has no context anywhere. The deploy then follows your
  kubeconfig's current context, and the warning names that context.

See the [README](../README.md) for the project overview and the other guides.
