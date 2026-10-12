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

Follow this page for new and changed tests. How to run the suites, regenerate
golden files, and generate an end-to-end fixture skeleton is in the
[contributor guide](../../CONTRIBUTING.md#tests).

Examples in this page are illustrative unless a path or flag is named as a
project convention. Illustrative names are not Deployah APIs.

## Philosophy

A test checks behavior a caller can observe: a return value, an error,
rendered output, a file on disk, or a change in the cluster. The test is an
executable description of that behavior.

Prefer the contract over the private steps that produce it. An unexported
function is worth a direct test when that function is itself a meaningful
internal contract, such as "did these two manifests change?"

A useful test is readable, focused on one behavior, deterministic,
independent of other tests, and specific when it fails.

Keep test code direct. A few repeated lines are better than a helper that
hides the action. Do not add a test whose only purpose is to raise a
coverage percentage. Do not add a testing framework.

## Testing levels

Test at the lowest level that gives enough confidence. A higher-level test
**MUST** cover a contract that the lower level cannot cover well.

Unit tests have no build tag. Scenario tests use the `integration` tag.
Cluster tests use the `e2e` tag. Do not add another tag.

### Unit tests

Unit tests are the default. Use them for business logic, configuration
parsing, validation, transforms, resource calculations, error handling, and
edge cases.

A unit test **SHOULD** be fast and deterministic. It **MUST NOT** need a
cluster or other external infrastructure.

Comparing two rendered manifests is a unit-test question. A cluster cannot
answer it more honestly. Leave this level when the behavior appears only
once several packages, Helm rendering, or a real API server are involved.

### Integration tests

An integration test checks how parts of Deployah work together. Configuration
resolution, Helm rendering, and scenario processing belong here. A container
is not required.

Pipeline fixtures live under `scenarios/<name>/`. A scenario directory holds
`deployah.yaml` and, when the case needs them, environment files,
`deployah.platform.yaml`, files under `.deployah/`, rendered manifests, and
an optional `e2e.yaml`.

Prefer a real integration when stand-in data would miss the interaction you
care about. Reuse the scenario tree for pipeline behavior. Keep a unit test
when the question is one function.

A scenario whose name starts with `invalid-`, `error-`, `fail-`, `bad-`, or
`malformed-` **MUST** include `error-config.yaml`. That file lists the error
text the scenario is expected to produce. Those strings are authored
fixtures, not regenerable golden files.

### End-to-end tests

An end-to-end (e2e) test checks user-visible behavior on a real Kubernetes
cluster. Use one for installing an application, upgrading a release, resource
lifecycle, deployment tasks, reconciliation, Helm lifecycle on a cluster, and
failure or recovery that depends on a real API server.

Creating a namespace is an e2e question: the contract is that the namespace
exists in the cluster afterward. A unit test of flag parsing cannot show
that. Do not use an e2e test as the pattern for an ordinary unit test.

The e2e suite shares one cluster for the run. The test that owns the cluster
stays serial. Each case still isolates the namespaces and objects it creates.

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

When several failures share one setup and one kind of assertion, a table is
enough. This snippet is illustrative:

```go
func TestValidate_MissingField(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name string
        env  string
        want string
    }{
        {name: "missing environment", env: "", want: "environment is required"},
        {name: "unknown environment", env: "staging", want: "unknown environment"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            err := validateEnv(tt.env)
            require.Error(t, err)
            assert.ErrorContains(t, err, tt.want)
        })
    }
}
```

`ErrorContains` fits here because the message text is the contract and there
is no sentinel. It does not prove which error you received. When a sentinel
exists, use `ErrorIs`. Use a separate test when one case needs a fixture
directory, a different dependency, or a different kind of assertion.

A pipeline failure that only appears while loading a full scenario belongs
in `scenarios/`, with the expected text in `error-config.yaml`. A unit test
of one function is the wrong level for that.

## Test file names

### Pair a test file with the production file

The default name is the production file plus `_test.go`. The name says which
behavior is under test.

```text
deploy.go
deploy_test.go
```

Use this pairing when one production file owns the behavior. Do not invent a
filename that is unrelated to that behavior, and do not group unrelated
tests into an arbitrary file.

### Split by behavior

Split a test file when it becomes hard to navigate because it covers several
behaviors. Split on that behavior. File length can show that navigation is
getting hard. The deciding criterion is whether the behaviors are distinct.
Add a category file when it helps. Do not create a set of empty category
files in advance.

```text
plan_test.go
plan_validation_test.go
plan_lifecycle_test.go
```

The category word **MUST** describe the behavior. `plan_validation_test.go`
holds validation tests for plan behavior. Leave the tests in one file while
one behavior is still easy to read.

Avoid names that do not describe a behavior: `deploy_test1.go`,
`deploy_extra_test.go`, `deploy_misc_test.go`, `common_test.go`,
`general_test.go`.

### Behavior that spans several source files

Keep a clear link between production code and its tests. When forcing one
test file per source file would make the tests harder to follow, name the
test file for the behavior instead. A command implemented across several
files may use a name such as `plan_cli_test.go`.

### Helper files

A package **MAY** use `helpers_test.go` for helpers that several behaviors
in that package share. Do not add that file to every package.

Keep a helper in the test file that uses it when only that behavior needs
it. A helpers file would only split one behavior in two.

When a package has more than one helper group, a descriptive name such as
`deploy_helpers_test.go` **MAY** be used. Do not split helpers into many
files for its own sake.

## Test function names

The function name **MUST** say which behavior is checked. Use
`Test<Behavior>_<Scenario>` when the scenario suffix makes the failure
easier to recognize.

```text
TestDeploy_InvalidConfig
TestResolve_MissingEnvironment
TestPlan_NoResourceChanges
```

Skip the suffix when the function name already identifies the only case.
`TestDeploy1` and `TestEverything` do not say what failed.

A reader should understand a failure from the name before opening the body.

### Subtests

Give each table case a phrase for the scenario: `missing environment`,
`unknown environment`, `context canceled`. Avoid `case1` and `invalid1`.

The test name and the subtest name work as a pair.
`TestValidate_MissingField/missing_environment` identifies the failure.
`TestValidate_MissingField/case1` does not.

## Helpers

A helper earns its place when it removes real repetition or makes a failure
easier to read. It **SHOULD** have one job, show its dependencies, and keep
tests isolated.

Judge a helper by the intent it adds, not by its length. A short helper is
appropriate when it names a testing contract, locks a shared default, or
removes setup that is easy to get wrong. A short helper is unnecessary when
it only forwards arguments to `t.TempDir`, `t.Cleanup`, `t.Setenv`,
`t.Context`, or a testify assertion. Call those directly.

Extract a helper when the repeated operation has real behavior: building a
value with several fields, loading fixtures, comparing output, or owning a
resource.

A helper **MUST NOT**:

- keep hidden mutable state that tests share
- depend on test order
- change global state quietly
- hide the assertion the test exists to make
- add a layer that makes the failure harder to trace

This helper is illustrative. It is short, and it is still worth having,
because every caller gets the same header and a failure during construction
is reported at the test:

```go
func mustPlan(tb testing.TB, action HelmAction, changes []ResourceChange) Plan {
    tb.Helper()
    plan, err := NewPlan(standardHeader(), action, changes)
    require.NoError(tb, err)
    return plan
}
```

A function whose body is only `t.TempDir()` adds none of that. The test's
main check stays in the test. The helper may require that the plan exists.
The test then asserts what the plan contains.

### Where helpers live

Use the narrowest owner.

**Next to the test.** Helpers for one test, or one behavior, stay in that
test file. A loader that treats a missing hook directory as "no hooks" is
that behavior's policy. A shared package would be the wrong home.

**In the package.** Helpers used by several tests in one package stay in
that package. `helpers_test.go` is optional. Do not move a package-local
helper into `internal/testing/` only because several tests in the same
package call it.

**Shared across packages.** A helper that more than one package imports
**MAY** live under `internal/testing/`, in a package with one job. Do not
add a package named `common`, `testutil`, or `testhelpers` unless a real
boundary needs one.

Package-local helpers that only tests call belong in `_test.go` files. A
helper that other packages import belongs in an ordinary `.go` file.

### `testing.TB` and `t.Helper`

Use `testing.TB` when the helper only needs facilities that tests,
benchmarks, and fuzz targets share.

Use `*testing.T` when the helper calls `t.Run` or `t.Parallel`.

Do not take a testing parameter in a function that does not fail the test
or clean up. A pure builder returns a value.

A helper that reports a failure through the `testing` API **MUST** call
`Helper()` at the start, so the failure is attributed to the caller. That
includes setup, assertions, fixture loading, golden comparison, and resource
creation. A pure function does not call it.

### What a helper may assert

A helper **MAY** use `require` for a precondition, such as "this plan built"
or "this fixture was readable."

The test's main check stays in the test. A reader should see the contract
without opening several helpers. A golden comparison may live in a helper.
An extra assertion of a critical invariant stays next to the call when it
names a different contract.

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

If a helper returns a cleanup function, the caller owns it and registers it.
The names in this snippet are illustrative:

```go
manifests, cleanup, err := render(t.Context(), spec)
require.NoError(t, err)
if cleanup != nil {
    t.Cleanup(cleanup)
}
```

Do not build a cleanup framework. Use `TestMain` only when process-wide
state cannot be isolated in one test, such as a kubeconfig path that
client libraries read from the process. Do not add `TestMain` only to share
ordinary setup.

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

A command that reads configuration from disk is a fixture case, because the
directory layout is part of the contract. This layout is illustrative:

```text
testdata/valid-environment/deployah.yaml
testdata/valid-environment/runtime.env
testdata/missing-environment/deployah.yaml
```

The success text `"configuration is valid"` stays in the test. It is one
short string. A file would only add a hop. The directories are read-only, so
parallel subtests may share them.

Put package data in that package's `testdata/` directory. Put pipeline
fixtures under `scenarios/<name>/`. Do not move every fixture into one
global directory. Ownership follows the test that needs the data.

Name the directory for the case: `missing-environment`,
`invalid-resource-limit`, `task-dependency-failure`. Avoid `fixture1` and
`test-case-4`. The name should explain the scenario before you open the
files.

Keep a fixture small enough to show the behavior and realistic enough that
the test still means something. Do not copy a full production configuration
when a few fields would do. Do not duplicate a large fixture that already
exists.

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
you have. A short file a person wrote, such as the word `changed` or an
expected error phrase, is a fixture even when its name ends in `.golden`.
Do not regenerate it.

### When a golden file helps

Consider a golden file when:

1. The output is substantial.
2. The whole output is a meaningful contract.
3. The output is deterministic.
4. An exact or semantic comparison catches real regressions.
5. An inline expectation would bury the test.
6. Reviewers should see the output diff.

Skip a golden file for a boolean, a number, a short error, a few struct
fields, or a short slice. Use `assert.Equal`, `assert.True`,
`require.ErrorIs`, or `require.ErrorAs`.

Package-local baselines belong in that package's `testdata/` directory,
commonly `testdata/golden/<name>.golden`. The extension **MAY** follow the
format. Scenario manifests belong under `scenarios/<name>/expected/` or
`scenarios/<name>/expected-<env>/`, one file per object, named
`<kind>-<name>.yaml`. The update flag is the same either way.

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
contract. `assert.Equal` fails when whitespace or key order changes.

Use semantic JSON comparison when structure matters and insignificant
formatting does not. `assert.JSONEq` accepts a formatting difference and
still fails when a value changes.

Pick the comparison that matches what users depend on. Do not normalize away
a property the test is supposed to lock.

Golden output **MUST** be deterministic. Keep out random identifiers, the
current time, unstable map order, ephemeral Kubernetes metadata, temporary
paths, machine-specific configuration, and secrets. A freshly generated TLS
certificate is the kind of field you may replace with a fixed marker before
comparison. Leave every other field intact. Normalization is a choice about
the contract. It is not a way to hide a regression.

### Golden files and other assertions

A golden file does not replace an explicit check of a critical invariant.
Combine them when the extra assertion names a different contract. Do not
repeat every golden field in more assertions.

A rendered plan can be locked two ways. The golden file locks the document.
A separate check locks one fact a reader must see, such as "the migrate
task is present even though no Kubernetes resources changed," or "the
document matches the published schema." Skip the second check when it only
restates a field the golden file already locks.

Writing the output twice and comparing the two strings is a determinism
check. The golden file is the approved bytes. They answer different
questions.

### Updating golden files

Every regenerable golden snapshot uses the flag `-update`. Do not add
`-update-golden`, `-update-snapshots`, `-rewrite`, `UPDATE_GOLDEN`, or
`UPDATE_SNAPSHOTS`.

Go runs each test package as its own binary. The package that owns the
goldens registers `-update` for that binary. Do not register the flag twice
in one binary. `go test ./... -update` is not a supported command. A package
that does not register the flag rejects it. Run `-update` on the package
that owns the files. Scenario manifests are behind the `integration` tag, so
regeneration of those files uses that tag. Commands are in the
[contributor guide](../../CONTRIBUTING.md#tests).

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

`-e2e.scaffold` generates an end-to-end fixture skeleton. It does not
regenerate golden files.

## Table-driven tests

Use a table when several cases share the same inputs, outputs, and
assertions. Name every case. Use `t.Run` so a failure names the case and so
the case can run in parallel.

The missing-field example above is the right size: two cases, one assertion
shape. A larger table of one command against several fixture directories is
the same idea, including a row that expects success and a row that expects
an error, while every row still checks the command the same way.

Leave a single case as its own test. Split the table when a row needs
different setup or a different kind of assertion. Do not share mutable
values across rows. Keep the table as data. Do not hide the assertion behind
a function field on the row.

## Assertions

Deployah tests use testify.

Use `require` when a failure means the rest of the test cannot run. Use
`assert` for checks that stand on their own.

Prefer a specific assertion. `assert.Equal` and `assert.True` say what was
compared.

For errors, pick the assertion that matches the contract. These snippets are
illustrative:

```go
require.Error(t, err)
assert.ErrorIs(t, err, ErrUnknownEnvironment)
```

`ErrorIs` matches a sentinel, including through wrapping.

```go
var verr *ValidationError
require.ErrorAs(t, err, &verr)
assert.Equal(t, "environment", verr.Field)
```

`ErrorAs` matches an error type and lets you check its fields.

```go
assert.ErrorContains(t, err, "environment is required")
```

`ErrorContains` is only for message text that is itself the contract. A
substring match does not show which error you received. An unrelated error
can contain the same words.

Call `require.Error` or `require.NoError` when later lines depend on that
outcome. `err != nil` alone is not enough when identity, type, or message
text matters.

Skip assertion messages that repeat what testify already prints. Add a
message when the surrounding values would otherwise be invisible, such as
command stderr when success was required and the command failed.

Keep the main behavioral assertion in the test function.

## Isolation

A test **MUST NOT** depend on another test or on execution order. Each test
owns the state it mutates.

Avoid shared mutable globals, a writable fixture directory shared by tests,
a fixed temporary path, and process-wide configuration that another test can
see.

A test **SHOULD** pass alone and as part of the suite, in any order. When a
shuffled run fails, reuse the seed it prints.

Shuffling test order is separate from random values inside a test. See
[Determinism](#determinism).

## Parallel execution

`t.Parallel()` is the default. Call it as the first line of the test and as
the first line of each subtest.

Read-only fixtures may be shared. A validate test that only reads its
fixture directory can run subtests in parallel against the same trees.

Skip `t.Parallel()` only for a concrete reason:

- The test, a parent test, or a helper calls `t.Setenv` or `t.Chdir`. Both
  change process-wide state. Go rejects them in a parallel test and under a
  parallel parent.
- The test depends on other shared mutable process state.
- Under `-update`, the test would write a golden path that another parallel
  test also writes.
- The test shares an external resource whose lifecycle is intentionally
  process-wide. The e2e suite's shared cluster is that case. Each case still
  isolates the namespaces and objects it creates.

A helper **MUST NOT** hide `t.Setenv`, `t.Chdir`, or another process-wide
change. If it needs one, the calling test cannot be parallel, and that
restriction should be obvious at the call site.

## Determinism

A test **SHOULD** produce the same result on every run.

When randomness changes inputs, expected results, or assertions, use a
reproducible seed, or keep enough information to rerun the failure.
Randomized fixtures and property-style checks belong here.

A random identifier **MAY** stay nondeterministic when its only job is to
isolate a resource. An e2e test may build a namespace name from random bytes
so two runs do not collide. The assertion is that deploy created that
namespace, not that the name was a particular string. An identifier used
this way must leave assertions and golden output stable, keep the resource
isolated, and still be cleaned up. Normalize it before it enters a golden
file.

Avoid sleeps that hope a condition becomes true, assumptions about the wall
clock, map order that the contract treats as stable, and shared mutable
state.

For Kubernetes readiness, Helm, container startup, reconciliation, and
background work, wait for a condition with a bounded timeout. Do not sleep
for a fixed interval and hope.

## Context and cleanup

Pass a context into work that can block. Prefer `t.Context()`.

`t.Context()` is canceled before `t.Cleanup` functions run. Cleanup that
still needs a context uses its own bounded context, for example
`context.WithTimeout(context.Background(), timeout)`.

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

A Kubernetes fake clientset does not reproduce API-server admission,
defaulting, or validation. Cover those contracts with scenario tests or e2e
tests.

Use `net/http/httptest` when the behavior under test is HTTP. Do not bind a
fixed port for that. Do not add an HTTP test when the code does not speak
HTTP.

## Package boundary

Same-package tests are the default. Use them unless the external boundary is
the contract you need to lock. An unexported comparison is tested from the
same package. An external test package cannot see it.

An external test package (`package foo_test`) is appropriate when the test
should import the package the way another module would. A test that drives
the public CLI is that kind of boundary.

A directory **MAY** contain both internal and external test files. That mix
is not a defect by itself. Do not move tests from one form to the other as
a style cleanup.

## Failure diagnostics

A failure should name the behavior, the expected result, the actual result,
and the scenario. Subtest names carry much of that.

When a golden file mismatches, the failure **MUST** include the file path
and say that `-update` regenerates an intentional change. A missing golden
file fails the test and shows the output that was produced.

When external infrastructure fails, include the context needed to reproduce
it: the scenario directory, the namespace, and the command's stderr.

Vague text such as `values differ` without the input is not enough. Name
the function and the case when testify's default output does not.

## Coverage

Write tests for behavior. Deployment correctness, configuration validation,
resource lifecycle, error handling, and important edge cases come first.

Coverage can show behavior you have not tested. A percentage does not show
that the tests are good. Do not add a test only to move a number.

This page does not set a coverage percentage. A coverage check in CI does
not replace these rules.

## Benchmarks and examples

Benchmarks are not required. Add one when you have a specific performance
question to measure. Most packages in this CLI do not need one.

`Example` functions are not required for every exported symbol in an
internal package. Add one when the example is the clearest way to show how
to call the API.

## Race detection

Run the race detector on unit and integration tests. It catches many
shared-memory mistakes. A clean run does not prove the program has no
concurrency bugs.

End-to-end tests target a live cluster. They are not a race-detector run.
