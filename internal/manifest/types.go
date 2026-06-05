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

package manifest

// Manifest defines the structure of the project manifest.
type Manifest struct {
	// APIVersion is the schema version of the manifest (e.g., "v1-alpha.1").
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	// Project is the project name.
	Project string `json:"project" yaml:"project"`
	// Environments is a list of environment definitions.
	Environments []Environment `json:"environments,omitempty" yaml:"environments,omitempty"`
	// Components is a map of component names to their configuration.
	Components map[string]Component `json:"components" yaml:"components"`
}

// EnvironmentNames returns the list of environment names defined in the manifest.
// Returns an empty slice if no environments are defined.
func (m *Manifest) EnvironmentNames() []string {
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

// Environment defines the environment definition in the project.
type Environment struct {
	Name       string            `json:"name" yaml:"name"`
	EnvFile    string            `json:"envFile,omitempty" yaml:"envFile,omitempty"`
	ConfigFile string            `json:"configFile,omitempty" yaml:"configFile,omitempty"`
	Context    string            `json:"context,omitempty" yaml:"context,omitempty"`
	Variables  map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`
}

// Component defines a deployable unit in the project.
type Component struct {
	Role           ComponentRole     `json:"role,omitempty" yaml:"role,omitempty"`
	EnvFile        string            `json:"envFile,omitempty" yaml:"envFile,omitempty"`
	ConfigFile     string            `json:"configFile,omitempty" yaml:"configFile,omitempty"`
	Environments   []string          `json:"environments,omitempty" yaml:"environments,omitempty"`
	Kind           ComponentKind     `json:"kind,omitempty" yaml:"kind,omitempty"`
	Image          string            `json:"image" yaml:"image"`
	Command        []string          `json:"command,omitempty" yaml:"command,omitempty"`
	Args           []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Port           int               `json:"port,omitempty" yaml:"port,omitempty"`
	Autoscaling    *Autoscaling      `json:"autoscaling,omitempty" yaml:"autoscaling,omitempty"`
	Resources      Resources         `json:"resources" yaml:"resources,omitempty"`
	ResourcePreset ResourcePreset    `json:"resourcePreset,omitempty" yaml:"resourcePreset,omitempty"`
	Ingress        *Ingress          `json:"ingress,omitempty" yaml:"ingress,omitempty"`
	Env            map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
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
