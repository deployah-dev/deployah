package k8s

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"
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
				assert.ErrorContains(t, err, tt.errContains)
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
		name        string
		value       string
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "empty is a no-op", value: "", want: ""},
		{name: "valid component sets requirement", value: "api", want: "deployah.dev/component=api"},
		{
			name: "value with invalid label characters returns error", value: "bad value!",
			wantErr: true, errContains: "failed to create component label requirement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSelectorBuilder().WithComponent(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Build())
		})
	}
}

func TestSelectorBuilder_WithInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       string
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "empty is a no-op", value: "", want: ""},
		{name: "valid instance sets requirement", value: "shop-dev", want: "app.kubernetes.io/instance=shop-dev"},
		{
			name: "value with invalid label characters returns error", value: "bad value!",
			wantErr: true, errContains: "failed to create instance label requirement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSelectorBuilder().WithInstance(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Build())
		})
	}
}

// TestSelectorBuilder_WithEnvironment verifies environment label requirement
// handling, including wildcard names using the logical MapKey.
func TestSelectorBuilder_WithEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       string
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "empty is a no-op", value: "", want: ""},
		{name: "plain environment name is unchanged", value: "production", want: "deployah.dev/environment=production"},
		{
			name: "wildcard environment uses logical map key", value: "review/pr-42",
			want: "deployah.dev/environment=review",
		},
		{
			name: "value with invalid label characters returns error", value: "bad value!",
			wantErr: true, errContains: "failed to create environment label requirement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewSelectorBuilder().WithEnvironment(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errContains)
				return
			}
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
		{
			name: "all set combines requirements", project: "my-app", component: "api", environment: "staging",
			want: "deployah.dev/component=api,deployah.dev/environment=staging,deployah.dev/project=my-app",
		},
		{
			name:    "invalid project value returns error before building the rest",
			project: "invalid project!", component: "api", environment: "staging",
			wantErr: true, errContains: "failed to create project label requirement",
		},
		{
			name:    "invalid component value returns error",
			project: "my-app", component: "bad value!", environment: "staging",
			wantErr: true, errContains: "failed to create component label requirement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildSelector(tt.project, tt.component, tt.environment)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				assert.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
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
		want      string
	}{
		{name: "both empty", project: "", component: "", want: ""},
		{
			name: "both set", project: "my-app", component: "api",
			want: "deployah.dev/component=api,deployah.dev/project=my-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := BuildComponentSelector(tt.project, tt.component)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
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
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "both empty matches everything", want: ""},
		{name: "project only", project: "my-app", want: "deployah.dev/project=my-app"},
		{name: "environment only uses logical map key", environment: "review/pr-7", want: "deployah.dev/environment=review"},
		{
			name: "both set", project: "my-app", environment: "production",
			want: "deployah.dev/environment=production,deployah.dev/project=my-app",
		},
		{
			name: "invalid project value returns error", project: "bad value!",
			wantErr: true, errContains: "project label",
		},
		{
			name: "invalid environment value returns error", environment: "bad value!",
			wantErr: true, errContains: "environment name 'bad value!' is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sel, err := BuildLabelSelector(tt.project, tt.environment)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, sel)
				assert.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, sel.String())
		})
	}
}

func TestBuildSelector_WildcardUsesMapKey(t *testing.T) {
	t.Parallel()

	got, err := BuildSelector("shop", "api", "review/pr-123")
	require.NoError(t, err)
	assert.Contains(t, got, "deployah.dev/environment=review")
	assert.NotContains(t, got, "deployah.dev/environment=review-pr-123")
	assert.NotContains(t, got, "deployah.dev/environment=review/pr-123")
	assert.Contains(t, got, "app.kubernetes.io/instance="+helm.GenerateReleaseName("shop", "review/pr-123"))
}

func TestBuildLabelSelector_WildcardUsesMapKey(t *testing.T) {
	t.Parallel()

	sel, err := BuildLabelSelector("shop", "review/pr-123")
	require.NoError(t, err)
	got := sel.String()
	assert.Contains(t, got, "deployah.dev/environment=review")
	assert.NotContains(t, got, "deployah.dev/environment=review-pr-123")
	assert.NotContains(t, got, "app.kubernetes.io/instance=")
}

func TestBuildSelector_InvalidWildcardRejected(t *testing.T) {
	t.Parallel()

	_, err := BuildSelector("shop", "api", "review/PR-123")
	require.Error(t, err)
	assert.ErrorContains(t, err, "lowercase DNS label")

	_, err = BuildLabelSelector("shop", "review/foo/bar")
	require.Error(t, err)
	assert.ErrorContains(t, err, "exactly one '/'")
}

func TestEnvironmentFromMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		annotations map[string]string
		want        string
	}{
		{name: "nil annotations"},
		{name: "empty map", annotations: map[string]string{}},
		{
			name: "plain production",
			annotations: map[string]string{
				spec.AnnotationEnvironmentInstance: "production",
			},
			want: "production",
		},
		{
			name: "wildcard original",
			annotations: map[string]string{
				spec.AnnotationEnvironmentInstance: "review/pr-123",
			},
			want: "review/pr-123",
		},
		{
			name: "long original",
			annotations: map[string]string{
				spec.AnnotationEnvironmentInstance: "review/" + strings.Repeat("a", 80),
			},
			want: "review/" + strings.Repeat("a", 80),
		},
		{
			name: "empty and whitespace annotation",
			annotations: map[string]string{
				spec.AnnotationEnvironmentInstance: "   ",
			},
		},
		{
			name: "malformed annotation is returned as stored",
			annotations: map[string]string{
				spec.AnnotationEnvironmentInstance: "not a valid env!!!",
			},
			want: "not a valid env!!!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, environmentFromMeta(tt.annotations))
		})
	}
}
