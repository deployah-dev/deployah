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
	"encoding/json"
	"fmt"
)

// Spec defines the structure of the project spec.
type Spec struct {
	// APIVersion is the schema version of the spec (e.g., "v1-alpha.1").
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	// Project is the project name.
	Project string `json:"project" yaml:"project"`
	// Environments is a list of environment definitions.
	Environments []Environment `json:"environments,omitempty" yaml:"environments,omitempty"`
	// Components is a map of component names to their configuration.
	Components map[string]Component `json:"components" yaml:"components"`
}

// EnvironmentNames returns the list of environment names defined in the spec.
// Returns an empty slice if no environments are defined.
func (m *Spec) EnvironmentNames() []string {
	if len(m.Environments) == 0 {
		return []string{}
	}

	names := make([]string, 0, len(m.Environments))
	for _, env := range m.Environments {
		if env.Name != "" {
			names = append(names, env.Name)
		}
	}

	return names
}

// Environment defines a named deployment target and its configuration sources.
type Environment struct {
	// Name is the environment identifier (e.g. "staging").
	Name string `json:"name" yaml:"name"`
	// EnvFile is the path to a dotenv file for this environment.
	EnvFile string `json:"envFile,omitempty" yaml:"envFile,omitempty"`
	// ConfigFile is the path to an environment-specific config file.
	ConfigFile string `json:"configFile,omitempty" yaml:"configFile,omitempty"`
	// Context is the Kubernetes context to use for this environment.
	Context string `json:"context,omitempty" yaml:"context,omitempty"`
	// Variables holds inline key-value overrides for this environment.
	Variables map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// Component defines a deployable unit in the project.
type Component struct {
	// Role selects the default deployment strategy for the component.
	Role ComponentRole `json:"role,omitempty" yaml:"role,omitempty"`
	// EnvFile is the path to a component-specific dotenv file.
	EnvFile string `json:"envFile,omitempty" yaml:"envFile,omitempty"`
	// ConfigFile is the path to a component-specific config file.
	ConfigFile string `json:"configFile,omitempty" yaml:"configFile,omitempty"`
	// Environments limits the component to the named environments.
	Environments []string `json:"environments,omitempty" yaml:"environments,omitempty"`
	// Kind selects stateless or stateful deployment behavior.
	Kind ComponentKind `json:"kind,omitempty" yaml:"kind,omitempty"`
	// Image is the container image reference.
	Image string `json:"image" yaml:"image"`
	// Command overrides the container entrypoint.
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
	// Args overrides the container command arguments.
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
	// Port is the primary container port for services.
	Port int `json:"port,omitempty" yaml:"port,omitempty"`
	// Autoscaling configures horizontal pod autoscaling.
	Autoscaling *Autoscaling `json:"autoscaling,omitempty" yaml:"autoscaling,omitempty"`
	// Resources sets explicit CPU, memory, and storage requests and limits.
	Resources Resources `json:"resources" yaml:"resources,omitempty"`
	// ResourcePreset selects a named resource profile when Resources is empty.
	ResourcePreset ResourcePreset `json:"resourcePreset,omitempty" yaml:"resourcePreset,omitempty"`
	// Ingress exposes the component through an HTTP or HTTPS route.
	Ingress *Ingress `json:"ingress,omitempty" yaml:"ingress,omitempty"`
	// Env sets static environment variables for the container.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// Health configures ready and alive checks for the component.
	Health *Health `json:"health,omitempty" yaml:"health,omitempty"`
}

// Health configures HTTP health checks for a service component. When omitted,
// TCP checks on the component port run automatically.
type Health struct {
	// Ready controls the readiness check. Provide a path to upgrade from TCP
	// to HTTP. Set to false to disable readiness and startup checks entirely.
	Ready *HealthReady `json:"ready,omitempty" yaml:"ready,omitempty"`
	// Alive controls the alive check. Provide a path to upgrade from TCP to
	// HTTP. Set to false to disable the alive check entirely.
	Alive *HealthAlive `json:"alive,omitempty" yaml:"alive,omitempty"`
}

// HealthReady configures the readiness check for a service component. It
// accepts either false (to disable) or an object with a path.
//
// When Ready is nil (field absent), a TCP readiness check on the component
// port runs automatically.
type HealthReady struct {
	// Disabled is true when the developer set ready: false.
	Disabled bool `json:"-" yaml:"-"`
	// Path is the HTTP endpoint that must return 2xx for the component to
	// receive traffic. Must start with /.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

// UnmarshalJSON handles both false and object forms:
//
//	ready: false         -> HealthReady{Disabled: true}
//	ready: {path: /h}   -> HealthReady{Path: "/h"}
func (r *HealthReady) UnmarshalJSON(data []byte) error {
	// Check for boolean false.
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if !b {
			r.Disabled = true
			return nil
		}
		return fmt.Errorf("health.ready: true is not valid; omit the field to enable the default TCP check")
	}

	// Unmarshal as object using an alias to avoid infinite recursion.
	type healthReadyAlias HealthReady
	var alias healthReadyAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("health.ready: expected false or an object with a path field: %w", err)
	}
	*r = HealthReady(alias)
	return nil
}

// HealthAlive configures the alive check for a service component. It accepts
// either false (to disable) or an object with a path and optional timing.
//
// When Alive is nil (field absent), a TCP alive check on the component port
// runs automatically.
type HealthAlive struct {
	// Disabled is true when the developer set alive: false.
	Disabled bool `json:"-" yaml:"-"`
	// Path is the HTTP endpoint that must return 2xx for the pod to be
	// considered alive. Must start with /. Check only internal process
	// state here, not external dependencies.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// Interval is how often to check the endpoint (e.g. "10s", "1m").
	// Defaults to "10s" when omitted.
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	// RestartAfter is how long the endpoint must fail continuously before
	// the pod is restarted (e.g. "60s", "2m"). Defaults to "60s" when
	// omitted. The effective window rounds up to the nearest multiple of
	// Interval.
	RestartAfter string `json:"restartAfter,omitempty" yaml:"restartAfter,omitempty"`
}

// UnmarshalJSON handles both false and object forms:
//
//	alive: false                           -> HealthAlive{Disabled: true}
//	alive: {path: /livez, interval: 10s}  -> HealthAlive{Path: "/livez", ...}
func (a *HealthAlive) UnmarshalJSON(data []byte) error {
	// Check for boolean false.
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		if !b {
			a.Disabled = true
			return nil
		}
		return fmt.Errorf("health.alive: true is not valid; omit the field to enable the default TCP check")
	}

	// Unmarshal as object using an alias to avoid infinite recursion.
	type healthAliveAlias HealthAlive
	var alias healthAliveAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("health.alive: expected false or an object with a path field: %w", err)
	}
	*a = HealthAlive(alias)
	return nil
}

// Autoscaling defines the autoscaling settings for the component.
type Autoscaling struct {
	Enabled     bool     `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	MinReplicas int      `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	MaxReplicas int      `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	Metrics     []Metric `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// Metric defines a metric used to trigger autoscaling.
type Metric struct {
	Type   MetricType `json:"type" yaml:"type"`
	Target int        `json:"target" yaml:"target"`
}

// Resources defines the resource requests and limits for the component.
type Resources struct {
	CPU              *string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory           *string `json:"memory,omitempty" yaml:"memory,omitempty"`
	EphemeralStorage *string `json:"ephemeralStorage,omitempty" yaml:"ephemeralStorage,omitempty"`
}

// Ingress specifies ingress settings for exposing the component via HTTP/HTTPS.
type Ingress struct {
	Host string `json:"host" yaml:"host"`
	TLS  bool   `json:"tls" yaml:"tls"`
}

// ComponentRole defines the role of a component and its default deployment
// strategy.
type ComponentRole string

const (
	// ComponentRoleService runs a long-lived HTTP or network service.
	ComponentRoleService ComponentRole = "service"
	// ComponentRoleWorker runs background or queue-processing workloads.
	ComponentRoleWorker ComponentRole = "worker"
	// ComponentRoleJob runs a finite batch or one-off task.
	ComponentRoleJob ComponentRole = "job"
)

// ComponentKind specifies the kind of the component.
type ComponentKind string

const (
	// ComponentKindStateless runs replicas that do not require stable storage.
	ComponentKindStateless ComponentKind = "stateless"
	// ComponentKindStateful runs replicas that require stable storage.
	ComponentKindStateful ComponentKind = "stateful"
)

// MetricType specifies the type of metric to monitor.
type MetricType string

const (
	// MetricTypeCPU scales on CPU utilization.
	MetricTypeCPU MetricType = "cpu"
	// MetricTypeMemory scales on memory utilization.
	MetricTypeMemory MetricType = "memory"
)

// ResourcePreset specifies the resource preset for the component.
type ResourcePreset string

const (
	// ResourcePresetNano is the smallest resource preset.
	ResourcePresetNano ResourcePreset = "nano"
	// ResourcePresetMicro is a very small resource preset.
	ResourcePresetMicro ResourcePreset = "micro"
	// ResourcePresetSmall is a small resource preset.
	ResourcePresetSmall ResourcePreset = "small"
	// ResourcePresetMedium is a medium resource preset.
	ResourcePresetMedium ResourcePreset = "medium"
	// ResourcePresetLarge is a large resource preset.
	ResourcePresetLarge ResourcePreset = "large"
	// ResourcePresetXLarge is an extra-large resource preset.
	ResourcePresetXLarge ResourcePreset = "xlarge"
	// ResourcePreset2XLarge is a double extra-large resource preset.
	ResourcePreset2XLarge ResourcePreset = "2xlarge"
)
