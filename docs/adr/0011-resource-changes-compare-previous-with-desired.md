# ADR-0011: Resource Changes compare Previous with Desired

## Status

Accepted

## Context

A release edit, cluster drift, and a later Helm write are three
different facts. Collapsing them into one action makes the plan
describe Live, or promise a Kubernetes delete Helm may not perform.

The three states are defined in ADR-0005. This ADR owns Resource
Changes: the declarative difference between Previous and Desired.

## Decision

Resource Changes use this vocabulary:

- Create: Previous is absent and Desired is present.
- Update: both are present and the declared content differs.
- Delete: Previous is present and Desired is absent. The object
  leaves the Desired release state. This does not promise a
  Kubernetes DELETE. `helm.sh/resource-policy: keep` is execution
  behavior, not a planning action.

Do not add Replace. Do not add Retain. Immutable-field rejection,
write method, field manager, force-conflicts, and delete propagation
are outside the semantic plan (ADR-0004, ADR-0012).

None is the absence of a Resource Change. It is not a stored action.

Human-readable Create and Delete output show the declared snapshot.
An Update shows Previous to Desired.

Resource Changes preserve Helm's deploy/upgrade resource sequence.
Create and Update changes follow Helm's install/upgrade kind
ordering, keeping manifest order for equal kinds. Resources removed
during an upgrade are listed afterward in Previous release order,
matching Helm's originals.Difference(targets) deletion pass. Helm
uninstall is outside this semantic deploy plan. Deployah adds no
independent ordering policy of its own. It mirrors Helm's
target-first, deletion-afterward upgrade sequence and does not add
alphabetical, UI-specific, or additional action-based sorting.

Representative combinations:

1. Previous equals Desired, and Live differs: there is no Resource
   Change. Drift exists. Drift does not create a Resource Change.
2. Previous differs from Desired, and Live already equals Desired:
   the Resource Change is still Update. Drift may also exist. Do not
   drop the Update because Live already matches.
3. Previous present, Desired absent, Live absent: the Resource Change
   is Delete. Missing-resource drift may also exist. Delete does not
   require the object to exist Live.
4. A fresh install has no Previous. Each Desired resource is Create.
   A Live object that already occupies that identity is not Drift,
   and it does not turn the Create into something else (ADR-0012).

Adoption and ownership are ADR-0012. Drift is ADR-0005.

## Consequences

### Positive

- Operators see the release edit even when Live already matches
  Desired.
- Delete means the object leaves the Desired release, without
  claiming Helm will remove it.

### Negative

- A Delete can name an object that is already absent Live, or that
  Helm will keep because of resource policy.
- Live can differ from both Previous and Desired without changing
  the Resource Change.
