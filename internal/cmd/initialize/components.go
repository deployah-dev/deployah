package initialize

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"k8s.io/apimachinery/pkg/api/resource"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/localkube"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/validate"
)

func validateComponentNameUnique(name string, existing map[string]spec.Component) error {
	if err := spec.ValidateComponentName(name); err != nil {
		return err
	}
	if _, exists := existing[name]; exists {
		return fmt.Errorf("component '%s' already exists", name)
	}
	return nil
}

// labeled is one select option: the spec value and the string shown in
// the prompt.
type labeled[T comparable] struct {
	value T
	label string
}

// labeledList is an ordered set of select options. Slice order is display
// order.
type labeledList[T comparable] []labeled[T]

// labels returns the display strings in slice order.
func (items labeledList[T]) labels() []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.label)
	}
	return out
}

// fromLabel reports the spec value for a display string. ok is false when
// label matches no option.
func (items labeledList[T]) fromLabel(label string) (T, bool) {
	for _, item := range items {
		if item.label == label {
			return item.value, true
		}
	}
	var zero T
	return zero, false
}

// label returns the display string for value, or empty if value is not in
// the list.
func (items labeledList[T]) label(value T) string {
	for _, item := range items {
		if item.value == value {
			return item.label
		}
	}
	return ""
}

var roles = labeledList[spec.ComponentRole]{
	{spec.ComponentRoleService, "service - handles HTTP requests (web apps, APIs)"},
	{spec.ComponentRoleWorker, "worker  - long-running background process, no HTTP (queue consumers)"},
}

var kinds = labeledList[spec.ComponentKind]{
	{spec.ComponentKindStateless, "stateless - remembers nothing on disk; replicas scale freely"},
	{spec.ComponentKindStateful, "stateful  - stable replica identity (optional persistent volume)"},
}

// customResourcesLabel is the merged resources select entry that opens the
// manual CPU/memory/storage form instead of picking a preset.
const customResourcesLabel = "Custom... (enter CPU/memory/storage manually)"

// presets lists resource presets smallest to largest. Labels are built at
// init so concurrent callers never race on Quantity.String against the
// shared [spec.ResourcePresetMappings] quantities.
var presets = labeledList[spec.ResourcePreset]{
	{spec.ResourcePresetNano, formatPresetLabel(spec.ResourcePresetNano)},
	{spec.ResourcePresetMicro, formatPresetLabel(spec.ResourcePresetMicro)},
	{spec.ResourcePresetSmall, formatPresetLabel(spec.ResourcePresetSmall)},
	{spec.ResourcePresetMedium, formatPresetLabel(spec.ResourcePresetMedium)},
	{spec.ResourcePresetLarge, formatPresetLabel(spec.ResourcePresetLarge)},
	{spec.ResourcePresetXLarge, formatPresetLabel(spec.ResourcePresetXLarge)},
	{spec.ResourcePreset2XLarge, formatPresetLabel(spec.ResourcePreset2XLarge)},
}

// formatPresetLabel formats a resource preset with its request values, e.g.
// "small - 500m CPU / 512Mi memory".
func formatPresetLabel(p spec.ResourcePreset) string {
	req := spec.ResourcePresetMappings[p]["requests"]
	cpu, memory := "?", "?"
	if req.CPU != nil {
		cpu = req.CPU.String()
	}
	if req.Memory != nil {
		memory = req.Memory.String()
	}
	return fmt.Sprintf("%s - %s CPU / %s memory", p, cpu, memory)
}

// presetLabel returns the display label for a resource preset. Unknown
// presets still render, with "?" for missing CPU and memory.
func presetLabel(p spec.ResourcePreset) string {
	if label := presets.label(p); label != "" {
		return label
	}
	return formatPresetLabel(p)
}

func collectComponents(c *nabat.Context, config *ProjectConfig) error {
	for {
		name, component, err := collectComponentDetails(c, config.EnvironmentNames, config.Components)
		if err != nil {
			if name == "" {
				return err
			}
			return fmt.Errorf("failed to collect details for component %s: %w", name, err)
		}
		config.Components[name] = component

		addAnother, confirmErr := c.Confirm(
			"Add another component?",
			nabat.WithAffirmative("Yes, add another"),
			nabat.WithNegative("No, I'm done"),
			nabat.WithPrefill(false),
		)
		if confirmErr != nil {
			return fmt.Errorf("failed to confirm another component: %w", confirmErr)
		}
		if !addAnother {
			return nil
		}
	}
}

// collectComponentDetails collects one component: name, essentials, then
// the advanced gate. The first component is collected before add-another
// is asked, so at least one component is always present.
func collectComponentDetails(c *nabat.Context, envNames []string, existing map[string]spec.Component) (string, spec.Component, error) {
	name, err := collectComponentName(c, existing)
	if err != nil {
		return "", spec.Component{}, err
	}
	var component spec.Component
	err = collectComponentEssentials(c, &component, name, envNames)
	if err != nil {
		return name, spec.Component{}, err
	}
	err = collectComponentAdvanced(c, &component, name, envNames)
	if err != nil {
		return name, spec.Component{}, err
	}
	return name, component, nil
}

func collectComponentName(c *nabat.Context, existing map[string]spec.Component) (string, error) {
	name, err := c.Input(
		"Component name ("+descComponentName+")",
		nabat.WithHint("web"),
		nabat.WithValidate(func(s string) error {
			return validateComponentNameUnique(s, existing)
		}),
	)
	if err != nil {
		return "", fmt.Errorf("failed to collect component name: %w", err)
	}
	return name, nil
}

// collectComponentEssentials asks role, image, port, resources, and expose
// as separate prompts so each form is height-homogeneous. Port and expose
// are asked only for services; expose only when local is selected.
func collectComponentEssentials(c *nabat.Context, component *spec.Component, name string, envNames []string) error {
	roleLabel, err := c.Select(
		fmt.Sprintf("Role for %s - how does this component run?", name),
		roles.labels(),
		roles.label(spec.ComponentRoleService),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component role: %w", err)
	}
	role, ok := roles.fromLabel(roleLabel)
	if !ok {
		return fmt.Errorf("unrecognized role selection %q", roleLabel)
	}

	image, err := c.Input(
		fmt.Sprintf(promptImageFmt, name),
		nabat.WithHint("nginx:latest"),
		nabat.WithValidate(func(s string) error {
			return validate.ValidateNonEmpty(s, "image")
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component image: %w", err)
	}

	answers := componentEssentialsAnswers{
		roleLabel: roleLabel,
		image:     image,
		askExpose: role.IsService() && slices.Contains(envNames, DefaultEnvironmentName),
	}

	if role.IsService() {
		portStr, portErr := c.Input(
			fmt.Sprintf("Port for %s", name),
			nabat.WithHint(strconv.Itoa(spec.DefaultServicePort)),
			nabat.WithPrefill(strconv.Itoa(spec.DefaultServicePort)),
			nabat.WithValidate(spec.ValidatePort),
		)
		if portErr != nil {
			return fmt.Errorf("failed to collect component port: %w", portErr)
		}
		answers.portStr = portStr
	}

	resourceLabel, err := c.Select(
		fmt.Sprintf("Resources for %s - Select a resource preset or enter custom values", name),
		append(presets.labels(), customResourcesLabel),
		presets.label(spec.ResourcePresetSmall),
	)
	if err != nil {
		return fmt.Errorf("failed to collect resources: %w", err)
	}
	answers.resourceLabel = resourceLabel

	if answers.askExpose {
		expose, exposeErr := c.Confirm(
			fmt.Sprintf(promptExposeFmt, name, name, localkube.DefaultIngressIP),
			nabat.WithAffirmative("Yes"),
			nabat.WithNegative("No"),
			nabat.WithPrefill(true),
		)
		if exposeErr != nil {
			return fmt.Errorf("failed to collect component expose: %w", exposeErr)
		}
		answers.expose = expose
	}

	custom, err := applyComponentEssentials(component, answers)
	if err != nil {
		return err
	}
	if !custom {
		return nil
	}
	return collectComponentCustomResources(c, component, name)
}

// componentEssentialsAnswers is the submitted state of the essentials prompts.
type componentEssentialsAnswers struct {
	roleLabel     string
	image         string
	portStr       string
	resourceLabel string
	expose        bool
	askExpose     bool
}

// applyComponentEssentials copies the essentials answers onto component.
// It returns true when the user chose custom resources so the caller can
// collect them.
func applyComponentEssentials(component *spec.Component, a componentEssentialsAnswers) (custom bool, err error) {
	role, ok := roles.fromLabel(a.roleLabel)
	if !ok {
		return false, fmt.Errorf("unrecognized role selection %q", a.roleLabel)
	}
	component.Role = role
	component.Image = a.image

	if role.IsService() {
		port, atoiErr := strconv.Atoi(a.portStr)
		if atoiErr != nil {
			return false, fmt.Errorf("invalid port number: %w", atoiErr)
		}
		component.Port = port
		if a.askExpose && a.expose {
			component.Expose = &spec.Expose{}
		}
	}

	if a.resourceLabel == customResourcesLabel {
		return true, nil
	}
	preset, ok := presets.fromLabel(a.resourceLabel)
	if !ok {
		return false, fmt.Errorf("unrecognized resource selection %q", a.resourceLabel)
	}
	component.ResourcePreset = preset
	return false, nil
}

// collectComponentAdvanced asks the remaining, optional questions behind a
// single "configure advanced options" gate.
func collectComponentAdvanced(c *nabat.Context, component *spec.Component, componentName string, availableEnvironments []string) error {
	advanced, err := c.Confirm(
		fmt.Sprintf(promptAdvancedFmt, componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, use defaults"),
		nabat.WithDefault(false),
	)
	if err != nil {
		return fmt.Errorf("failed to get advanced options preference: %w", err)
	}

	if !advanced {
		// Zero values for every advanced field stay omitted in the written
		// spec. Schema defaults (e.g. kind: stateless) apply at load time,
		// and a nil Environments means "active everywhere".
		return nil
	}

	return collectComponentAdvancedDetails(c, component, componentName, availableEnvironments)
}

// collectComponentAdvancedDetails asks the optional questions behind the
// advanced gate. The gate itself lives in [collectComponentAdvanced].
func collectComponentAdvancedDetails(c *nabat.Context, component *spec.Component, componentName string, availableEnvironments []string) error {
	if err := collectComponentKind(c, component, componentName); err != nil {
		return err
	}

	if component.Kind == spec.ComponentKindStateful {
		if err := collectComponentPersistence(c, component, componentName); err != nil {
			return err
		}
		if err := collectComponentReplicas(c, component, componentName); err != nil {
			return err
		}
		if component.Persistence != nil {
			c.Printf("Note: set a profile storageClass (or persistence.storageClass) in deployah.platform.yaml for this stateful component.\n")
		}
	}

	if component.Expose != nil {
		if err := collectComponentExposeOptions(c, component, componentName); err != nil {
			return err
		}
	}

	if err := collectComponentCommand(c, component, componentName); err != nil {
		return err
	}

	if err := collectComponentArgs(c, component, componentName); err != nil {
		return err
	}

	if component.Role.IsWorker() {
		if err := collectComponentMetricsPort(c, component, componentName); err != nil {
			return err
		}
	}

	if err := collectComponentConfigFiles(c, component, componentName); err != nil {
		return err
	}

	// Autoscaling is skipped for stateful in the init wizard; HPA remains
	// available via the written deployah.yaml for advanced users.
	if component.Kind != spec.ComponentKindStateful {
		if err := collectComponentAutoscaling(c, component, componentName); err != nil {
			return err
		}
	}

	if err := collectComponentEnvironmentVariables(c, component, componentName); err != nil {
		return err
	}

	if component.ListensOnPort() {
		if err := collectComponentHealth(c, component, componentName); err != nil {
			return err
		}
	}
	if component.Role.IsWorker() {
		if err := collectComponentExecHealth(c, component, componentName); err != nil {
			return err
		}
	}

	return collectComponentEnvironments(c, component, componentName, availableEnvironments)
}

func collectComponentKind(c *nabat.Context, component *spec.Component, componentName string) error {
	choice, err := c.Select(
		fmt.Sprintf("Kind for %s - Deployment or StatefulSet (stable identity)?", componentName),
		kinds.labels(),
		kinds.label(spec.ComponentKindStateless),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component kind: %w", err)
	}

	kind, ok := kinds.fromLabel(choice)
	if !ok {
		return fmt.Errorf("unrecognized kind selection %q", choice)
	}
	component.Kind = kind
	return nil
}

func collectComponentPersistence(c *nabat.Context, component *spec.Component, componentName string) error {
	addVolume, err := c.Confirm(
		fmt.Sprintf("Add a persistent volume for %s?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, identity only (no PVC)"),
	)
	if err != nil {
		return fmt.Errorf("failed to get persistence preference: %w", err)
	}
	if !addVolume {
		return nil
	}

	size, err := c.Input(
		fmt.Sprintf("Persistence size for %s", componentName),
		nabat.WithHint("20Gi"),
		nabat.WithDefault("20Gi"),
		nabat.WithValidate(func(s string) error {
			return validate.ValidateNonEmpty(s, "persistence.size")
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect persistence size: %w", err)
	}

	mountPath, err := c.Input(
		fmt.Sprintf("Persistence mount path for %s", componentName),
		nabat.WithHint("/data"),
		nabat.WithDefault("/data"),
		nabat.WithValidate(func(s string) error {
			if s == "" || s[0] != '/' {
				return fmt.Errorf("mountPath must be an absolute path starting with /")
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect persistence mount path: %w", err)
	}

	component.Persistence = &spec.Persistence{
		Size:      size,
		MountPath: mountPath,
	}
	return nil
}

func collectComponentReplicas(c *nabat.Context, component *spec.Component, componentName string) error {
	replicasStr, err := c.Input(
		fmt.Sprintf("Replicas for %s", componentName),
		nabat.WithHint("1"),
		nabat.WithDefault("1"),
		nabat.WithValidate(func(s string) error {
			n, atoiErr := strconv.Atoi(s)
			if atoiErr != nil || n < 1 {
				return fmt.Errorf("replicas must be an integer >= 1")
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect replicas: %w", err)
	}
	replicas, err := strconv.Atoi(replicasStr)
	if err != nil {
		return fmt.Errorf("invalid replicas: %w", err)
	}
	component.Replicas = &replicas
	return nil
}

func collectComponentConfigFiles(c *nabat.Context, component *spec.Component, componentName string) error {
	addConfigFiles, err := c.Confirm(
		fmt.Sprintf("Add component-specific config files for %s? Would you like to specify custom environment and config files for this component?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, use defaults"),
	)
	if err != nil {
		return fmt.Errorf("failed to get config files preference: %w", err)
	}

	if addConfigFiles {
		var envFile, configFile string
		err = c.Form(
			nabat.WithFormField(&envFile, fmt.Sprintf("Environment File for %s", componentName),
				"Component-specific environment file (optional)",
				nabat.WithHint(fmt.Sprintf(".env.%s", componentName)),
			),
			nabat.WithFormField(&configFile, fmt.Sprintf("Config File for %s", componentName),
				"Component-specific configuration file (optional)",
				nabat.WithHint(fmt.Sprintf("config.%s.yaml", componentName)),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to get config files: %w", err)
		}

		if envFile != "" {
			component.EnvFile = envFile
		}
		if configFile != "" {
			component.ConfigFile = configFile
		}
	}

	return nil
}

func collectComponentCommand(c *nabat.Context, component *spec.Component, componentName string) error {
	addCommand, err := c.Confirm(
		fmt.Sprintf("Add custom command for %s? Would you like to override the container's default command?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, use image default"),
		nabat.WithDefault(false),
	)
	if err != nil {
		return fmt.Errorf("failed to get command preference: %w", err)
	}

	if addCommand {
		commandStr, inputErr := c.Input(
			fmt.Sprintf("Command for %s - Command to run in the container (space-separated)", componentName),
			nabat.WithHint("python app.py"),
		)
		if inputErr != nil {
			return fmt.Errorf("failed to get command: %w", inputErr)
		}

		if commandStr != "" {
			tokens, splitErr := shlex.Split(commandStr)
			if splitErr != nil {
				return fmt.Errorf("failed to parse command: %w", splitErr)
			}
			component.Command = tokens
		}
	}

	return nil
}

func collectComponentArgs(c *nabat.Context, component *spec.Component, componentName string) error {
	addArgs, err := c.Confirm(
		fmt.Sprintf("Add arguments for %s? Would you like to add arguments to the command?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
		nabat.WithDefault(false),
	)
	if err != nil {
		return fmt.Errorf("failed to get args preference: %w", err)
	}

	if addArgs {
		argsStr, inputErr := c.Input(
			fmt.Sprintf("Arguments for %s - Arguments to pass to the command (space-separated)", componentName),
			nabat.WithHint("--port 8080 --debug"),
		)
		if inputErr != nil {
			return fmt.Errorf("failed to get arguments: %w", inputErr)
		}

		if argsStr != "" {
			tokens, splitErr := shlex.Split(argsStr)
			if splitErr != nil {
				return fmt.Errorf("failed to parse arguments: %w", splitErr)
			}
			component.Args = tokens
		}
	}

	return nil
}

func collectComponentAutoscaling(c *nabat.Context, component *spec.Component, componentName string) error {
	addAutoscaling, err := c.Confirm(
		fmt.Sprintf("Enable autoscaling for %s? Would you like to enable automatic scaling based on resource usage?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get autoscaling preference: %w", err)
	}

	if addAutoscaling {
		autoscaling := &spec.Autoscaling{
			Enabled: true,
		}

		var minReplicasStr, maxReplicasStr string
		err = c.Form(
			nabat.WithFormTitle(fmt.Sprintf("Autoscaling Configuration for %s", componentName)),
			nabat.WithFormField(&minReplicasStr, "Minimum Replicas",
				"Minimum number of replicas to maintain",
				nabat.WithHint(strconv.Itoa(spec.DefaultMinReplicas)),
				nabat.WithPrefill(strconv.Itoa(spec.DefaultMinReplicas)),
				nabat.WithValidate(func(s string) error { return validate.ValidatePositiveInteger(s, "minimum replicas") }),
			),
			nabat.WithFormField(&maxReplicasStr, "Maximum Replicas",
				"Maximum number of replicas allowed",
				nabat.WithHint(strconv.Itoa(spec.DefaultMaxReplicas)),
				nabat.WithPrefill(strconv.Itoa(spec.DefaultMaxReplicas)),
				nabat.WithValidate(func(s string) error { return validate.ValidatePositiveInteger(s, "maximum replicas") }),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to get autoscaling config: %w", err)
		}

		if err = validate.ValidateMinMaxReplicas(minReplicasStr, maxReplicasStr); err != nil {
			return fmt.Errorf("autoscaling configuration error: %w", err)
		}

		var minReplicas, maxReplicas int
		minReplicas, err = strconv.Atoi(minReplicasStr)
		if err != nil {
			return fmt.Errorf("invalid min replicas: %w", err)
		}
		maxReplicas, err = strconv.Atoi(maxReplicasStr)
		if err != nil {
			return fmt.Errorf("invalid max replicas: %w", err)
		}
		autoscaling.MinReplicas = minReplicas
		autoscaling.MaxReplicas = maxReplicas

		autoscaling.Metrics = []spec.Metric{
			{
				Type:   spec.MetricTypeCPU,
				Target: spec.DefaultCPUTarget,
			},
		}

		component.Autoscaling = autoscaling
	}

	return nil
}

// collectComponentCustomResources collects explicit CPU/memory/storage
// values. Called only when the resources select is customResourcesLabel.
func collectComponentCustomResources(c *nabat.Context, component *spec.Component, componentName string) error {
	var cpu, memory, ephemeralStorage string
	err := c.Form(
		nabat.WithFormTitle(fmt.Sprintf("Custom Resources for %s", componentName)),
		nabat.WithFormField(&cpu, "CPU",
			"CPU resource (e.g., 500m, 1)",
			nabat.WithHint("500m"),
			nabat.WithValidate(func(s string) error { return validate.ValidateResourceString(s, "CPU") }),
		),
		nabat.WithFormField(&memory, "Memory",
			"Memory resource (e.g., 512Mi, 1Gi)",
			nabat.WithHint("512Mi"),
			nabat.WithValidate(func(s string) error { return validate.ValidateResourceString(s, "Memory") }),
		),
		nabat.WithFormField(&ephemeralStorage, "Ephemeral Storage",
			"Ephemeral storage (e.g., 1Gi, 2Gi)",
			nabat.WithHint("1Gi"),
			nabat.WithValidate(func(s string) error { return validate.ValidateResourceString(s, "EphemeralStorage") }),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to get custom resources: %w", err)
	}

	resources := spec.Resources{}
	if cpu != "" {
		resources.CPU = new(resource.MustParse(cpu))
	}
	if memory != "" {
		resources.Memory = new(resource.MustParse(memory))
	}
	if ephemeralStorage != "" {
		resources.EphemeralStorage = new(resource.MustParse(ephemeralStorage))
	}

	component.Resources = resources
	return nil
}

// collectComponentExposeOptions customizes an exposed component's domain and
// subdomain; empty answers keep the platform defaults.
func collectComponentExposeOptions(c *nabat.Context, component *spec.Component, componentName string) error {
	var domain, subdomain string
	err := c.Form(
		nabat.WithFormField(&domain, fmt.Sprintf("Domain for %s (optional)", componentName),
			"Domain key from the platform file; leave empty for the environment's default domain",
			nabat.WithHint("public"),
		),
		nabat.WithFormField(&subdomain, fmt.Sprintf("Subdomain for %s (optional)", componentName),
			fmt.Sprintf("Hostname label; leave empty to use the component name (%s)", componentName),
			nabat.WithHint(componentName),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to get expose options: %w", err)
	}

	component.Expose.Domain = strings.TrimSpace(domain)
	if s := strings.TrimSpace(subdomain); s != "" {
		component.Expose.Subdomain = &s
	}
	return nil
}

// collectComponentHealth asks a single question: the HTTP health check path.
func collectComponentHealth(c *nabat.Context, component *spec.Component, componentName string) error {
	path, err := c.Input(
		fmt.Sprintf("HTTP health check path for %s (leave empty for a TCP check on the port)", componentName),
		nabat.WithHint("/healthz"),
		nabat.WithValidate(func(s string) error {
			if s != "" && s[0] != '/' {
				return fmt.Errorf("health check path must start with /")
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect health check path: %w", err)
	}

	if path == "" {
		// Leave Health unset: the engine falls back to an automatic TCP
		// probe on the port.
		return nil
	}

	// Upgrades both readiness and alive checks to HTTP probes on path.
	component.Health = &spec.Health{
		Ready: &spec.HealthReady{Path: path},
		Alive: &spec.HealthAlive{Path: path},
	}
	return nil
}

// collectComponentMetricsPort optionally asks for a Prometheus metrics port
// on workers (required when metrics are enabled).
func collectComponentMetricsPort(c *nabat.Context, component *spec.Component, componentName string) error {
	enable, err := c.Confirm(
		fmt.Sprintf("Expose Prometheus metrics for %s?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
		nabat.WithDefault(false),
	)
	if err != nil {
		return fmt.Errorf("failed to collect metrics preference: %w", err)
	}
	if !enable {
		return nil
	}
	portStr, err := c.Input(
		fmt.Sprintf("Metrics port for %s", componentName),
		nabat.WithHint("9090"),
		nabat.WithDefault("9090"),
		nabat.WithValidate(spec.ValidatePort),
	)
	if err != nil {
		return fmt.Errorf("failed to collect metrics port: %w", err)
	}
	return applyCollectedMetricsPort(component, portStr)
}

// applyCollectedMetricsPort sets metrics.port from a wizard answer.
func applyCollectedMetricsPort(component *spec.Component, portStr string) error {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid metrics port: %w", err)
	}
	component.Metrics = &spec.ComponentMetrics{Port: port}
	return nil
}

// collectComponentExecHealth optionally asks for an exec liveness command on
// workers (process-exit is the default when omitted).
func collectComponentExecHealth(c *nabat.Context, component *spec.Component, componentName string) error {
	enable, err := c.Confirm(
		fmt.Sprintf("Add an exec alive check for %s? (default is process-exit only)", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
		nabat.WithDefault(false),
	)
	if err != nil {
		return fmt.Errorf("failed to collect exec health preference: %w", err)
	}
	if !enable {
		return nil
	}
	cmdStr, err := c.Input(
		fmt.Sprintf("Alive exec command for %s (space-separated)", componentName),
		nabat.WithHint("pgrep -f worker"),
		nabat.WithDefault("pgrep -f worker"),
		nabat.WithValidate(func(s string) error {
			return validate.ValidateNonEmpty(s, "exec command")
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect exec health command: %w", err)
	}
	return applyCollectedExecHealth(component, cmdStr)
}

// applyCollectedExecHealth sets health.alive.exec from a space-separated
// wizard answer.
func applyCollectedExecHealth(component *spec.Component, cmdStr string) error {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return fmt.Errorf("exec command must not be empty")
	}
	component.Health = &spec.Health{
		Alive: &spec.HealthAlive{Exec: parts},
	}
	return nil
}

func collectComponentEnvironmentVariables(c *nabat.Context, component *spec.Component, componentName string) error {
	addComponentEnvVars, err := c.Confirm(
		fmt.Sprintf("Add environment variables for %s? Would you like to add component-specific environment variables?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get component env preference: %w", err)
	}

	if addComponentEnvVars {
		envVars, envErr := collectEnvironmentVariables(c)
		if envErr != nil {
			return fmt.Errorf("failed to collect component environment variables: %w", envErr)
		}
		component.Env = envVars
	}

	return nil
}

func collectComponentEnvironments(c *nabat.Context, component *spec.Component, componentName string, availableEnvironments []string) error {
	if len(availableEnvironments) == 0 {
		return fmt.Errorf("no environments available for component deployment")
	}

	if len(availableEnvironments) == 1 {
		component.Environments = slices.Clone(availableEnvironments)
		return nil
	}

	selectedEnvs, err := c.MultiSelect(
		fmt.Sprintf("Environment Selection for %s - Select one or more environments for this component", componentName),
		availableEnvironments,
		availableEnvironments,
	)
	if err != nil {
		return fmt.Errorf("failed to collect environment selection: %w", err)
	}

	if len(selectedEnvs) == 0 {
		return fmt.Errorf("at least one environment must be selected for component %s", componentName)
	}

	component.Environments = selectedEnvs
	return nil
}
