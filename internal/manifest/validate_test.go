package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestValidateComponentResources(t *testing.T) {
	tests := []struct {
		name      string
		component Component
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid with resources only",
			component: Component{
				Resources: Resources{
					CPU:    ptr.To("500m"),
					Memory: ptr.To("512Mi"),
				},
			},
			expectErr: false,
		},
		{
			name: "valid with resourcePreset only",
			component: Component{
				ResourcePreset: "small",
			},
			expectErr: false,
		},
		{
			name: "valid with neither (will use defaults)",
			component: Component{
				Image: "nginx:latest",
			},
			expectErr: false,
		},
		{
			name: "invalid with empty resources object",
			component: Component{
				Image: "nginx:latest",
				Resources: Resources{
					CPU: ptr.To(""), // Explicitly empty string
				},
			},
			expectErr: true,
			errMsg:    "component cannot have empty 'resources' object - either specify actual resource values or remove the resources field entirely",
		},
		{
			name: "invalid with both resources and resourcePreset",
			component: Component{
				Resources: Resources{
					CPU: ptr.To("500m"),
				},
				ResourcePreset: "small",
			},
			expectErr: true,
			errMsg:    "component cannot have both 'resources' and 'resourcePreset' fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateComponentResources(tt.component)
			if tt.expectErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateManifestComponents(t *testing.T) {
	tests := []struct {
		name      string
		manifest  *Manifest
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid manifest with single component",
			manifest: &Manifest{
				Components: map[string]Component{
					"web": {
						Resources: Resources{
							CPU: ptr.To("500m"),
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid manifest with conflicting component",
			manifest: &Manifest{
				Components: map[string]Component{
					"web": {
						Resources: Resources{
							CPU: ptr.To("500m"),
						},
						ResourcePreset: "small",
					},
				},
			},
			expectErr: true,
			errMsg:    "component web: component cannot have both 'resources' and 'resourcePreset' fields",
		},
		{
			name: "valid manifest with multiple components",
			manifest: &Manifest{
				Components: map[string]Component{
					"web": {
						Resources: Resources{
							CPU: ptr.To("500m"),
						},
					},
					"api": {
						ResourcePreset: "medium",
					},
					"worker": {
						Image: "worker:latest", // No resources specified
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid manifest with empty resources object",
			manifest: &Manifest{
				Components: map[string]Component{
					"web": {
						Image: "nginx:latest",
						Resources: Resources{
							CPU: ptr.To(""), // Explicitly empty string
						},
					},
				},
			},
			expectErr: true,
			errMsg:    "component web: component cannot have empty 'resources' object - either specify actual resource values or remove the resources field entirely",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateManifestComponents(tt.manifest)
			if tt.expectErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
