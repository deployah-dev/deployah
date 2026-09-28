# ADR-0012: Resource ownership and adoption are outside the semantic plan

## Status

Accepted

## Context

A Live object can already exist when it first enters Desired, with no
Previous record. Helm's default install uses TakeOwnership=false, so
that collision can fail at deploy time. Predicting whether adoption
would succeed pulls ownership feasibility into the semantic plan
(ADR-0004).

## Decision

Ownership and adoption are future pre-flight work. They are not
Resource Changes.

Previous absent and Desired present is Create, whether or not a Live
object already occupies that identity (ADR-0011). The semantic plan
does not report an adoption candidate, and it does not take
ownership.

Deployah's default Helm lifecycle uses TakeOwnership=false. Explicit
ownership takeover is future work: GitHub issue
[#142](https://github.com/deployah-dev/deployah/issues/142).

## Consequences

### Positive

- Create stays a statement of Desired entering the release, not a
  guess about who owns the Live object.

### Negative

- Default deploy still fails later if Helm requires ownership that
  Deployah did not take.
- The plan does not warn that a Live object already occupies a fresh
  install's identity.
