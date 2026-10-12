# Testing principles

This is the reference for writing, organizing, and reviewing Deployah tests.
It is for contributors, maintainers, reviewers, and coding agents.

Deployah is a command-line tool that deploys applications to Kubernetes.
Tests should follow that shape: configuration, planning, rendering, and what
the user sees on a cluster.

Words in bold capitals have a fixed meaning:

- **MUST**: a required project convention.
- **SHOULD**: the default. Depart from it when you can say why.
- **MAY**: optional. Use it when the situation calls for it.

Existing tests show current practice. For new and changed tests, follow this
page when an older test does otherwise. This page states the standard. It
does not list gaps in the tree.

## Philosophy

A test checks behavior a caller can observe: a return value, an error,
rendered output, a file on disk, or a change in the cluster. The test is an
executable description of that behavior.

Prefer the contract over the private steps that produce it. An unexported
function is worth a direct test when that function is itself a meaningful
internal contract. `releaseIntentChanged` is one of those functions.

A useful test is readable, focused on one behavior, deterministic,
independent of other tests, and specific when it fails.

Keep test code direct. A few repeated lines are better than a helper that
hides the action. Do not add a test whose only purpose is to raise a
coverage percentage. Do not add a testing framework.

## Testing levels

Test at the lowest level that gives enough confidence. A higher-level test
**MUST** cover a contract that the lower level cannot cover well.

### Unit tests

Unit tests are the default. Use them for business logic, configuration
parsing, validation, transforms, resource calculations, error handling, and
edge cases.

A unit test **SHOULD** be fast and deterministic. It **MUST NOT** need a
cluster or other external infrastructure.

[`internal/plan/release_intent_test.go`](../../internal/plan/release_intent_test.go)
is a unit test. It calls `releaseIntentChanged` with YAML from
`testdata/release_intent/` and checks a boolean or an error. That is the
right level: the question is how two manifests compare, and a cluster cannot
answer it more honestly.

Leave this level when the behavior only appears once several packages, Helm
rendering, or a real API server are involved.

### Integration tests

An integration test checks how parts of Deployah work together. Configuration
resolution, Helm rendering, and scenario processing belong here. A container
is not required.

The scenario suite lives under [`scenarios/`](../../scenarios/) and runs from
[`internal/testing`](../../internal/testing/integration_test.go) with the
`integration` build tag. A scenario directory holds `deployah.yaml` and, when
the case needs them, environment files, `error-config.yaml`, rendered
manifests, and an optional `e2e.yaml`.

Prefer a real integration when stand-in data would miss the interaction you
care about. Reuse the scenario tree for pipeline behavior. Keep a unit test
when the question is one function.

### End-to-end tests

An end-to-end (e2e) test checks user-visible behavior on a real Kubernetes
cluster. The suite uses [Kind](https://kind.sigs.k8s.io/), the `e2e` build
tag, and [`internal/e2e/`](../../internal/e2e/). `TestE2E` starts one cluster
and runs the cases as subtests.

Use an e2e test for installing an application, upgrading a release, resource
lifecycle, deployment tasks, reconciliation, Helm lifecycle on a cluster, and
failure or recovery that depends on a real API server.

`runDeployCreatesNamespace` in
[`internal/e2e/namespace_test.go`](../../internal/e2e/namespace_test.go)
copies the `basic-web-service` scenario into a workspace and runs
`deployah deploy`. The check that matters is that the namespace now exists
in the cluster. A unit test of flag parsing cannot show that. Do not use
this test as the pattern for an ordinary unit test.

The suite shares one cluster on purpose. Each case still isolates the
namespaces and objects it creates.

### Overlapping tests

Review tests that assert the same contract, and consider consolidating them.
Do not delete an overlapping test automatically.

Keep a test when it protects any of these:

- an independent contract
- a distinct failure mode
- confidence that only that testing level can give

A unit test of plan JSON and an e2e test that deploy creates a namespace are
different contracts. Two unit tests that only check the same sentinel are
candidates to merge, after you confirm that neither failure mode is lost.
A success test does not replace a failure test at the same level.

## Failure paths

A change that only has a success test is incomplete when failure is part of
the behavior.

Cover the failures that matter for the code you change:

- invalid input
- missing configuration
- a dependency that fails
- an error returned to the caller
- cancellation
- timeout
- boundary values
- invalid state
- cleanup that fails

Choose the critical paths. A long list of near-duplicate error strings adds
little.

`TestReleaseIntentChanged_NilHook` checks a real failure. A nil hook is
invalid even when both manifests are empty. The cases share one shape, so a
table is enough:

```go
func TestReleaseIntentChanged_NilHook(t *testing.T) {
    t.Parallel()
    hook := testHook("Job", "migrate", "migrate", "busybox", 1)
    tests := []struct {
        name         string
        prevHooks    []*v1.Hook
        desiredHooks []*v1.Hook
        want         string
    }{
        {name: "nil previous hook", prevHooks: []*v1.Hook{hook, nil}, want: "previous hook 2 is nil"},
        {name: "nil desired hook", desiredHooks: []*v1.Hook{nil}, want: "desired hook 1 is nil"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            _, err := releaseIntentChanged("", tt.prevHooks, "", tt.desiredHooks)
            require.Error(t, err)
            assert.ErrorContains(t, err, tt.want)
        })
    }
}
```

`hooksJSON` returns `fmt.Errorf("%s hook %d is nil", ...)`. There is no
sentinel. The message text is the contract, so `assert.ErrorContains` is the
right check. It does not prove error identity. When a sentinel exists, use
`ErrorIs` instead, as
`TestInstallApp_RejectsUnsupportedCSAHistory` does with
`ErrUnsupportedReleaseApplyMethod`.

Use another table row when the failure has the same setup and the same kind
of assertion. Use a separate test when setup or assertions diverge.

`TestValidate_Environment` puts success and failure in one table because
every row runs the validate command against a fixture directory and then
checks either success text or error text. Rows such as `invalid dotenv key`
and `missing explicit envFile` are the failure paths. The expected fragments
(`FOO-BAR`, `INVALID_ENV_KEY`) are the message the command returns. They are
message checks. They do not identify the error type. Profile resolution
tests identify the type with `require.ErrorAs` into `*spec.ResolutionError`.

Scenario directories whose names start with `invalid-`, `error-`, `fail-`,
`bad-`, or `malformed-` must ship an `error-config.yaml` whose
`expectedErrors` list names the specific failure. Those strings are authored
expectations. They are fixtures. `scenarios/error-task-cycle/error-config.yaml`
expects the substring `cycle`. That is the right place for a pipeline error
a unit test of one function would not load.

## Test file names

### Pair a test file with the production file

The default name is the production file plus `_test.go`. The name says which
behavior is under test.

```text
internal/plan/release_intent.go
internal/plan/release_intent_test.go
```

The same pairing is `validate.go` with `validate_test.go`, and `session.go`
with `session_test.go`.

Use this pairing when one production file owns the behavior. Do not invent a
filename that is unrelated to that behavior, and do not group unrelated
tests into an arbitrary file.

### Split by behavior

Split a test file when it becomes hard to navigate because it covers several
behaviors. Split on that behavior. File length can show that navigation is
getting hard. The deciding criterion is whether the behaviors are distinct.
Add a category file when it helps. Do not create a set of empty category
files in advance.

[`internal/plan/view/human.go`](../../internal/plan/view/human.go) is covered
by files named for the behavior:

```text
human_test.go
human_semantic_test.go
human_chart_crd_test.go
human_style_test.go
```

[`internal/plan/semantic_build.go`](../../internal/plan/semantic_build.go)
is covered by:

```text
semantic_build_test.go
semantic_build_crd_test.go
semantic_build_extras_test.go
```

The category word **MUST** describe the behavior. `human_chart_crd_test.go`
holds chart-CRD rendering tests. That name is doing its job.

Leave the tests in one file while one behavior is still easy to read.

Avoid names that do not describe a behavior:

```text
deploy_test1.go
deploy_extra_test.go
deploy_misc_test.go
common_test.go
general_test.go
miscellaneous_test.go
```

The `deploy_*` names above are illustrative. They are not files in this
repository.

### Behavior that spans several source files

Keep a clear link between production code and its tests. When forcing one
test file per source file would make the tests harder to follow, name the
test file for the behavior instead.

[`internal/cmd/plan_cli_test.go`](../../internal/cmd/plan_cli_test.go) tests
the plan command as a user runs it. That behavior is not owned by a single
source file, so a behavior name is a better fit than splitting assertions
across every file the command touches.

### Helper files

A package **MAY** use `helpers_test.go` for helpers that several behaviors
in that package share. Do not add that file to every package.

`readHookFixtures` stays in `release_intent_test.go` because only
release-intent tests load those hook files. A dedicated helpers file would
split one behavior across two files for no gain.

[`internal/plan/view/helpers_test.go`](../../internal/plan/view/helpers_test.go)
exists because many view tests build plans and compare golden files.

When a package has more than one helper group, a descriptive name such as
`deploy_helpers_test.go` **MAY** be used. That name is illustrative. Do not
split helpers into many files for its own sake.

## Test function names

The function name **MUST** say which behavior is checked. Use
`Test<Behavior>_<Scenario>` when the scenario suffix makes the failure
easier to recognize.

Names from this repository:

- `TestReleaseIntentChanged_NilHook`
- `TestValidate_Environment`
- `TestWriteJSON_TasksContract`
- `TestInstallApp_RejectsUnsupportedCSAHistory`

Skip the suffix when the function name already identifies the only case.
`TestWriteJSON_Deterministic` earns the suffix: the behavior under test is
that two writes match. `TestDeploy1` and `TestEverything` do not say what
failed.

A reader should understand a failure from the name before opening the body.

### Subtests

Give each table case a phrase for the scenario: `nil previous hook`,
`invalid dotenv key`, `missing explicit envFile`. Avoid `case1` and
`invalid1`.

The test name and the subtest name work as a pair.
`TestReleaseIntentChanged_NilHook/nil_previous_hook` identifies the failure.
`TestValidate_Environment/case1` does not.

## Helpers

A helper earns its place when it removes real repetition or makes a failure
easier to read. It **SHOULD** have one job, show its dependencies, and keep
tests isolated.

Judge a helper by the intent it adds, not by its length. A short helper is
appropriate when it names a testing contract, locks a shared default, or
removes setup that is easy to get wrong. A short helper is unnecessary when
it only forwards arguments to `t.TempDir`, `t.Cleanup`, `t.Setenv`,
`t.Context`, or a testify assertion. Call those directly. Do not wrap one
of them in a function named `setup` or `helper`.

Extract a helper when the repeated operation has real behavior: building a
value with several fields, loading fixtures, comparing output, or owning a
resource.

A helper **MUST NOT**:

- keep hidden mutable state that tests share
- depend on test order
- change global state quietly
- hide the assertion the test exists to make
- add a layer that makes the failure harder to trace

### Where helpers live

Use the narrowest owner.

**Next to the test.** Helpers for one test, or one behavior, stay in that
test file. `readOptionalFixture` and `readHookFixtures` are in
`release_intent_test.go`. They take `testing.TB`, call `tb.Helper()`, and
treat a missing hook directory as "no hooks". That policy belongs to
release-intent fixtures, so a shared package would be the wrong home.

**In the package.** Helpers used by several tests in one package stay in
that package. `helpers_test.go` is optional. `mustPlan` fills a header that
would otherwise be copied into every view test:

```go
func mustPlan(tb testing.TB, helmAction semantic.HelmAction, changes []semantic.ResourceChange) semantic.Plan {
    tb.Helper()
    return mustPlanWithTasks(tb, helmAction, changes, nil)
}
```

`mustPlan` is short because it delegates. The value is the contract behind
that call: `mustPlanWithTasks` fills the header the view tests share
(project `web`, environment `prod`, release `web`, namespace `prod`), and
`semantic.New` fails the test through `tb.Helper()` when the plan cannot be
built. Each test passes the Helm action and the resource changes. A
function whose body is only `t.TempDir()` adds none of that. Do not move a
package-local helper into `internal/testing/` only because several tests in
the same package call it.

**Shared across packages.** A helper that more than one package imports
**MAY** live under [`internal/testing/`](../../internal/testing/), in a
package with a specific job.
[`internal/testing/testpath`](../../internal/testing/testpath/path.go)
reads fixture paths and fails with the path in the message.
[`internal/testing/e2e`](../../internal/testing/e2e/doc.go) supports the
Kind suite. The `testing` package itself discovers scenarios and compares
scenario manifests.

Do not add a package named `common`, `testutil`, or `testhelpers` unless a
real boundary needs one.

Package-local helpers that only tests call belong in `_test.go` files. A
helper that other packages import belongs in an ordinary `.go` file, as
`testpath` does.

### `testing.TB` and `t.Helper`

Use `testing.TB` when the helper only needs facilities that tests,
benchmarks, and fuzz targets share. `mustPlan` and `testpath.ReadFile` do
this.

Use `*testing.T` when the helper calls `t.Run` or `t.Parallel`.

Do not take a testing parameter in a function that does not fail the test
or clean up. A pure builder returns a value.

A helper that reports a failure through the `testing` API **MUST** call
`Helper()` at the start, so the failure is attributed to the caller. That
includes setup, assertions, fixture loading, golden comparison, and resource
creation. A pure function does not call it.

### What a helper may assert

A helper **MAY** use `require` for a precondition. `mustPlan` fails the test
when `semantic.New` returns an error, because a plan that failed to build
cannot be rendered.

The test's main check stays in the test.
`TestWriteJSON_TasksContract` calls `mustPlanWithTasks`, then calls
`assertJSONGolden` and `validatePlanSchema` in the test body. A reader sees
both contracts without opening every helper.

### Resource ownership

A helper that acquires a resource **MUST** leave cleanup reliable, or it
**MUST** make ownership explicit for the caller.

Prefer a facility that already cleans up. `t.TempDir()` removes the
directory when the test finishes, including when the test fails. Do not
also register `t.Cleanup` to delete that same directory.

Register `t.Cleanup` for a resource the testing framework does not manage:
a process, a local server, a client, a background goroutine, a container,
or a Kubernetes object. Register it immediately after a successful acquire,
so a later failure still releases it.

Use `t.TempDir` for a writable copy of a fixture. Use `t.Cleanup` for a
chart directory created by `helm.PrepareChart`. `renderDesired` in the view
tests registers that cleanup and reports a removal error with `t.Errorf`.

If the helper returns a cleanup function, the caller owns it and registers
it. `TestRenderManifests_RejectsUnsupportedCSAHistory` does that:

```go
_, cleanup, err := c.RenderManifests(t.Context(), resolved, nil, nil)
if cleanup != nil {
    t.Cleanup(cleanup)
}
```

Do not build a cleanup framework. Use `TestMain` only when process-wide
state cannot be isolated in one test.
[`testenv.RunWithIsolatedKubeconfig`](../../internal/testing/testenv/kubeconfig.go)
is that case: it points `HOME` and client-go's recommended kubeconfig paths
at a temporary directory for the whole package. Do not add `TestMain` only
to share ordinary setup.

A shared helper with real behavior **SHOULD** have tests of its own. Do not
test a trivial wrapper only to raise coverage.

## Fixtures

A fixture is data you write and maintain as a known input, a known
dependency state, or an authored expectation. Typical fixtures are Deployah
configuration, Kubernetes manifests used as input, Helm values, environment
files, invalid configuration, a previous deployment state, and a static API
response.

Use a fixture when:

1. The input is substantial structured data.
2. Inline data would be harder to read than the file.
3. Several tests share the data.
4. A realistic configuration shape matters.
5. The case needs several files.
6. The test needs a reproducible directory.
7. You are stating an invalid configuration on purpose.

Skip a fixture for a boolean, a short string, a small struct, or a one-line
error. Keep those values in the test. A fixture is not a way to make a
function shorter.

`TestValidate_Environment` runs the validate command against directories
under [`internal/cmd/validate/testdata/`](../../internal/cmd/validate/testdata/):

```text
testdata/runtime-env-valid/deployah.yaml
testdata/runtime-env-valid/runtime.env
testdata/runtime-env-missing/deployah.yaml
testdata/runtime-env-bad-key/deployah.yaml
testdata/runtime-env-bad-key/runtime.env
```

The command reads those files, so the directory layout is part of the
contract. The test passes `testpath.Dir` as the working directory. The
directories are read-only, and the subtests call `t.Parallel()`.

The success text `"Manifest and environment valid"` stays in the table. It
is one short string. A file would only add a hop.

Put package data in that package's `testdata/` directory. Put pipeline
fixtures under `scenarios/<name>/`: `deployah.yaml`, dotenv files,
`deployah.platform.yaml`, `.deployah/` extras, and `e2e.yaml`. Do not move
every fixture into one global directory. Ownership follows the test that
needs the data.

Name the directory for the case: `runtime-env-missing`, `error-task-cycle`.
Avoid `fixture1` and `test-case-4`. The name should explain the scenario
before you open the files.

Keep a fixture small enough to show the behavior and realistic enough that
the test still means something. Do not copy a full production configuration
when a few fields would do. Do not duplicate a large fixture that already
exists. Prefer `testpath.Dir` and `testpath.ReadFile` over a new loader.

Tests **MUST NOT** modify source-controlled fixtures during a normal run.
Copy into a directory from `t.TempDir()` when the test needs to write.
Read-only fixtures **MAY** be shared by parallel tests. Writable fixture
state **MUST** be isolated per test.

## Golden files

A golden file is an approved baseline of deterministic output that the test
generated. The test compares new output with that baseline. Use one when the
whole output is a contract: CLI text, rendered manifests, a deployment plan,
or a large JSON or YAML document.

A fixture is authored. A golden file is generated and then approved. A
scenario may use both. A name ending in `.golden` does not decide which one
you have.

`TestReleaseIntentChanged` reads `result.golden` and `error.golden` under
`testdata/release_intent/`. Those files contain the words `changed` or
`unchanged`, or a short error string that a person wrote. The test does not
generate them, and it does not rewrite them. They are fixtures. Do not add
`-update` support because the suffix looks like a snapshot.

### When a golden file helps

Consider a golden file when:

1. The output is substantial.
2. The whole output is a meaningful contract.
3. The output is deterministic.
4. An exact or semantic comparison catches real regressions.
5. An inline expectation would bury the test.
6. Reviewers should see the output diff.

Plan text and JSON in
`internal/plan/view/testdata/golden/*.golden` are this case. Rendered
scenario manifests under `scenarios/<name>/expected/` and
`expected-<env>/` are this case too. Those files are YAML named
`<kind>-<name>.yaml`, for example `deployment-web.yaml`. The extension can
follow the format. The update flag does not.

Skip a golden file for a boolean, a number, a short error, a few struct
fields, or a short slice. Use `assert.Equal`, `assert.True`,
`require.ErrorIs`, or `require.ErrorAs`.

### Decision guide

| Situation | Example | Use |
| --- | --- | --- |
| Simple expected value | A function returns a boolean | Inline assertion. No fixture and no golden file. |
| Small structured input | A few fields on a config struct | Inline data, unless the test is about files on disk. |
| Large configuration input | A realistic Deployah config with several components | Fixture. |
| Several related files | `deployah.yaml`, an env file, and component config | A fixture directory under `testdata/` or `scenarios/`. |
| Large deterministic output | A full deployment plan or rendered manifests | A golden file. Regenerate it with `-update`. |
| Critical semantic invariant | A task stays visible when no Kubernetes resources change | An explicit assertion. Add a golden file only when the full rendering is also a contract. |
| Expected error | Invalid config returns a specific failure | `ErrorIs` or `ErrorAs` for identity or type. `ErrorContains` only when the message is the contract. A large structured report may be a golden file. A short authored string is a fixture, even if the name ends in `.golden`. |
| Full deployment scenario | One config produces several manifests | Fixture input plus generated golden manifests, when full comparison is the contract. |
| Nondeterministic output | Timestamps, random ids, temp paths, or secrets | Explicit assertions, or intentional normalization. Do not snapshot unstable output. |

### Comparison

Use exact comparison when formatting, order, or whitespace is part of the
contract. `assertGolden` compares the full string:

```go
func assertGolden(t *testing.T, name, got string) {
    t.Helper()
    require.Equal(t, string(readGolden(t, name, got)), got)
}
```

Use semantic JSON comparison when structure matters and insignificant
formatting does not. `assertJSONGolden` uses `require.JSONEq`:

```go
func assertJSONGolden(t *testing.T, name, got string) {
    t.Helper()
    require.JSONEq(t, string(readGolden(t, name, got)), got)
}
```

`require.Equal` fails when whitespace or key order changes.
`require.JSONEq` accepts that difference and still fails when a value
changes. Pick the one that matches what users depend on. Do not normalize
away a property the test is supposed to lock.

Scenario manifests are marshaled with sorted map keys, then compared as
text. `maskNondeterministicFields` replaces `tls.crt` and `tls.key` on a
freshly generated `kubernetes.io/tls` Secret so two offline renders of the
same chart can match. Other fields stay in the comparison.

Golden output **MUST** be deterministic. Keep out random identifiers, the
current time, unstable map order, ephemeral Kubernetes metadata, temporary
paths, machine-specific configuration, and secrets. Normalization is a
choice about the contract. It is not a way to hide a regression.

### Golden files and other assertions

A golden file does not replace an explicit check of a critical invariant.
Combine them when the extra assertion names a different contract. Do not
repeat every golden field in more assertions.

`TestWriteJSON_TasksContract` builds a plan that has a migrate task and no
resource changes, compares the document with `json_tasks.golden`, and checks
the JSON against the plan schema:

```go
require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
assertJSONGolden(t, "json_tasks", buf.String())
validatePlanSchema(t, buf.Bytes())
```

The golden file locks the rendered document. `validatePlanSchema` locks a
different contract: the document matches the published schema. Skip the
second check when it only restates a field the golden file already locks.

`TestWriteJSON_Deterministic` also writes the document twice and compares
the two strings before the golden check. The equality of the two writes is
the determinism contract. The golden file is the approved bytes.

### Updating golden files

Every regenerable golden snapshot uses the flag `-update`. Do not add
`-update-golden`, `-update-snapshots`, `-rewrite`, `UPDATE_GOLDEN`, or
`UPDATE_SNAPSHOTS`.

Go runs each test package as its own binary. The package that owns the
goldens registers `-update` itself. One shared flag variable across packages
is not required. Do not register `-update` twice in the same binary.

`go test ./... -update` is not a supported command. A package that does not
register the flag rejects it.

**Normal mode**, without `-update`:

- Golden files **MUST NOT** be modified.
- A missing required golden file **MUST** fail the test.
- A difference **MUST** fail the test.
- The test **MUST NOT** skip the comparison, rewrite the file, or accept
  the new output.

**Update mode**, with `-update`:

- The run **MAY** create or replace the regenerable baselines for the tests
  that ran.
- The new content comes from actual test output.
- Paths are deterministic.
- A write error fails the test.
- Unrelated files stay unchanged.
- A baseline those tests no longer produce should not remain as if it were
  still approved.
- Parallel tests **MUST NOT** write the same path. If two tests share one
  golden path, they should be one contract. Do not depend on which test
  runs first.

Commands for the packages that support the flag:

```sh
go test ./internal/plan/view -update

go test ./internal/plan/view \
  -run '^TestWriteHuman' \
  -update

go test -tags integration ./internal/testing -update

go test -tags integration ./internal/testing \
  -run '^TestDeployahIntegration$' \
  -update
```

View tests read and write `testdata/golden/<name>.golden`. Pass `-tags
integration` for scenario manifests. Without that tag the scenario test is
not compiled, and `-update` does not regenerate `scenarios/`.

Workflow:

1. Make the production or test change.
2. Run the relevant tests without `-update`.
3. Confirm each failure is a change you intended.
4. Re-run that package, or those tests, with `-update`.
5. Review every golden diff.
6. Confirm the diff matches the intended behavior.
7. Run the same tests again without `-update`.
8. Run the broader suite you would run for the change.
9. Commit the golden changes with the code change.

Review generated goldens the same way you review other source. CI runs
tests in normal mode and **MUST NOT** pass `-update`.

`-e2e.scaffold` writes an `e2e.yaml` skeleton from a live cluster. It does
not regenerate golden files.

## Table-driven tests

Use a table when several cases share the same inputs, outputs, and
assertions. Name every case. Use `t.Run` so a failure names the case and so
the case can run in parallel.

`TestReleaseIntentChanged_NilHook` is the right size: two cases, one
assertion shape. `TestValidate_Environment` is a larger table of the same
command against different fixture directories. The `wantErr` branch is still
one shape.

Leave a single case as its own test. Split the table when a row needs
different setup or a different kind of assertion. Do not share mutable
values across rows. Keep the table as data. Do not hide the assertion behind
a function field on the row.

## Assertions

Deployah tests use [testify](https://github.com/stretchr/testify).

Use `require` when a failure means the rest of the test cannot run. Use
`assert` for checks that stand on their own.

Prefer a specific assertion. `assert.Equal` and `assert.True` say what was
compared.

For errors, pick the assertion that matches the contract:

- Use `ErrorIs` when a sentinel identifies the error, including through
  wrapping. `TestInstallApp_RejectsUnsupportedCSAHistory` does this:

  ```go
  require.Error(t, err)
  assert.ErrorIs(t, err, ErrUnsupportedReleaseApplyMethod)
  ```

- Use `ErrorAs` when the contract is the error's type or its fields.
  `TestResolveProfileNames` does this, then checks the code on the concrete
  value:

  ```go
  var re *spec.ResolutionError
  require.ErrorAs(t, err, &re)
  assert.Equal(t, tt.errCode, re.Code)
  ```

- Use `ErrorContains` only when the message text is part of the contract.
  The nil-hook text is that kind of contract, because the function returns
  a formatted error and no sentinel. A substring match does not show which
  error you received. An unrelated error can contain the same words.

Call `require.Error` or `require.NoError` when later lines depend on that
outcome. `err != nil` alone is not enough when identity, type, or message
text matters.

Skip assertion messages that repeat what testify already prints. Add a
message when the surrounding values would otherwise be invisible.
`TestValidate_Environment` prints stderr when success was required and the
command failed.

Keep the main behavioral assertion in the test function.

## Isolation

A test **MUST NOT** depend on another test or on execution order. Each test
owns the state it mutates.

Avoid shared mutable globals, a writable fixture directory shared by tests,
a fixed temporary path, and process-wide configuration that another test can
see.

A test **SHOULD** pass alone and as part of the suite. The Nix test runners
pass `-shuffle=on`. When a shuffled run fails, reuse the seed it prints.

Shuffling test order is separate from random values inside a test. Use a
reproducible seed when randomness changes inputs or expected behavior. A
random resource name may stay nondeterministic. See
[Determinism](#determinism).

## Parallel execution

`t.Parallel()` is the default. Call it as the first line of the test and as
the first line of each subtest.

Read-only fixtures may be shared. `TestValidate_Environment` does this: the
command only reads the fixture directory, so the subtests run in parallel
against the same trees.

Skip `t.Parallel()` only for a concrete reason:

- The test, a parent test, or a helper calls `t.Setenv` or `t.Chdir`. Both
  change process-wide state. Go rejects them in a parallel test and under a
  parallel parent.
- The test depends on other shared mutable process state.
- Under `-update`, the test would write a golden path that another parallel
  test also writes.
- The test shares an external resource whose lifecycle is intentionally
  process-wide. `TestE2E` shares one Kind cluster and stays serial for that
  reason. Each case still isolates the namespaces and objects it creates.

Scenario runs call `t.Chdir` inside `RunScenarioTest`. They stay serial
because of that call.

A helper **MUST NOT** hide `t.Setenv`, `t.Chdir`, or another process-wide
change. If it needs one, the calling test cannot be parallel, and that
restriction should be obvious at the call site.

## Determinism

A test **SHOULD** produce the same result on every run.

When randomness changes inputs, expected results, or assertions, use a
reproducible seed, or keep enough information to rerun the failure.
Randomized fixtures and property-style checks belong here.

A random identifier **MAY** stay nondeterministic when its only job is to
isolate a resource. `runDeployCreatesNamespace` reads `crypto/rand` and
builds a namespace name, `e2e-ns-` plus random hex, so runs do not collide.
The test checks that this namespace was missing and that deploy created it.
The exact characters are not an expected value. An identifier used this way
must leave assertions and golden output stable, keep the resource isolated,
and still be cleaned up. Normalize it before it enters a golden file.

Avoid sleeps that hope a condition becomes true, assumptions about the wall
clock, map order that the contract treats as stable, and shared mutable
state.

For Kubernetes readiness, Helm, container startup, reconciliation, and
background work, wait for a condition with a bounded timeout. Do not sleep
for a fixed interval and hope.

## Context and cleanup

Pass a context into work that can block. Prefer `t.Context()`.

`t.Context()` is canceled before `t.Cleanup` functions run. Cleanup that
still needs a context uses its own bounded context.
`runDeployCreatesNamespace` drives deploy with the test context, then
cleans up with `context.WithTimeout(context.Background(), ...)`.

Use `t.Cleanup`, `t.TempDir`, `t.Context`, `t.Setenv`, and `t.Chdir` from
the `testing` package. Respect the parallel limits on `t.Setenv` and
`t.Chdir`. Cleanup **MUST** run when the test fails. Follow the ownership
rules in [Resource ownership](#resource-ownership). Do not add a cleanup
framework.

## Test doubles

Prefer a real dependency when it is practical and deterministic.

Use a fake or a mock when you need a dependency failure, a specific error,
controlled timing, or to avoid a side effect you cannot undo. Keep the
double next to the tests that use it. Do not create a mock for every
interface.

Kubernetes fake clientsets, as used by the deploy resize tests and
`internal/readiness`, do not reproduce API-server admission, defaulting, or
validation. Cover those contracts with scenario tests or e2e tests.

Use `net/http/httptest` when the behavior under test is HTTP. Do not bind
a fixed port for that. Do not add an HTTP test when the code does not
speak HTTP.

## Package boundary

Same-package tests are the default. `release_intent_test.go` is
`package plan`, which lets it call `releaseIntentChanged` directly. Use
that form unless the external boundary is the contract you need to lock.

An external test package (`package foo_test`) is appropriate when the test
should import the package the way another module would.
`TestValidate_Environment` is `package validate_test` because it drives the
public CLI.

A directory **MAY** contain both internal and external test files. That mix
is not a defect by itself. `internal/plan/view` has `package view` tests and
`package view_test` tests. Do not move tests from one form to the other as
a style cleanup. A later review can decide, file by file, whether the
external package is earning its boundary.

## Running tests

The Nix flake is the interface CI uses.

```sh
nix run .#test-unit
nix run .#test-integration
DEPLOYAH_E2E_FORCE=1 nix run .#test-e2e
```

`test-unit` runs the unit suite with the race detector and `-shuffle=on`.
The app passes `-tags=!integration`. Tests that require the `integration`
or `e2e` build tag are not part of this run. Unit tests themselves do not
need a special build tag.

`test-integration` runs `./internal/testing` with `-tags=integration`, the
race detector, and `-shuffle=on`.

`test-e2e` runs `./internal/e2e` with `-tags=e2e`, `-parallel=4`, and a 30
minute timeout. The race detector is off because the work is a live
cluster. `DEPLOYAH_E2E_FORCE=1` replaces an existing Kind cluster named
`deployah`. The suite checks for a container engine with `docker info`.
Without one, a local run skips. When `CI=true`, a missing engine fails the
test.

`go test ./...` with no tags skips integration and e2e files, because those
files require their build tags.

Do not add a new build tag. Use unit tests, the `integration` tag, or the
`e2e` tag.

To generate an `e2e.yaml` skeleton from a live cluster, replace
`<scenario-name>` with the scenario directory name. That directory name is
the subtest. Fixtures run under `parallel` or `sequential`, so the pattern
matches either group:

```sh
go test -v -tags e2e ./internal/e2e \
  -run 'TestE2E/TestE2EFixtures/(parallel|sequential)/<scenario-name>' \
  -e2e.scaffold
```

`basic-web-service` is one such name. Pass `-v`. The skeleton is logged
with `t.Logf`, and `go test` hides logs when the test passes.

Scaffold copies the scenario into a temporary workspace
(`NewScenarioWorkspace`), lists live objects in that case's namespace, and
builds the skeleton from them. It does not edit the source-controlled
scenario. When `e2e.yaml` is already in the workspace, which is the usual
case, the skeleton is printed and the file is left unchanged. When the file
is absent, the skeleton is written only inside that temporary copy. Review
the output, then copy it into the scenario fixture yourself if you want to
keep it.

`-e2e.scaffold` is not `-update`. `-update` regenerates golden snapshots.

## Failure diagnostics

A failure should name the behavior, the expected result, the actual result,
and the scenario. Subtest names carry much of that.

When a golden file mismatches, the failure should include the file path.
View tests fail with the path when the golden file is missing, and they
print the output they got. Scenario comparison names the stale golden file
and says to re-run with `-update`.

When external infrastructure fails, include the context needed to reproduce
it: the scenario directory, the namespace, and the command's stderr.

Vague text such as `values differ` without the input is not enough. Name
the function and the case when testify's default output does not.

## Coverage

Write tests for behavior. Deployment correctness, configuration validation,
resource lifecycle, error handling, and important edge cases come first.
Do not add a test only to move a percentage.

This page does not set a coverage number. CI applies the settings in
[`codecov.yml`](../../codecov.yml). If that policy changes, change it
there.

## Benchmarks and examples

Benchmarks are not required. Add one when you have a specific performance
question to measure. Most packages in this CLI do not need one.

`Example` functions are not required for every exported symbol in an
internal package. Add one when the example is the clearest way to show how
to call the API. `internal/spec/example_test.go` and
`internal/localkube/example_test.go` do this for a few constructors. Follow
that pattern only when it helps a reader.

## Race detection

Unit and integration runs use the Go race detector. E2e runs do not. A
clean race run catches many shared-memory mistakes. It does not prove the
program has no concurrency bugs. Use `nix run .#test-unit` or
`nix run .#test-integration` for changes that touch shared state.
