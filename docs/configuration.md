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

It helps to know there are two different things:

1. **Substitution variables.** These fill `${...}` placeholders in your spec
   before Deployah reads it. Use them to change the spec itself, such as the
   image tag or the ingress host. This works today and is described below.
2. **Container environment variables.** These are the variables your app reads
   at runtime. You would set them with the `env` field on a component. Note:
   that field is accepted by the schema but is **not applied to the running
   container yet** (it is planned). For now, put runtime values into your image
   or your app's own config.

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

### Where values come from

Deployah looks for a variable in three places. If the same name is set in more
than one place, the later one wins (lowest to highest):

1. **The environment's `variables`** in your spec. Write these with their plain
   name, with no prefix.
2. **The environment's env file**, for example `.env.production`. Only keys that
   start with `DPY_VAR_` are used, and the prefix is removed.
3. **Your shell**, also with the `DPY_VAR_` prefix.

So the same `${APP_ENV}` can come from any of these:

```yaml
# in deployah.yaml (no prefix here)
environments:
  production:
    variables:
      APP_ENV: from-spec
```

```env
# in .env.production (needs the prefix)
DPY_VAR_APP_ENV=from-envfile
```

```sh
# in your shell (needs the prefix)
export DPY_VAR_APP_ENV=from-shell
```

With all three set, `${APP_ENV}` is `from-shell`, because the shell wins.

> [!NOTE]
> Only env-file and shell variables need the `DPY_VAR_` prefix, because
> Deployah has to pick its own variables out of all the others on your system.
> The `variables` you write inside the spec do not need a prefix.

### Env files

An env file is a simple list of `KEY=value` lines. Blank lines and lines that
start with `#` are ignored, and spaces around the key and value are trimmed.

If you do not set `envFile` for an environment, Deployah looks for a file in
this order and uses the first one it finds:

1. `.env.<environment>` (for example `.env.production`)
2. `.deployah/.env.<environment>`
3. `.env`
4. `.deployah/.env`

If you do set `envFile` and the file is missing, Deployah stops with an error.

### Files: Deployah vs. your app

| File | Used by | Purpose |
|---|---|---|
| `deployah.yaml` | Deployah | Your spec. |
| `.env` / `.env.<env>` | Deployah and your app | Variables. Deployah only reads the keys that start with `DPY_VAR_`. |
| `config.yaml` / `config.<env>.yaml` | Your app | Your app's own config. Deployah ignores these. |

Keys in an env file that do not start with `DPY_VAR_` are left alone. Deployah
does not use them, so they are free for your app to read on its own. The `config`
files are for your app only.

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
| Substitution variables (`${...}`) | shell `DPY_VAR_*` → env file `DPY_VAR_*` → the environment's `variables` in the spec |
| Env file | explicit `envFile` in the spec → `.env.<env>` → `.deployah/.env.<env>` → `.env` → `.deployah/.env` |
| Platform file location | `--platform-file` flag → `DEPLOYAH_PLATFORM_FILE` env var → same directory as the spec |

Two context situations print a warning, so a deploy to the wrong cluster is
visible before it happens:

- `--context` overrides the platform file's context for the environment.
  Silence it with `DEPLOYAH_ALLOW_CONTEXT_MISMATCH=1`.
- The environment has no context anywhere. The deploy then follows your
  kubeconfig's current context, and the warning names that context.

See the [README](../README.md) for the project overview and the other guides.
