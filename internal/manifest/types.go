package manifest

// Manifest defines the structure of the project manifest.
type Manifest struct {
	ApiVersion   string               `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Project      string               `json:"project" yaml:"project"`
	Environments []Environment        `json:"environments,omitempty" yaml:"environments,omitempty"`
	Components   map[string]Component `json:"components" yaml:"components"`
}

// Environment defines the environment definition in the project.
type Environment struct {
	Name       string            `json:"name" yaml:"name"`
	EnvFile    string            `json:"envFile,omitempty" yaml:"envFile,omitempty"`
	ConfigFile string            `json:"configFile,omitempty" yaml:"configFile,omitempty"`
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
	Resources      Resources         `json:"resources" yaml:"resources"`
	ResourcePreset ResourcePreset    `json:"resourcePreset" yaml:"resourcePreset"`
	Ingress        Ingress           `json:"ingress" yaml:"ingress"`
	Env            map[string]string `json:"env" yaml:"env"`
}

// Autoscaling defines the autoscaling settings for the component.
type Autoscaling struct {
	Enabled        bool              `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	MinReplicas    int               `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	MaxReplicas    int               `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	Metrics        []Metric          `json:"metrics,omitempty" yaml:"metrics,omitempty"`
	Resources      Resources         `json:"resources" yaml:"resources"`
	ResourcePreset ResourcePreset    `json:"resourcePreset" yaml:"resourcePreset"`
	Ingress        Ingress           `json:"ingress" yaml:"ingress"`
	Env            map[string]string `json:"env" yaml:"env"`
}

// Metric defines a metric used to trigger autoscaling.
type Metric struct {
	Type   MetricType `json:"type" yaml:"type"`
	Target int        `json:"target" yaml:"target"`
}

// Resources defines the resource requests and limits for the component.
type Resources struct {
	CPU              string `json:"cpu" yaml:"cpu"`
	Memory           string `json:"memory" yaml:"memory"`
	EphemeralStorage string `json:"ephemeralStorage" yaml:"ephemeralStorage"`
}

// Ingress specifies the ingress settings for exposing the component via HTTP/HTTPS.
type Ingress struct {
	Host string `json:"host" yaml:"host"`
	TLS  bool   `json:"tls" yaml:"tls"`
}

// ComponentRole defines the role of the component within the application and determines the default deployment strategy.
type ComponentRole string

const (
	ComponentRoleService ComponentRole = "service"
	ComponentRoleWorker  ComponentRole = "worker"
	ComponentRoleJob     ComponentRole = "job"
)

// ComponentKind specifies the kind of the component.
type ComponentKind string

const (
	ComponentKindStateless ComponentKind = "stateless"
	ComponentKindStateful  ComponentKind = "stateful"
)

// MetricType specifies the type of metric to monitor.
type MetricType string

const (
	MetricTypeCPU    MetricType = "cpu"
	MetricTypeMemory MetricType = "memory"
)

// ResourcePreset specifies the resource preset for the component.
type ResourcePreset string

const (
	ResourcePresetNano    ResourcePreset = "nano"
	ResourcePresetMicro   ResourcePreset = "micro"
	ResourcePresetSmall   ResourcePreset = "small"
	ResourcePresetMedium  ResourcePreset = "medium"
	ResourcePresetLarge   ResourcePreset = "large"
	ResourcePresetXLarge  ResourcePreset = "xlarge"
	ResourcePreset2XLarge ResourcePreset = "2xlarge"
)
