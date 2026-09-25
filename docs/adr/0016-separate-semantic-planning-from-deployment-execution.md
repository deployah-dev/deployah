# ADR-0016: Separate semantic planning from deployment execution

## Status

Accepted

## Context

Deployah both inspects a deployment and carries one out. Inspection
describes release changes and their consequences. Execution carries
out a deployment the operator asked for. Those are different jobs.
Inspection must not decide whether that deployment runs.

## Decision

Planning and inspection are read-only. They describe release changes
and the resource and task consequences of those changes. Cluster
state informs drift and those consequences. It is not itself a
release change.

A requested deployment runs after validation and guards succeed,
including any volume preparation that must happen before Helm. It
then runs Helm. Planning results do not determine whether that
deployment is executed.

Helm chooses install or upgrade from whether the release already
exists. A release that does not exist yet is an install. An existing
release follows Helm's normal upgrade path.

An unchanged desired deployment may produce no planned release
change, while explicitly running deploy still executes that upgrade
path. Hooks follow the same path. The upgrade can record a new
revision even when the plan shows no release change.

## Consequences

### Positive

- Inspection can show no release change without blocking a later
  deploy.
- Existing releases follow Helm's upgrade path, including hooks and
  a new revision.

### Negative

- Deploy can record a new Helm revision when the plan showed no
  release change.
- Hook tasks can run on that upgrade even when their definitions
  did not change.
