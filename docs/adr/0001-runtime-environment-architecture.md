# ADR-0001: Runtime environment architecture

## Status

Accepted

## Context

A spec can rewrite itself with `${...}` placeholders and can also set the
environment of a running container. Those look similar (both use names like
`TAG` and files named `.env`), but they answer different questions. Mixing
them made dotenv files fill image tags, hid process-only keys such as
`DPY_VAR_*` from the pod, and left Helm and the CLI to invent their own
merge rules.

Container env also has two shapes people expect to keep: a file of keys
(ConfigMap data) and a YAML map (`env:`). Flattening those into one map
would hide which source wins and would fight Kubernetes' own `env` vs
`envFrom` rule.

## Decision

Substitution and runtime container env are separate systems.

- Substitution reads `environments.*.variables`, then process `DPY_VAR_*`
  (prefix stripped). Dotenv files are not on this path.
- Runtime env has two streams that are never flattened: FileValues (dotenv
  layers) and ExplicitValues (YAML `env:`).
- The spec/resolution layer owns discovery, merge, `from:` inheritance, key
  validation, and provenance. Helm and Kubernetes code only represent the
  already-resolved maps.
- The resolved runtime model stays semantic. It does not carry ConfigMap
  names, hook weights, or Kubernetes object types.

## Consequences

### Positive

- `${TAG}` and a dotenv key `DPY_VAR_TAG` cannot surprise each other.
- Process env such as `HOME` or `CI` never enters a pod.
- Helm, plan, and `deployah run` cannot drift by re-reading files.
- FileValues and ExplicitValues can be delivered the way Kubernetes
  expects (`envFrom` vs `env:`).

### Negative

- Operators who used dotenv `DPY_VAR_*` for substitution must move those
  keys to `variables` or the process environment.
- Two maps appear in resolve output instead of one combined list.
