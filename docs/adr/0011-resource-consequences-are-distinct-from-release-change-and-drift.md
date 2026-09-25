# ADR-0011: Resource consequences are distinct from release change and drift

## Status

Accepted

## Context

Release change, Drift, and planned write consequences can disagree.
Collapsing them into one action invents Updates because a planned
Helm lifecycle transition exists, or Deletes of objects that no
longer exist.

The three states are defined in ADR-0005. This ADR owns the visible
resource consequence when the semantic plan includes the
corresponding write.

## Decision

Visible resource consequences use this vocabulary:

- Create: the relevant object does not exist Live, and the planned
  Helm lifecycle transition includes that create.
- Update: the relevant Live object exists, differs from Desired, and
  the planned Helm lifecycle transition includes that write.
- Delete: the object exists Live, leaves Desired/release state, and
  the planned Helm lifecycle transition includes that delete.
- Retain: the object leaves Desired/release state, but Helm lifecycle
  policy intentionally leaves it Live.

None is the absence of a consequence. It is not a stored action or a
required output row. Do not add Replace. Immutable-field rejection and
replacement feasibility are outside semantic planning (ADR-0004).

Human-readable Create output shows the complete intended manifest. An
Update starts from Live when the semantic plan includes that write.

Retain's primary case is Live `helm.sh/resource-policy: keep`. When
that policy suppresses deletion, report Retain, not Delete. If
Previous declared keep and Live no longer has it, that removal is
drift. If keep exists only in Live, that Live-only field itself is not
drift (ADR-0005); Helm still honors it, so a leaving resource is
Retain.

Representative combinations:

1. Previous differs from Desired, Live already equals Desired: release
   change exists, Drift may exist, no resource consequence. Do not
   invent an Update merely because the plan includes a Helm lifecycle
   transition.
2. Previous present, Desired absent, Live absent: release change and
   missing-resource drift, no Delete.
3. Previous present, Live absent, Desired unchanged: missing-resource
   drift only. Missing Live state alone is not a release change
   (ADR-0006). It does not decide whether a requested deployment
   runs (ADR-0016).
4. When the planned Helm lifecycle transition includes a write for a
   release change, and a Desired resource that existed in Previous is
   missing Live, show Create plus missing-resource drift.

Adoption is ADR-0012.

## Consequences

### Positive

- Operators can see Helm recording release intent even when Live
  already matches Desired.
- Keep policy is Retain: the object leaves the release and stays Live.

### Negative

- The plan does not show a recreate for a missing Live object
  unless there is a release change. A requested deploy of an
  existing release still follows Helm upgrade (ADR-0016).
