package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectorBuilder_Build_Empty verifies an empty builder yields an
// empty selector string.
func TestSelectorBuilder_Build_Empty(t *testing.T) {
	t.Parallel()

	sb := NewSelectorBuilder()
	assert.Empty(t, sb.Build())
}

// TestSelectorBuilder_WithProject verifies project label requirement
// handling, including the empty-string no-op case.
func TestSelectorBuilder_WithProject(t *testing.T) {
	t.Parallel()

	t.Run("empty is a no-op", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithProject("")
		require.NoError(t, err)
		assert.Empty(t, got.Build())
	})

	t.Run("valid project sets requirement", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithProject("my-app")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/project=my-app", got.Build())
	})

	t.Run("project with dots and dashes", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithProject("my-app.v2")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/project=my-app.v2", got.Build())
	})

	t.Run("value with invalid label characters returns error", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		_, err := sb.WithProject("invalid project!")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create project label requirement")
	})
}

// TestSelectorBuilder_WithComponent verifies component label requirement
// handling, including the empty-string no-op case.
func TestSelectorBuilder_WithComponent(t *testing.T) {
	t.Parallel()

	t.Run("empty is a no-op", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithComponent("")
		require.NoError(t, err)
		assert.Empty(t, got.Build())
	})

	t.Run("valid component sets requirement", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithComponent("api")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/component=api", got.Build())
	})
}

// TestSelectorBuilder_WithEnvironment verifies environment label requirement
// handling, including normalization of wildcard "/" environments to their
// Kubernetes-safe form.
func TestSelectorBuilder_WithEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("empty is a no-op", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithEnvironment("")
		require.NoError(t, err)
		assert.Empty(t, got.Build())
	})

	t.Run("plain environment name is unchanged", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithEnvironment("production")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/environment=production", got.Build())
	})

	t.Run("wildcard environment is normalized to k8s-safe form", func(t *testing.T) {
		t.Parallel()

		sb := NewSelectorBuilder()
		got, err := sb.WithEnvironment("review/pr-42")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/environment=review-pr-42", got.Build())
	})
}

// TestSelectorBuilder_Chaining verifies multiple requirements combine into
// a single comma-separated selector, and that builder calls can be chained.
func TestSelectorBuilder_Chaining(t *testing.T) {
	t.Parallel()

	sb := NewSelectorBuilder()
	sb, err := sb.WithProject("my-app")
	require.NoError(t, err)
	sb, err = sb.WithComponent("api")
	require.NoError(t, err)
	sb, err = sb.WithEnvironment("staging")
	require.NoError(t, err)

	got := sb.Build()
	assert.Contains(t, got, "deployah.dev/project=my-app")
	assert.Contains(t, got, "deployah.dev/component=api")
	assert.Contains(t, got, "deployah.dev/environment=staging")
}

// TestBuildSelector verifies the convenience wrapper that combines project,
// component, and environment into a single selector string.
func TestBuildSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		project     string
		component   string
		environment string
		want        string
	}{
		{name: "all empty yields empty selector", project: "", component: "", environment: "", want: ""},
		{
			name: "project only", project: "my-app", component: "", environment: "",
			want: "deployah.dev/project=my-app",
		},
		{
			name: "component only", project: "", component: "api", environment: "",
			want: "deployah.dev/component=api",
		},
		{
			name: "environment only", project: "", component: "", environment: "production",
			want: "deployah.dev/environment=production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildSelector(tt.project, tt.component, tt.environment)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("all set combines requirements", func(t *testing.T) {
		t.Parallel()

		got, err := BuildSelector("my-app", "api", "staging")
		require.NoError(t, err)
		assert.Contains(t, got, "deployah.dev/project=my-app")
		assert.Contains(t, got, "deployah.dev/component=api")
		assert.Contains(t, got, "deployah.dev/environment=staging")
	})

	t.Run("invalid project value returns error before building the rest", func(t *testing.T) {
		t.Parallel()

		got, err := BuildSelector("invalid project!", "api", "staging")
		require.Error(t, err)
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "failed to create project label requirement")
	})
}

// TestBuildProjectSelector verifies the project-only convenience wrapper.
func TestBuildProjectSelector(t *testing.T) {
	t.Parallel()

	t.Run("empty project", func(t *testing.T) {
		t.Parallel()

		got, err := BuildProjectSelector("")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("valid project", func(t *testing.T) {
		t.Parallel()

		got, err := BuildProjectSelector("my-app")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/project=my-app", got)
	})
}

// TestBuildComponentSelector verifies the project+component convenience
// wrapper.
func TestBuildComponentSelector(t *testing.T) {
	t.Parallel()

	t.Run("both empty", func(t *testing.T) {
		t.Parallel()

		got, err := BuildComponentSelector("", "")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("both set", func(t *testing.T) {
		t.Parallel()

		got, err := BuildComponentSelector("my-app", "api")
		require.NoError(t, err)
		assert.Contains(t, got, "deployah.dev/project=my-app")
		assert.Contains(t, got, "deployah.dev/component=api")
	})
}

// TestBuildLabelSelector verifies the [labels.Selector]-returning variant
// used by watch/list calls, including empty and combined cases.
func TestBuildLabelSelector(t *testing.T) {
	t.Parallel()

	t.Run("both empty matches everything", func(t *testing.T) {
		t.Parallel()

		// labels.NewSelector() returns a nil-backed internalSelector, so
		// sel is never a bare nil interface even here; assert on Empty()
		// instead of nilness.
		sel, err := BuildLabelSelector("", "")
		require.NoError(t, err)
		assert.True(t, sel.Empty())
	})

	t.Run("project only", func(t *testing.T) {
		t.Parallel()

		sel, err := BuildLabelSelector("my-app", "")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/project=my-app", sel.String())
	})

	t.Run("environment only normalizes wildcard names", func(t *testing.T) {
		t.Parallel()

		sel, err := BuildLabelSelector("", "review/pr-7")
		require.NoError(t, err)
		assert.Equal(t, "deployah.dev/environment=review-pr-7", sel.String())
	})

	t.Run("both set", func(t *testing.T) {
		t.Parallel()

		sel, err := BuildLabelSelector("my-app", "production")
		require.NoError(t, err)
		assert.Contains(t, sel.String(), "deployah.dev/project=my-app")
		assert.Contains(t, sel.String(), "deployah.dev/environment=production")
	})

	t.Run("invalid project value returns error", func(t *testing.T) {
		t.Parallel()

		sel, err := BuildLabelSelector("bad value!", "")
		require.Error(t, err)
		assert.Nil(t, sel)
		assert.Contains(t, err.Error(), "project label")
	})

	t.Run("invalid environment value returns error", func(t *testing.T) {
		t.Parallel()

		sel, err := BuildLabelSelector("", "bad value!")
		require.Error(t, err)
		assert.Nil(t, sel)
		assert.Contains(t, err.Error(), "environment label")
	})
}
