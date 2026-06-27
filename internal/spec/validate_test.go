package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateComponentResources verifies ValidateComponentResources rules.
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
					CPU:    new("500m"),
					Memory: new("512Mi"),
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
					CPU: new(""), // Explicitly empty string
				},
			},
			expectErr: true,
			errMsg:    "component cannot have empty 'resources' object - either specify actual resource values or remove the resources field entirely",
		},
		{
			name: "invalid with both resources and resourcePreset",
			component: Component{
				Resources: Resources{
					CPU: new("500m"),
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

// TestValidateSpecComponents verifies ValidateSpecComponents across
// manifest component configurations.
func TestValidateSpecComponents(t *testing.T) {
	tests := []struct {
		name      string
		manifest  *Spec
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid manifest with single component",
			manifest: &Spec{
				Components: map[string]Component{
					"web": {
						Resources: Resources{
							CPU: new("500m"),
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "invalid manifest with conflicting component",
			manifest: &Spec{
				Components: map[string]Component{
					"web": {
						Resources: Resources{
							CPU: new("500m"),
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
			manifest: &Spec{
				Components: map[string]Component{
					"web": {
						Resources: Resources{
							CPU: new("500m"),
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
			manifest: &Spec{
				Components: map[string]Component{
					"web": {
						Image: "nginx:latest",
						Resources: Resources{
							CPU: new(""), // Explicitly empty string
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
			err := ValidateSpecComponents(tt.manifest)
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

// TestValidateComponentHealth verifies ValidateComponentHealth rules.
func TestValidateComponentHealth(t *testing.T) {
	t.Parallel()

	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name      string
		component Component
		expectErr bool
		errMsg    string
	}{
		{
			name:      "health nil is valid (zero config)",
			component: Component{Role: ComponentRoleService},
			expectErr: false,
		},
		{
			name: "ready with path is valid",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Ready: &HealthReady{Path: "/health"},
				},
			},
			expectErr: false,
		},
		{
			name: "alive with path, interval, restartAfter is valid",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Alive: &HealthAlive{
						Path:         "/livez",
						Interval:     "10s",
						RestartAfter: "60s",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "ready false and alive false is valid",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Ready: &HealthReady{Disabled: true},
					Alive: &HealthAlive{Disabled: true},
				},
			},
			expectErr: false,
		},
		{
			name: "health on worker role is invalid",
			component: Component{
				Role:   ComponentRoleWorker,
				Health: &Health{Ready: &HealthReady{Path: "/health"}},
			},
			expectErr: true,
			errMsg:    "health checks are only supported for role: service",
		},
		{
			name: "health on job role is invalid",
			component: Component{
				Role:   ComponentRoleJob,
				Health: &Health{Ready: &HealthReady{Path: "/health"}},
			},
			expectErr: true,
			errMsg:    "health checks are only supported for role: service",
		},
		{
			name: "ready path without leading slash is invalid",
			component: Component{
				Role:   ComponentRoleService,
				Health: &Health{Ready: &HealthReady{Path: "health"}},
			},
			expectErr: true,
			errMsg:    "health.ready.path must start with /",
		},
		{
			name: "alive path without leading slash is invalid",
			component: Component{
				Role:   ComponentRoleService,
				Health: &Health{Alive: &HealthAlive{Path: "livez"}},
			},
			expectErr: true,
			errMsg:    "health.alive.path must start with /",
		},
		{
			name: "alive interval zero is invalid",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Alive: &HealthAlive{
						Path:     "/livez",
						Interval: "0s",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "alive restartAfter less than interval is invalid",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Alive: &HealthAlive{
						Path:         "/livez",
						Interval:     "60s",
						RestartAfter: "10s",
					},
				},
			},
			expectErr: true,
			errMsg:    "must be greater than or equal to",
		},
		{
			name: "alive restartAfter equal to interval is valid",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Alive: &HealthAlive{
						Path:         "/livez",
						Interval:     "10s",
						RestartAfter: "10s",
					},
				},
			},
			expectErr: false,
		},
		{
			name: "alive invalid interval format",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Alive: &HealthAlive{
						Path:     "/livez",
						Interval: "10",
					},
				},
			},
			expectErr: true,
		},
		{
			name: "resources and health both valid on service",
			component: Component{
				Role: ComponentRoleService,
				Resources: Resources{
					CPU: strPtr("500m"),
				},
				Health: &Health{
					Ready: &HealthReady{Path: "/health"},
					Alive: &HealthAlive{Path: "/livez"},
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentHealth(tt.component)
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
