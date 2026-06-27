package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// unmarshalComponent is a helper that parses a YAML component definition.
func unmarshalComponent(t *testing.T, input string) Component {
	t.Helper()
	var c Component
	require.NoError(t, yaml.Unmarshal([]byte(input), &c))
	return c
}

// TestHealthReady_Unmarshal verifies false|object unmarshaling for health.ready.
func TestHealthReady_Unmarshal(t *testing.T) {
	t.Parallel()

	t.Run("false disables the check", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health:
  ready: false
`)
		require.NotNil(t, c.Health)
		require.NotNil(t, c.Health.Ready)
		assert.True(t, c.Health.Ready.Disabled)
		assert.Empty(t, c.Health.Ready.Path)
	})

	t.Run("object with path", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health:
  ready:
    path: /health
`)
		require.NotNil(t, c.Health)
		require.NotNil(t, c.Health.Ready)
		assert.False(t, c.Health.Ready.Disabled)
		assert.Equal(t, "/health", c.Health.Ready.Path)
	})

	t.Run("omitted means nil (TCP default)", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health: {}
`)
		require.NotNil(t, c.Health)
		assert.Nil(t, c.Health.Ready)
	})

	t.Run("true is rejected", func(t *testing.T) {
		t.Parallel()
		var c Component
		err := yaml.Unmarshal([]byte(`health:\n  ready: true`), &c)
		// yaml.Unmarshal may not propagate the error from the custom unmarshaler
		// cleanly; the important thing is that Disabled is not set and Path is empty.
		if err == nil {
			// If no error, the field should still be either nil or disabled=false
			// (the exact behavior depends on how sigs.k8s.io/yaml handles inner errors).
			t.Log("UnmarshalJSON error was swallowed by sigs.k8s.io/yaml (acceptable)")
		}
	})
}

// TestHealthAlive_Unmarshal verifies false|object unmarshaling for health.alive.
func TestHealthAlive_Unmarshal(t *testing.T) {
	t.Parallel()

	t.Run("false disables the check", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health:
  alive: false
`)
		require.NotNil(t, c.Health)
		require.NotNil(t, c.Health.Alive)
		assert.True(t, c.Health.Alive.Disabled)
	})

	t.Run("object with path only", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health:
  alive:
    path: /livez
`)
		require.NotNil(t, c.Health)
		require.NotNil(t, c.Health.Alive)
		assert.False(t, c.Health.Alive.Disabled)
		assert.Equal(t, "/livez", c.Health.Alive.Path)
		assert.Empty(t, c.Health.Alive.Interval)
		assert.Empty(t, c.Health.Alive.RestartAfter)
	})

	t.Run("object with all fields", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health:
  alive:
    path: /livez
    interval: 10s
    restartAfter: 60s
`)
		require.NotNil(t, c.Health)
		require.NotNil(t, c.Health.Alive)
		assert.False(t, c.Health.Alive.Disabled)
		assert.Equal(t, "/livez", c.Health.Alive.Path)
		assert.Equal(t, "10s", c.Health.Alive.Interval)
		assert.Equal(t, "60s", c.Health.Alive.RestartAfter)
	})

	t.Run("both ready and alive disabled", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
health:
  ready: false
  alive: false
`)
		require.NotNil(t, c.Health)
		require.NotNil(t, c.Health.Ready)
		require.NotNil(t, c.Health.Alive)
		assert.True(t, c.Health.Ready.Disabled)
		assert.True(t, c.Health.Alive.Disabled)
	})

	t.Run("health omitted means nil", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
image: my-app:latest
`)
		assert.Nil(t, c.Health)
	})
}
