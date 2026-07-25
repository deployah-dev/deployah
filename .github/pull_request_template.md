<!--
Thank you for contributing to Deployah.
Link related work with: Fixes #N  or  Refs #N
-->

## Summary

<!-- What changed and why. 1-3 bullets. -->

-

## Related

<!-- Optional. Delete if none. -->
Fixes #

## Test plan

<!-- How you verified. Check what applies. -->

- [ ] Unit tests added/updated
- [ ] Scenario under `scenarios/` (if behavior changes)
- [ ] `nix run .#lint` / pre-commit clean
- [ ] Manual smoke (command + expected result), if user-facing

## Labels

<!-- Release notes group merged PRs by these labels (.github/release.yml). -->

- [ ] One of: `kind/feature`, `kind/bug`, `kind/docs`, `kind/chore`
- [ ] Add `breaking-change` if this breaks existing CLI or config behavior
- [ ] Add `skip-changelog` for internal-only PRs that should not appear in notes

## Checklist

- [ ] Title is short and imperative (matches commit style)
- [ ] Docs / CLI help updated when user-facing (`README.md`, `docs/cli/`)
- [ ] No secrets or local-only paths in the diff
