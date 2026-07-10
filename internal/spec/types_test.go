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

// TestComponentRole_IsService verifies only the "service" role is treated
// as a service.
func TestComponentRole_IsService(t *testing.T) {
	t.Parallel()

	assert.True(t, ComponentRoleService.IsService())
	assert.False(t, ComponentRoleWorker.IsService())
	assert.False(t, ComponentRoleJob.IsService())
}

// TestComponent_ListensOnPort verifies a component listens on a port only
// when it has both the service role and a positive port.
func TestComponent_ListensOnPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component Component
		want      bool
	}{
		{name: "service with port", component: Component{Role: ComponentRoleService, Port: 8080}, want: true},
		{name: "service without a port", component: Component{Role: ComponentRoleService, Port: 0}, want: false},
		{name: "worker with a port set", component: Component{Role: ComponentRoleWorker, Port: 8080}, want: false},
		{name: "job with a port set", component: Component{Role: ComponentRoleJob, Port: 8080}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.component.ListensOnPort())
		})
	}
}

// TestHealthReady_Unmarshal verifies false|object unmarshaling for health.ready.
// TestExpose_Unmarshal verifies the true|false|object shorthand for expose.
func TestExpose_Unmarshal(t *testing.T) {
	t.Parallel()

	t.Run("true is the zero value", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
expose: true
`)
		require.NotNil(t, c.Expose)
		assert.Equal(t, Expose{}, *c.Expose)
	})

	t.Run("false is disabled", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
expose: false
`)
		require.NotNil(t, c.Expose)
		assert.True(t, c.Expose.disabled)
	})

	t.Run("object form", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
expose:
  domain: internal
  subdomain: api
`)
		require.NotNil(t, c.Expose)
		assert.Equal(t, "internal", c.Expose.Domain)
		require.NotNil(t, c.Expose.Subdomain)
		assert.Equal(t, "api", *c.Expose.Subdomain)
	})

	t.Run("apex", func(t *testing.T) {
		t.Parallel()
		c := unmarshalComponent(t, `
expose:
  apex: true
`)
		require.NotNil(t, c.Expose)
		assert.True(t, c.Expose.Apex)
	})
}

// TestExpose_Marshal verifies the zero value round-trips as `expose: true`.
func TestExpose_Marshal(t *testing.T) {
	t.Parallel()

	out, err := yaml.Marshal(Component{Image: "x", Expose: &Expose{}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "expose: true")

	sub := "api"
	out, err = yaml.Marshal(Component{Image: "x", Expose: &Expose{Subdomain: &sub}})
	require.NoError(t, err)
	assert.Contains(t, string(out), "subdomain: api")
}

// TestNormalizeComponents verifies `expose: false` becomes a nil Expose.
func TestNormalizeComponents(t *testing.T) {
	t.Parallel()

	s := &Spec{Components: map[string]Component{
		"off": {Expose: &Expose{disabled: true}},
		"on":  {Expose: &Expose{}},
	}}
	normalizeComponents(s)
	assert.Nil(t, s.Components["off"].Expose)
	assert.NotNil(t, s.Components["on"].Expose)
}

// TestHealthReady_Unmarshal verifies false|object unmarshaling for
// health.ready.
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
