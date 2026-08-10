// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
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
					CPU:    MustQuantity("500m"),
					Memory: MustQuantity("512Mi"),
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
					CPU: &resource.Quantity{}, // Explicit zero quantity
				},
			},
			expectErr: true,
			errMsg:    "component cannot have empty 'resources' object - either specify actual resource values or remove the resources field entirely",
		},
		{
			name: "invalid with both resources and resourcePreset",
			component: Component{
				Resources: Resources{
					CPU: MustQuantity("500m"),
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
							CPU: MustQuantity("500m"),
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
							CPU: MustQuantity("500m"),
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
							CPU: MustQuantity("500m"),
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
							CPU: &resource.Quantity{}, // Explicit zero quantity
						},
					},
				},
			},
			expectErr: true,
			errMsg:    "component web: component cannot have empty 'resources' object - either specify actual resource values or remove the resources field entirely",
		},
		{
			name: "invalid empty profile name",
			manifest: &Spec{
				Components: map[string]Component{
					"web": {
						Profiles: []string{"public-web", "  "},
					},
				},
			},
			expectErr: true,
			errMsg:    "profiles[1]: profile name must not be empty",
		},
		{
			name: "valid profiles array",
			manifest: &Spec{
				Components: map[string]Component{
					"web": {
						Profiles: []string{"public-web", "high-security"},
					},
				},
			},
			expectErr: false,
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

// TestValidateComponentExpose verifies apex and subdomain are mutually
// exclusive.
func TestValidateComponentExpose(t *testing.T) {
	t.Parallel()

	sub := "api"
	assert.NoError(t, ValidateComponentExpose(Component{}))
	assert.NoError(t, ValidateComponentExpose(Component{Expose: &Expose{Apex: true}}))
	assert.NoError(t, ValidateComponentExpose(Component{Expose: &Expose{Subdomain: &sub}}))

	err := ValidateComponentExpose(Component{Expose: &Expose{Apex: true, Subdomain: &sub}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// TestValidateComponentAutoscaling verifies replica bounds and metric types.
func TestValidateComponentAutoscaling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component Component
		wantErr   string
	}{
		{
			name:      "nil autoscaling",
			component: Component{},
		},
		{
			name: "disabled autoscaling",
			component: Component{
				Autoscaling: &Autoscaling{Enabled: false, MinReplicas: 5, MaxReplicas: 1},
			},
		},
		{
			name: "valid cpu and memory metrics",
			component: Component{
				Autoscaling: &Autoscaling{
					Enabled:     true,
					MinReplicas: 1,
					MaxReplicas: 5,
					Metrics: []Metric{
						{Type: MetricTypeCPU, Target: 70},
						{Type: MetricTypeMemory, Target: 80},
					},
				},
			},
		},
		{
			name: "minReplicas greater than maxReplicas",
			component: Component{
				Autoscaling: &Autoscaling{Enabled: true, MinReplicas: 5, MaxReplicas: 2},
			},
			wantErr: "minReplicas cannot be greater than maxReplicas",
		},
		{
			name: "unsupported metric type",
			component: Component{
				Autoscaling: &Autoscaling{
					Enabled:     true,
					MinReplicas: 1,
					MaxReplicas: 2,
					Metrics:     []Metric{{Type: MetricType("qps"), Target: 100}},
				},
			},
			wantErr: `unsupported metric type "qps"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentAutoscaling(tt.component)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateComponentPersistence verifies stateful/stateless persistence rules.
func TestValidateComponentPersistence(t *testing.T) {
	t.Parallel()

	replicas2 := 2
	tests := []struct {
		name      string
		component Component
		wantErr   string
	}{
		{
			name:      "stateless without persistence",
			component: Component{Kind: ComponentKindStateless},
		},
		{
			name: "stateful with persistence",
			component: Component{
				Kind: ComponentKindStateful,
				Persistence: &Persistence{
					Size:      "20Gi",
					MountPath: "/data",
				},
			},
		},
		{
			name:      "stateful without persistence (identity only)",
			component: Component{Kind: ComponentKindStateful},
		},
		{
			name: "stateful missing size",
			component: Component{
				Kind:        ComponentKindStateful,
				Persistence: &Persistence{MountPath: "/data"},
			},
			wantErr: "persistence.size is required when persistence is set",
		},
		{
			name: "stateful missing mountPath",
			component: Component{
				Kind:        ComponentKindStateful,
				Persistence: &Persistence{Size: "20Gi"},
			},
			wantErr: "persistence.mountPath is required when persistence is set",
		},
		{
			name: "stateless persistence with replicas > 1",
			component: Component{
				Kind:        ComponentKindStateless,
				Replicas:    &replicas2,
				Persistence: &Persistence{Size: "1Gi", MountPath: "/data"},
			},
			wantErr: "cannot set replicas > 1",
		},
		{
			name: "stateless persistence with autoscaling",
			component: Component{
				Kind: ComponentKindStateless,
				Persistence: &Persistence{
					Size:      "1Gi",
					MountPath: "/data",
				},
				Autoscaling: &Autoscaling{Enabled: true, MinReplicas: 1, MaxReplicas: 3},
			},
			wantErr: "cannot enable autoscaling",
		},
		{
			name: "stateless persistence allowed with one replica",
			component: Component{
				Kind: ComponentKindStateless,
				Persistence: &Persistence{
					Size:      "1Gi",
					MountPath: "/data",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentPersistence(tt.component)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateComponentReplicas verifies replicas vs autoscaling mutual exclusion.
func TestValidateComponentReplicas(t *testing.T) {
	t.Parallel()

	replicas1 := 1
	replicas0 := 0
	tests := []struct {
		name      string
		component Component
		wantErr   string
	}{
		{
			name:      "nil replicas",
			component: Component{},
		},
		{
			name: "replicas without autoscaling",
			component: Component{
				Replicas: &replicas1,
			},
		},
		{
			name: "replicas with autoscaling enabled",
			component: Component{
				Replicas:    &replicas1,
				Autoscaling: &Autoscaling{Enabled: true, MinReplicas: 1, MaxReplicas: 3},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "replicas with autoscaling disabled",
			component: Component{
				Replicas:    &replicas1,
				Autoscaling: &Autoscaling{Enabled: false},
			},
		},
		{
			name: "replicas less than 1",
			component: Component{
				Replicas: &replicas0,
			},
			wantErr: "at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentReplicas(tt.component)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestValidateComponentEnvironmentFilter rejects unsupported /* suffixes.
func TestValidateComponentEnvironmentFilter(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidateComponentEnvironmentFilter(Component{}))
	assert.NoError(t, ValidateComponentEnvironmentFilter(Component{
		Environments: []string{"production", "staging"},
	}))

	err := ValidateComponentEnvironmentFilter(Component{
		Environments: []string{"preview/*"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"preview/*"`)
	assert.Contains(t, err.Error(), `use "preview"`)
}

// TestValidateComponentHealth verifies ValidateComponentHealth rules.
func TestValidateComponentHealth(t *testing.T) {
	t.Parallel()

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
				Port: 8080,
				Health: &Health{
					Ready: &HealthReady{Path: "/health"},
				},
			},
			expectErr: false,
		},
		{
			name: "ready with path and no port is valid (schema defaults port)",
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
				Port: 8080,
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
			name: "alive with path and no port is valid (schema defaults port)",
			component: Component{
				Role: ComponentRoleService,
				Health: &Health{
					Alive: &HealthAlive{Path: "/livez"},
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
			name: "worker with exec alive is valid",
			component: Component{
				Role:   ComponentRoleWorker,
				Health: &Health{Alive: &HealthAlive{Exec: []string{"pgrep", "-f", "worker"}}},
			},
			expectErr: false,
		},
		{
			name: "path and exec on alive are mutually exclusive",
			component: Component{
				Role: ComponentRoleService,
				Port: 8080,
				Health: &Health{
					Alive: &HealthAlive{Path: "/livez", Exec: []string{"true"}},
				},
			},
			expectErr: true,
			errMsg:    "mutually exclusive",
		},
		{
			name: "empty exec part is invalid",
			component: Component{
				Role: ComponentRoleWorker,
				Health: &Health{
					Alive: &HealthAlive{Exec: []string{"pgrep", "  "}},
				},
			},
			expectErr: true,
			errMsg:    "health.alive.exec[1] must not be empty",
		},
		{
			name: "health on job role is invalid",
			component: Component{
				Role:   ComponentRoleJob,
				Health: &Health{Ready: &HealthReady{Path: "/health"}},
			},
			expectErr: true,
			errMsg:    "health checks are not supported for role: job",
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
				Port: 8080,
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
				Port: 8080,
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
				Port: 8080,
				Resources: Resources{
					CPU: MustQuantity("500m"),
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

func TestValidateComponentWorker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		component Component
		expectErr bool
		errMsg    string
	}{
		{
			name:      "service is ignored",
			component: Component{Role: ComponentRoleService, Port: 8080},
		},
		{
			name:      "worker without forbidden fields is valid",
			component: Component{Role: ComponentRoleWorker, Image: "worker:1"},
		},
		{
			name:      "worker with port is invalid",
			component: Component{Role: ComponentRoleWorker, Port: 8080},
			expectErr: true,
			errMsg:    "port is not supported on role: worker",
		},
		{
			name:      "worker with expose is invalid",
			component: Component{Role: ComponentRoleWorker, Expose: &Expose{}},
			expectErr: true,
			errMsg:    "expose is not supported on role: worker",
		},
		{
			name: "worker with health.ready is invalid",
			component: Component{
				Role:   ComponentRoleWorker,
				Health: &Health{Ready: &HealthReady{Path: "/health"}},
			},
			expectErr: true,
			errMsg:    "health.ready is not supported",
		},
		{
			name: "worker with health.alive.path is invalid",
			component: Component{
				Role:   ComponentRoleWorker,
				Health: &Health{Alive: &HealthAlive{Path: "/livez"}},
			},
			expectErr: true,
			errMsg:    "health.alive.path is not supported",
		},
		{
			name: "worker with health.alive.exec is valid",
			component: Component{
				Role:   ComponentRoleWorker,
				Health: &Health{Alive: &HealthAlive{Exec: []string{"true"}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentWorker(tt.component)
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateComponentMetrics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		component Component
		expectErr bool
		errMsg    string
	}{
		{
			name:      "nil metrics is valid",
			component: Component{Role: ComponentRoleService},
		},
		{
			name: "worker metrics true without port is invalid",
			component: Component{
				Role:    ComponentRoleWorker,
				Metrics: &ComponentMetrics{},
			},
			expectErr: true,
			errMsg:    "metrics.port is required",
		},
		{
			name: "worker metrics with port is valid",
			component: Component{
				Role:    ComponentRoleWorker,
				Metrics: &ComponentMetrics{Port: 9090},
			},
		},
		{
			name: "service metrics without port is valid",
			component: Component{
				Role:    ComponentRoleService,
				Port:    8080,
				Metrics: &ComponentMetrics{},
			},
		},
		{
			name: "metrics path must start with slash",
			component: Component{
				Role:    ComponentRoleService,
				Metrics: &ComponentMetrics{Path: "metrics"},
			},
			expectErr: true,
			errMsg:    "metrics.path must start with /",
		},
		{
			name: "disabled metrics skips port check",
			component: Component{
				Role:    ComponentRoleWorker,
				Metrics: &ComponentMetrics{Disabled: true},
			},
		},
		{
			name: "invalid metrics interval",
			component: Component{
				Role:    ComponentRoleService,
				Metrics: &ComponentMetrics{Interval: "not-a-duration"},
			},
			expectErr: true,
			errMsg:    "metrics.interval",
		},
		{
			name: "invalid metrics scrapeTimeout",
			component: Component{
				Role:    ComponentRoleService,
				Metrics: &ComponentMetrics{ScrapeTimeout: "nope"},
			},
			expectErr: true,
			errMsg:    "metrics.scrapeTimeout",
		},
		{
			name: "metrics port out of range",
			component: Component{
				Role:    ComponentRoleService,
				Metrics: &ComponentMetrics{Port: 70000},
			},
			expectErr: true,
			errMsg:    "metrics.port must be between 1 and 65535",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentMetrics(tt.component)
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateComponentShutdownTimeout(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateComponentShutdownTimeout(Component{}))
	assert.NoError(t, ValidateComponentShutdownTimeout(Component{ShutdownTimeout: "60s"}))
	err := ValidateComponentShutdownTimeout(Component{ShutdownTimeout: "0s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdownTimeout")

	err = ValidateComponentShutdownTimeout(Component{ShutdownTimeout: "not-a-duration"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdownTimeout")
}
