package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"
)

// TestSelectorBuilder_Build_Empty verifies an empty builder yields an
// empty selector string.
func TestSelectorBuilder_Build_Empty(t *testing.T) {
	t.Parallel()

	sb := NewSelectorBuilder()
	assert.Empty(t, sb.Build())
}

// TestSelectorBuilder_WithProject verifies project label requirement
// handling, including the empty-string no-op case and the invalid-value
// error path.
func TestSelectorBuilder_WithProject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       string
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "empty is a no-op", value: "", want: ""},
		{name: "valid project sets requirement", value: "my-app", want: "deployah.dev/project=my-app"},
		{name: "project with dots and dashes", value: "my-app.v2", want: "deployah.dev/project=my-app.v2"},
		{
			name: "value with invalid label characters returns error", value: "invalid project!",
			wantErr: true, errContains: "failed to create project label requirement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSelectorBuilder().WithProject(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Build())
		})
	}
}

// TestSelectorBuilder_WithComponent verifies component label requirement
// handling, including the empty-string no-op case.
func TestSelectorBuilder_WithComponent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty is a no-op", value: "", want: ""},
		{name: "valid component sets requirement", value: "api", want: "deployah.dev/component=api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSelectorBuilder().WithComponent(tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Build())
		})
	}
}

// TestSelectorBuilder_WithEnvironment verifies environment label requirement
// handling, including normalization of wildcard "/" environments to their
// Kubernetes-safe form.
func TestSelectorBuilder_WithEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty is a no-op", value: "", want: ""},
		{name: "plain environment name is unchanged", value: "production", want: "deployah.dev/environment=production"},
		{
			name: "wildcard environment is normalized to k8s-safe form", value: "review/pr-42",
			want: "deployah.dev/environment=review-pr-42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSelectorBuilder().WithEnvironment(tt.value)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Build())
		})
	}
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
// component, and environment into a single selector string, including the
// error path when a value contains invalid label characters.
func TestBuildSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		project     string
		component   string
		environment string
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "all empty yields empty selector", want: ""},
		{name: "project only", project: "my-app", want: "deployah.dev/project=my-app"},
		{name: "component only", component: "api", want: "deployah.dev/component=api"},
		{name: "environment only", environment: "production", want: "deployah.dev/environment=production"},
		{name: "all set combines requirements", project: "my-app", component: "api", environment: "staging"},
		{
			name:    "invalid project value returns error before building the rest",
			project: "invalid project!", component: "api", environment: "staging",
			wantErr: true, errContains: "failed to create project label requirement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildSelector(tt.project, tt.component, tt.environment)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			if tt.project != "" && tt.component != "" && tt.environment != "" {
				// All three requirements are present; assert containment
				// rather than exact string, since selector ordering is
				// an implementation detail.
				assert.Contains(t, got, "deployah.dev/project="+tt.project)
				assert.Contains(t, got, "deployah.dev/component="+tt.component)
				assert.Contains(t, got, "deployah.dev/environment="+tt.environment)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildProjectSelector verifies the project-only convenience wrapper.
func TestBuildProjectSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project string
		want    string
	}{
		{name: "empty project", project: "", want: ""},
		{name: "valid project", project: "my-app", want: "deployah.dev/project=my-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildProjectSelector(tt.project)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildComponentSelector verifies the project+component convenience
// wrapper.
func TestBuildComponentSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		project   string
		component string
		check     func(t *testing.T, got string)
	}{
		{
			name: "both empty", project: "", component: "",
			check: func(t *testing.T, got string) { t.Helper(); assert.Empty(t, got) },
		},
		{
			name: "both set", project: "my-app", component: "api",
			check: func(t *testing.T, got string) {
				t.Helper()
				assert.Contains(t, got, "deployah.dev/project=my-app")
				assert.Contains(t, got, "deployah.dev/component=api")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildComponentSelector(tt.project, tt.component)
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

// TestBuildLabelSelector verifies the [labels.Selector]-returning variant
// used by watch/list calls, including empty, combined, and error cases.
func TestBuildLabelSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		project     string
		environment string
		wantErr     bool
		errContains string
		check       func(t *testing.T, sel labels.Selector)
	}{
		{
			name: "both empty matches everything",
			// labels.NewSelector() returns a nil-backed internalSelector,
			// so sel is never a bare nil interface even here; assert on
			// Empty() instead of nilness.
			check: func(t *testing.T, sel labels.Selector) { t.Helper(); assert.True(t, sel.Empty()) },
		},
		{
			name: "project only", project: "my-app",
			check: func(t *testing.T, sel labels.Selector) {
				t.Helper()
				assert.Equal(t, "deployah.dev/project=my-app", sel.String())
			},
		},
		{
			name: "environment only normalizes wildcard names", environment: "review/pr-7",
			check: func(t *testing.T, sel labels.Selector) {
				t.Helper()
				assert.Equal(t, "deployah.dev/environment=review-pr-7", sel.String())
			},
		},
		{
			name: "both set", project: "my-app", environment: "production",
			check: func(t *testing.T, sel labels.Selector) {
				t.Helper()
				assert.Contains(t, sel.String(), "deployah.dev/project=my-app")
				assert.Contains(t, sel.String(), "deployah.dev/environment=production")
			},
		},
		{
			name: "invalid project value returns error", project: "bad value!",
			wantErr: true, errContains: "project label",
		},
		{
			name: "invalid environment value returns error", environment: "bad value!",
			wantErr: true, errContains: "environment label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sel, err := BuildLabelSelector(tt.project, tt.environment)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, sel)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			tt.check(t, sel)
		})
	}
}
