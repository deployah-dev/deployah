# ADR-0012: Semantic plan reports adoption candidates without taking ownership

## Status

Accepted

## Context

A Live object can already exist when it first enters Desired, with no
Previous record. Treating that as Create is wrong. Treating it as
automatic Helm ownership is also wrong. Resource consequences are
ADR-0011.

## Decision

Adoptable is an informational ownership condition. It is not Create,
Update, Delete, or Retain. It does not mean deployment will succeed.
It does not take ownership.

The relevant state is Previous absent, Live present, Desired present.
Determine the ordinary resource consequence independently
(ADR-0011): Live differs from Desired is Update; Live already equals
Desired is no resource consequence. Independently evaluate Helm
ownership metadata.

If the Live object is already correctly owned by this release, it is
not Adoptable merely because Previous is absent. If ownership is
missing or belongs elsewhere, surface Adoptable.

Do not predict that adoption is safe. Do not automatically take
ownership. Deployah's default Helm lifecycle uses TakeOwnership=false.
Explicit ownership takeover is future work: GitHub issue
[#142](https://github.com/deployah-dev/deployah/issues/142).

## Consequences

### Positive

- Operators can see that a Live object is entering the release
  footprint without Deployah taking it.

### Negative

- Default deploy still fails later if Helm requires ownership that
  Deployah did not take.
