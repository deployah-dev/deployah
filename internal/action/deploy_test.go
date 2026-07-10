package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/spec"
)

type mockDeployer struct {
	err error
}

func (m *mockDeployer) InstallApp(_ context.Context, _ *spec.Spec, _ string, _ bool, _ *spec.ResolvedSpec) error {
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
	APIVersion: "v1-alpha.2",
	Project:    "my-app",
}

// Run loads the manifest and delegates to the deployer.
func TestDeploy_Run(t *testing.T) {
	t.Run("succeeds when deployer and loader succeed", func(t *testing.T) {
		d := action.NewDeploy(&mockDeployer{}, &mockSpecLoader{m: testManifest})
		m, err := d.Run(context.Background(), "prod", false)
		require.NoError(t, err)
		assert.Equal(t, "my-app", m.Project)
	})

	t.Run("returns error when manifest loader fails", func(t *testing.T) {
		d := action.NewDeploy(&mockDeployer{}, &mockSpecLoader{err: fmt.Errorf("not found")})
		_, err := d.Run(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load spec")
	})

	t.Run("returns error when deployer fails", func(t *testing.T) {
		d := action.NewDeploy(&mockDeployer{err: fmt.Errorf("helm error")}, &mockSpecLoader{m: testManifest})
		_, err := d.Run(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "install")
	})
}
