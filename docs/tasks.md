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

`on` is one value: `preDeploy`, `postDeploy`, `manual`, or `schedule`. To run the same
command before and after deploy, define two tasks that share `from`.

`after` orders tasks **inside the same `on`**. The named task must also run in
every environment the dependent runs in. Cross-phase `after` is an error.
`after` is not allowed on `manual` or `schedule` tasks.

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

## Scheduled tasks

Set `"on": schedule` so Deployah creates a Kubernetes CronJob in the release.
`deployah deploy` applies the CronJob and does not start a Job on that deploy.
Quote `"on"` in YAML 1.1.

```yaml
tasks:
  cleanup:
    from: api
    "on": schedule
    schedule: "0 3 * * *"
    command: ["cleanup"]
```

Fields:

- `schedule`: a 5-field cron expression, a Vixie step such as `*/5`, a
  named weekday (`sun`-`sat`), `?` (same as `*`), or a descriptor
  (`@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`,
  `@annually`, `@every 1h`). Do not put `TZ=` or `CRON_TZ=` in the string;
  use `timeZone`.
- `timeZone`: IANA name. Defaults to `Etc/UTC`. Values other than `Etc/UTC`
  need Kubernetes 1.27 or later; older API servers drop the field with no
  error.
- `concurrencyPolicy`: `Allow`, `Forbid`, or `Replace`. Defaults to
  `Forbid`.
- `timeout`: how long one run may take. When omitted, the CronJob uses a 1h
  cluster deadline. `deployah run` does not apply that default; the CLI Job
  has no cluster deadline unless you set `timeout`.
- `suspend`: when `true`, the CronJob creates no Jobs until you set it back
  to `false`.

`deployah run cleanup dev` still creates a one-shot Job. That Job and the
CronJob can overlap. Fanout is an Indexed Job inside the CronJob template.

`Forbid` with no starting deadline defers the next tick instead of dropping
it: one catch-up run starts when the active run finishes. If a task overruns
its interval, lengthen the interval or split the work.

Setting `suspend` back to `false` schedules the missed run at once, not on
the next tick.

`@every` is a delay from CronJob creation time, so a redeploy shifts the
schedule. Use a cron expression for a fixed wall-clock time.

`ttlSecondsAfterFinished` deletes finished Jobs before
`successfulJobsHistoryLimit` can keep them, so `kubectl get jobs` can be
empty. Leave TTL unset if you want the history limits to apply.

## Fanout

Fanout runs several indexed copies of a task. Use a number as a shortcut
(count, one at a time) or an object. It works on `preDeploy`, `postDeploy`,
`manual`, and `schedule`.

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
