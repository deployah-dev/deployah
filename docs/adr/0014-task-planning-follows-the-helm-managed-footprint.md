# ADR-0014: Task planning follows the Helm-managed footprint

## Status

Accepted

## Context

Deployah tasks map onto Helm hooks, ordinary Helm resources, or a
one-time operation outside the release. Treating leftover hook Jobs as
declarative Drift, or listing a current manual task as a deploy
resource, invents a lifecycle Helm does not run. The lifecycle the
plan describes is ADR-0006. Whether a requested deployment runs is
ADR-0016. Resource consequences are ADR-0011.

## Decision

`preDeploy` and `postDeploy` definitions are Helm hooks. A change to
those definitions is a release change. On a Helm upgrade, unchanged
hook tasks still run, because Helm executes them as part of the
upgrade.

Scheduled tasks are ordinary Helm-managed resources, for example
CronJobs. A schedule task is a release change only when that
resource changes. The task type does not by itself make one.

A current manual task is outside the deploy semantic plan. It is not
shown as a deploy Task by itself, and has no release Resource entry
merely because it exists in the spec.

Jobs or other objects created by executing `preDeploy` and
`postDeploy` hooks are execution artifacts. Their existence, success,
failure, or leftover presence is not declarative Drift. They are not
a release change. Semantic planning models the hook definition, the
task-level action, and whether that hook is part of the planned
release change. A requested deploy follows Helm's hook rules for the
install or upgrade it runs (ADR-0016).

Changing phase between Helm-managed phases is a Task Update.

- schedule to preDeploy: previous CronJob is Resource Delete, new hook
  definition is Create, task is Update.
- preDeploy to schedule: previous hook definition leaves the release,
  new CronJob is Resource Create, task is Update.

Do not treat removal of an old hook definition as a Kubernetes Delete
of leftover runtime hook Jobs.

manual to a Helm-managed phase creates the new Helm-managed footprint.
A Helm-managed phase to manual removes the previous Helm-managed
footprint. The resulting manual task itself remains outside deploy
semantic planning.

## Consequences

### Positive

- Hook and schedule tasks are explained in Helm terms, not as a second
  controller.

### Negative

- Leftover hook Jobs are not shown as Drift, even when they remain
  Live after a failed hook.
