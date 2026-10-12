# Contributing to Deployah

Thank you for considering a contribution. This guide covers how to report
issues, propose changes, and submit pull requests.

By participating, you agree to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Where to start

| Kind of contribution | Where |
| --- | --- |
| Questions, early ideas, showcases | [GitHub Discussions](https://github.com/deployah-dev/deployah/discussions) |
| Bugs, feature requests, design proposals | [GitHub Issues](https://github.com/deployah-dev/deployah/issues) |
| Security vulnerabilities | [SECURITY.md](SECURITY.md) (private report only) |
| Code, docs, or tests | Pull request against `main` |

Search existing issues and discussions before opening a new one. Use the issue
templates when they fit.

## Development setup

The Nix flake is the main dev and CI interface. With
[direnv](https://direnv.net/) (the `.envrc` uses `use flake`), tools load when
you enter the repo.

```sh
nix develop
```

Format, lint, and tidy:

```sh
nix run .#fmt
nix run .#lint
nix run .#lint-md
nix run .#tidy
```

Build and run:

```sh
nix build
nix run . -- --help
```

More detail lives in the [Development](README.md#development) section of the
README.

## Tests

When you add or change behavior, add or update tests in the same change.
Test at the lowest level that gives a distinct kind of confidence: a unit
test for focused logic, a scenario test under `scenarios/` when the behavior
spans the deploy pipeline, and an end-to-end test when the change needs a
real cluster.

Names, helpers, fixtures, golden files, parallelism, and failure paths are
defined in the [testing principles](docs/principles/testing.md).

```sh
nix run .#test-unit
nix run .#test-integration
# Skips locally when `docker info` fails. CI does not.
DEPLOYAH_E2E_FORCE=1 nix run .#test-e2e
```

`test-unit` and `test-integration` run the race detector and shuffle test
order. `test-e2e` does not use the race detector. The subject is a live
cluster. `go test ./...` with no tags skips tests that require the
`integration` or `e2e` build tag.

Regenerate golden files only after you have confirmed the new output is
intended. Review the diff, then run the tests again without `-update`. Do
not pass `-update` in CI. `go test ./... -update` is not supported.

```sh
go test ./internal/plan/view -update
go test ./internal/plan/view -run '^TestWriteHuman' -update
go test -tags integration ./internal/testing -update
go test -tags integration ./internal/testing \
  -run '^TestDeployahIntegration$' \
  -update
```

The `integration` tag is required for scenario manifests. Without it those
tests are not compiled, and nothing under `scenarios/` is regenerated.

To generate an `e2e.yaml` skeleton, replace `<scenario-name>` with the
scenario directory name. The scenario runs under `parallel` or
`sequential`:

```sh
go test -v -tags e2e ./internal/e2e \
  -run 'TestE2E/TestE2EFixtures/(parallel|sequential)/<scenario-name>' \
  -e2e.scaffold
```

Pass `-v` so the logged skeleton is visible. Scaffold uses a temporary copy
of the scenario and does not edit the source-controlled files. When
`e2e.yaml` is already in that copy, the skeleton is printed and the file is
left unchanged. When it is absent, the skeleton is written only in the
temporary copy. Review the output and copy it into the scenario yourself if
you want to keep it. `-e2e.scaffold` does not regenerate golden files.

Before you open a pull request, make sure lint and the tests you touched are
clean. CI runs flake validation, lint/fmt/tidy, unit, integration, and e2e on
every pull request and push to `main`.

## Pull requests

1. Fork the repository and create a branch from `main`.
2. Make your changes with clear, focused commits.
3. Update docs or CLI help when the change is user-facing (`README.md`,
   `docs/cli/`).
4. Open a pull request and fill in the template (summary, test plan, labels).
5. Link related work with `Fixes #N` or `Refs #N`.

Pull request titles should be short and imperative (for example,
`Add plan dry-run flag`). Apply one of `kind/feature`, `kind/bug`,
`kind/docs`, or `kind/chore`. Add `breaking-change` when you break existing
CLI or config behavior. Add `skip-changelog` for internal-only work that
should not appear in release notes.

## Commit messages

Use a short imperative subject. Explain why in the body when the change is not
obvious from the subject alone.

## AI-assisted contributions

AI tools are welcome if you disclose them and stay accountable for the result.

- You must understand and review every change you submit.
- Say in the pull request description that AI assistance was used.
- Optionally add an `Assisted-by: <tool>/<model>` trailer on commits.
- Do not use AI to generate replies in review discussions.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
