package action_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/spec"
)

// ctxMarker identifies the context a dependency received.
type ctxMarker struct{}

type mockDeployer struct {
	err          error
	resolved     *spec.ResolvedSpec
	dryRun       bool
	postRenderer postrenderer.PostRenderer
	called       bool
	ctx          context.Context
}

func (m *mockDeployer) InstallApp(ctx context.Context, dryRun bool, resolved *spec.ResolvedSpec, pr postrenderer.PostRenderer) error {
	m.called = true
	m.ctx = ctx
	m.dryRun = dryRun
	m.resolved = resolved
	m.postRenderer = pr
	if m.err != nil {
		return m.err
	}
	return ctx.Err()
}

type mockSpecLoader struct {
	m           *spec.Spec
	err         error
	environment string
	ctx         context.Context
	calls       int
}

func (m *mockSpecLoader) Spec(ctx context.Context, environment string) (*spec.Spec, spec.SubstitutionReport, error) {
	m.calls++
	m.ctx = ctx
	m.environment = environment
	if m.err != nil {
		return nil, spec.SubstitutionReport{}, m.err
	}
	if err := ctx.Err(); err != nil {
		return nil, spec.SubstitutionReport{}, err
	}
	return m.m, spec.SubstitutionReport{}, nil
}

func testSpec() *spec.Spec {
	return &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "my-app",
	}
}

// Run loads the manifest and delegates to the deployer.
func TestDeploy_Run(t *testing.T) {
	t.Parallel()
	t.Run("succeeds when deployer and loader succeed", func(t *testing.T) {
		t.Parallel()
		ctx := context.WithValue(t.Context(), ctxMarker{}, "deploy")
		deployer := &mockDeployer{}
		loader := &mockSpecLoader{m: testSpec()}
		d := action.NewDeploy(deployer, loader)
		m, err := d.Run(ctx, "prod", false)
		require.NoError(t, err)
		assert.Equal(t, "my-app", m.Project)
		assert.Equal(t, 1, loader.calls)
		assert.Equal(t, "prod", loader.environment)
		assert.Equal(t, "deploy", loader.ctx.Value(ctxMarker{}))
		require.True(t, deployer.called)
		assert.False(t, deployer.dryRun)
		assert.Nil(t, deployer.postRenderer)
		assert.Equal(t, "deploy", deployer.ctx.Value(ctxMarker{}))
		require.NotNil(t, deployer.resolved)
		require.NotNil(t, deployer.resolved.Components)
		require.NotNil(t, deployer.resolved.Tasks)
		assert.Equal(t, "prod", deployer.resolved.Env.Original)
	})

	t.Run("dry run still resolves and forwards the flag", func(t *testing.T) {
		t.Parallel()
		deployer := &mockDeployer{}
		loader := &mockSpecLoader{m: testSpec()}
		d := action.NewDeploy(deployer, loader)
		m, err := d.Run(t.Context(), "prod", true)
		require.NoError(t, err)
		assert.Equal(t, "my-app", m.Project)
		assert.Equal(t, "prod", loader.environment)
		require.True(t, deployer.called)
		assert.True(t, deployer.dryRun)
		assert.Nil(t, deployer.postRenderer)
		require.NotNil(t, deployer.resolved)
		assert.Equal(t, "prod", deployer.resolved.Env.Original)
	})

	t.Run("wildcard environment keeps the instance identity", func(t *testing.T) {
		t.Parallel()
		deployer := &mockDeployer{}
		loader := &mockSpecLoader{m: testSpec()}
		d := action.NewDeploy(deployer, loader)
		_, err := d.Run(t.Context(), "review/pr-123", false)
		require.NoError(t, err)
		assert.Equal(t, "review/pr-123", loader.environment)
		require.NotNil(t, deployer.resolved)
		assert.Equal(t, "review/pr-123", deployer.resolved.Env.Original)
		assert.Equal(t, "review", deployer.resolved.Env.MapKey)
	})

	t.Run("returns error when manifest loader fails", func(t *testing.T) {
		t.Parallel()
		errLoad := errors.New("not found")
		deployer := &mockDeployer{}
		d := action.NewDeploy(deployer, &mockSpecLoader{err: errLoad})
		_, err := d.Run(t.Context(), "prod", false)
		require.ErrorIs(t, err, errLoad)
		assert.ErrorContains(t, err, "load spec")
		assert.False(t, deployer.called)
	})

	t.Run("returns error when deployer fails", func(t *testing.T) {
		t.Parallel()
		errInstall := errors.New("helm error")
		d := action.NewDeploy(&mockDeployer{err: errInstall}, &mockSpecLoader{m: testSpec()})
		_, err := d.Run(t.Context(), "prod", false)
		require.ErrorIs(t, err, errInstall)
		assert.ErrorContains(t, err, "install")
	})

	t.Run("returns canceled when the loader context is done", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		deployer := &mockDeployer{}
		d := action.NewDeploy(deployer, &mockSpecLoader{m: testSpec()})
		_, err := d.Run(ctx, "prod", false)
		require.ErrorIs(t, err, context.Canceled)
		assert.ErrorContains(t, err, "load spec")
		assert.False(t, deployer.called)
	})

	t.Run("returns error when resolve fails", func(t *testing.T) {
		t.Parallel()
		m := &spec.Spec{
			APIVersion: spec.CurrentManifestVersion,
			Project:    "my-app",
			Components: map[string]spec.Component{
				"api": {Image: "busybox"},
			},
			Tasks: map[string]spec.Task{
				"migrate": {
					From:    "api",
					On:      spec.TaskOnPreDeploy,
					After:   []string{"migrate"},
					Command: []string{"migrate"},
				},
			},
			Environments: map[string]spec.Environment{
				"prod": {},
			},
		}
		deployer := &mockDeployer{}
		d := action.NewDeploy(deployer, &mockSpecLoader{m: m})
		_, err := d.Run(t.Context(), "prod", false)
		require.Error(t, err)
		assert.ErrorContains(t, err, "resolve spec")
		cycle, ok := errors.AsType[*spec.HookCycleError](err)
		require.True(t, ok)
		assert.Equal(t, []string{"migrate"}, cycle.Tasks)
		assert.False(t, deployer.called)
	})
}
