package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
)

// TestBuildEnvironmentOverview verifies rows merge both files: platform
// entries are deployable (with context or fallback), spec-only names are
// not, and spec entries contribute their overrides.
func TestBuildEnvironmentOverview(t *testing.T) {
	t.Parallel()

	rawSpec := &spec.Spec{
		Environments: map[string]spec.Environment{
			"production": {Variables: map[string]string{"TAG": "v1"}},
			"qa":         {EnvFile: ".env.qa"},
		},
	}
	platform := &spec.PlatformConfig{
		Environments: map[string]spec.PlatformEnvironment{
			"local": {
				Context: "kind-deployah",
				Domains: map[string]spec.PlatformDomain{"public": {BaseDomain: "127.0.0.1.nip.io"}},
			},
			"production": {},
		},
	}

	rows := buildEnvironmentOverview(rawSpec, platform, "minikube")
	require.Len(t, rows, 3)

	local, production, qa := rows[0], rows[1], rows[2]

	assert.Equal(t, "local", local.Name)
	assert.Equal(t, "platform", local.Source)
	assert.True(t, local.Deployable)
	assert.Equal(t, "kind-deployah", local.Context)
	assert.Empty(t, local.ContextFallback)
	assert.Equal(t, []string{"public"}, local.Domains)

	assert.Equal(t, "production", production.Name)
	assert.True(t, production.Deployable)
	assert.Empty(t, production.Context)
	assert.Equal(t, "minikube", production.ContextFallback)
	assert.Equal(t, []string{"variables"}, production.Overrides)

	assert.Equal(t, "qa", qa.Name)
	assert.Equal(t, "spec-only", qa.Source)
	assert.False(t, qa.Deployable)
	assert.Equal(t, []string{"envFile"}, qa.Overrides)
}

// TestBuildEnvironmentOverview_NoPlatformFile verifies the spec acts as the
// registry when no platform file exists.
func TestBuildEnvironmentOverview_NoPlatformFile(t *testing.T) {
	t.Parallel()

	rawSpec := &spec.Spec{
		Environments: map[string]spec.Environment{"staging": {}},
	}

	rows := buildEnvironmentOverview(rawSpec, nil, "minikube")
	require.Len(t, rows, 1)
	assert.Equal(t, "spec", rows[0].Source)
	assert.True(t, rows[0].Deployable)
	assert.Equal(t, "minikube", rows[0].ContextFallback)
}
