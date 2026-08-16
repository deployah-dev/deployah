# Tasks

Run-to-completion work such as migrations, smoke checks, and one-off backfills.
You declare tasks next to components in `deployah.yaml`. Deployah runs hook
tasks around deploy, and you run any task yourself with `deployah run`.

## Add a migrate task

Point the task at a component with `from` so it reuses that image and env.
Set `"on": preDeploy` so it runs before the app starts on every install and
upgrade. Quote `"on"` in YAML 1.1 so it is not read as a boolean.

```yaml
apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: ghcr.io/acme/shop:1.2.3
    env:
      DATABASE_URL: ${DATABASE_URL}
tasks:
  migrate:
    from: api
    "on": preDeploy
    command: ["migrate", "up"]
```

`from` copies env, environments, profiles, and resources. It also copies
envFile and configFile paths, but those files are not mounted on the Job
yet (same as components). It does not copy command, args, or service fields
such as port. Profiles apply to the Job: node selector, tolerations, and
security context. Task `env` overlays the parent map. Runtime secrets for
tasks go in `env:` (inherited or overlay). `${...}` substitution works the
same as elsewhere in the spec.

`command` is required when the task uses the parent image. If you set `image`
on the task, command is optional.

## Run a smoke check after deploy

```yaml
tasks:
  smoke:
    from: api
    "on": postDeploy
    command: ["curl", "-f", "http://api/health"]
```

`on` is one value: `preDeploy`, `postDeploy`, or `manual`. To run the same
command before and after deploy, define two tasks that share `from`.

`after` orders tasks **inside the same `on`**. The named task must also run in
every environment the dependent runs in. Cross-phase `after` is an error.
`after` is not allowed on `manual` tasks.

## Run a task yourself

`deployah run` works for every task, including hooks you want to retry.
It runs only that task, not the tasks in its `after` list.

```sh
deployah run backfill production --yes
```

Manual tasks exist only for the CLI. They are not part of the Helm release.

```yaml
tasks:
  backfill:
    from: api
    "on": manual
    command: ["backfill"]
```

Wait is the default. `--detach` returns after the Job is created. Concurrent
runs are allowed; each run gets a unique Job name.

## Fanout

Fanout runs several indexed copies of a task. Use a number as a shortcut
(count, one at a time) or an object. It works on `preDeploy`, `postDeploy`,
and `manual`.

```yaml
tasks:
  migrate:
    from: api
    "on": preDeploy
    command: ["migrate", "up"]
    fanout: 2                 # two copies on every deploy
  backfill:
    from: api
    "on": manual
    command: ["backfill"]
    fanout: 4                 # count 4, parallelism 1
    # fanout:
    #   count: 4
    #   parallelism: 2
```

`--count` and `--parallelism` on `deployah run` override that execution only.
Each copy sees `JOB_COMPLETION_INDEX` (0, 1, 2, ...). Parallelism is capped at
count and cannot exceed 100000 (the Kubernetes Indexed Job limit).

## First install and the database

On a first install, `preDeploy` runs **before** Deployments and Services. A
migrate task that talks to Postgres needs that database already reachable
(another release, a managed DB, or a job you ran first). `deployah plan`
prints this reminder on a fresh install.

## Logs

```sh
deployah logs shop --component=migrate --no-follow
deployah logs shop --component=backfill --no-follow
```

The component label is the task name. Finished Job pods are included, not
only running ones.

## Rollback

A Helm rollback does **not** run tasks. Failed hook Jobs are kept so you can
read logs. Migrations that already ran are not reverted; write a down
migration and `deployah run` it if you need that.

## See also

- [Spec reference](spec-reference.md#tasks) for every field
- [Troubleshooting](troubleshooting.md) for a failed hook or a tight timeout
