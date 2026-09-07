# ADR-0002: Runtime environment resolution and inheritance

## Status

Accepted

## Context

Runtime env can come from environment-wide files, per-component files,
per-task files, and YAML `env:` maps. Tasks use `from:` to follow a
component. If inheritance copied file paths, a task would re-read (or
miss) files the parent already resolved, and two entities could share
the same map in memory.

Source order and output order are easy to confuse. Walking the
filesystem, ranging a Go map, or sorting override history would make
`deployah resolve` flicker and make reviews harder.

## Decision

Explicit vs implicit is discovery only. It selects which files feed one
layer, not how streams merge.

- Environment values: an explicit `environments.<env>.envFile`, or a
  merge of the four implicit environment files (later file wins).
- Entity values: an explicit entity `envFile`, else a clone of the
  parent component's already-resolved entity values when `from:` is
  set, else a merge of the four implicit entity files.
- FileValues = environment values, then entity values (entity wins on
  overlap).
- ExplicitValues = a clone of the parent's ExplicitValues, then this
  entity's YAML `env:`.
- `from:` names a component, not another task. Inheritance copies
  resolved maps, not paths and not Kubernetes objects. Task merge does
  not copy `env` or `envFile`.
- Missing implicit files are skipped. A missing explicit path is an
  error. Keys must be POSIX names.
- Source precedence follows the declared file lists. Emitted keys and
  provenance walk components, then tasks, each by name, then FileValues
  keys, then ExplicitValues keys (bytewise). Override history for a key
  stays in source order.
- Resolution runs eagerly once per top-level resolve, for every active
  entity, components before tasks. The resolver itself is stateless. Maps
  stored on the resolved spec are fresh and treated as read-only.

## Consequences

### Positive

- A task with `from:` sees the same entity file data the parent already
  resolved, without opening those files again.
- An explicit task `envFile` replaces only the entity layer. Environment
  files still apply.
- Resolve output and rendered keys are stable across runs.
- Downstream code cannot alias a parent's maps.

### Negative

- Changing a parent file requires a new resolve; nothing caches "already
  done" inside the resolver.
- Tasks cannot inherit from other tasks. Share a component, or repeat
  `envFile` / `env:`.
- Invalid keys fail every env-selecting command, including offline plan.
