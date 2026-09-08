package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/spec"
)

type mockDeployer struct {
	err      error
	resolved *spec.ResolvedSpec
}

func (m *mockDeployer) InstallApp(_ context.Context, _ bool, resolved *spec.ResolvedSpec, _ postrenderer.PostRenderer) error {
	m.resolved = resolved
	return m.err
}

type mockSpecLoader struct {
	m   *spec.Spec
	err error
}

func (m *mockSpecLoader) Spec(_ context.Context, _ string) (*spec.Spec, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.m, nil
}

var testManifest = &spec.Spec{
	APIVersion: spec.CurrentManifestVersion,
	Project:    "my-app",
}

// Run loads the manifest and delegates to the deployer.
func TestDeploy_Run(t *testing.T) {
	t.Parallel()
	t.Run("succeeds when deployer and loader succeed", func(t *testing.T) {
		t.Parallel()
		deployer := &mockDeployer{}
		d := action.NewDeploy(deployer, &mockSpecLoader{m: testManifest})
		m, err := d.Run(t.Context(), "prod", false)
		require.NoError(t, err)
		assert.Equal(t, "my-app", m.Project)
		require.NotNil(t, deployer.resolved)
		require.NotNil(t, deployer.resolved.Components)
		require.NotNil(t, deployer.resolved.Tasks)
		assert.Equal(t, "prod", deployer.resolved.Env.Original)
	})

	t.Run("returns error when manifest loader fails", func(t *testing.T) {
		t.Parallel()
		d := action.NewDeploy(&mockDeployer{}, &mockSpecLoader{err: fmt.Errorf("not found")})
		_, err := d.Run(t.Context(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load spec")
	})

	t.Run("returns error when deployer fails", func(t *testing.T) {
		t.Parallel()
		d := action.NewDeploy(&mockDeployer{err: fmt.Errorf("helm error")}, &mockSpecLoader{m: testManifest})
		_, err := d.Run(t.Context(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "install")
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
		d := action.NewDeploy(&mockDeployer{}, &mockSpecLoader{m: m})
		_, err := d.Run(t.Context(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve spec")
	})
}
