package initialize

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"k8s.io/apimachinery/pkg/api/resource"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/util"
)

func validateComponentNameUnique(name string, existing map[string]spec.Component) error {
	if err := spec.ValidateComponentName(name); err != nil {
		return fmt.Errorf("failed to validate component name: %w", err)
	}
	if _, exists := existing[name]; exists {
		return fmt.Errorf("component '%s' already exists", name)
	}
	return nil
}

// roleOrder lists component roles in display order for the role select.
var roleOrder = []spec.ComponentRole{
	spec.ComponentRoleService,
	spec.ComponentRoleWorker,
	spec.ComponentRoleJob,
}

// roleLabels maps each role to a select label with a short explanation and
// concrete examples, following the same example-driven pattern as
// presetLabel.
var roleLabels = map[spec.ComponentRole]string{
	spec.ComponentRoleService: "service - handles HTTP requests (web apps, APIs)",
	spec.ComponentRoleWorker:  "worker  - long-running background process, no HTTP (queue consumers)",
	spec.ComponentRoleJob:     "job     - runs a task to completion, then exits (migrations, batch tasks)",
}

// roleFromLabel reverses roleLabels. It reports false when label does not
// match any known role.
func roleFromLabel(label string) (spec.ComponentRole, bool) {
	for _, r := range roleOrder {
		if roleLabels[r] == label {
			return r, true
		}
	}
	return "", false
}

// kindOrder lists component kinds in display order for the kind select.
var kindOrder = []spec.ComponentKind{
	spec.ComponentKindStateless,
	spec.ComponentKindStateful,
}

// kindLabels maps each kind to a select label with a short explanation.
var kindLabels = map[spec.ComponentKind]string{
	spec.ComponentKindStateless: "stateless - remembers nothing on disk; replicas scale freely",
	spec.ComponentKindStateful:  "stateful  - keeps data on a persistent volume; stable replica identity",
}

// kindFromLabel reverses kindLabels. It reports false when label does not
// match any known kind.
func kindFromLabel(label string) (spec.ComponentKind, bool) {
	for _, k := range kindOrder {
		if kindLabels[k] == label {
			return k, true
		}
	}
	return "", false
}

// presetOrder lists resource presets in display order, smallest to largest.
var presetOrder = []spec.ResourcePreset{
	spec.ResourcePresetNano,
	spec.ResourcePresetMicro,
	spec.ResourcePresetSmall,
	spec.ResourcePresetMedium,
	spec.ResourcePresetLarge,
	spec.ResourcePresetXLarge,
	spec.ResourcePreset2XLarge,
}

// customResourcesLabel is the merged resources select entry that opens the
// manual CPU/memory/storage form instead of picking a preset.
const customResourcesLabel = "Custom... (enter CPU/memory/storage manually)"

// presetLabel formats a resource preset with its request values for
// display in the merged resources select, e.g. "small - 500m CPU / 512Mi
// memory".
func presetLabel(p spec.ResourcePreset) string {
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

// presetFromLabel reverses presetLabel. It reports false when label does not
// match any known preset (i.e. the caller picked customResourcesLabel).
func presetFromLabel(label string) (spec.ResourcePreset, bool) {
	for _, p := range presetOrder {
		if presetLabel(p) == label {
			return p, true
		}
	}
	return "", false
}

// resolveComponents implements the flag > prompt > default resolution
// order for components.
func resolveComponents(c *nabat.Context, config *ProjectConfig, useDefaults bool) error {
	if useDefaults {
		// --set covers per-field overrides on top of this single default
		// component.
		config.Components[DefaultComponentName] = spec.Component{
			Role:           spec.ComponentRoleService,
			Image:          DefaultComponentImage,
			ResourcePreset: spec.ResourcePresetSmall,
		}
		return nil
	}

	return collectComponents(c, config)
}

func collectComponents(c *nabat.Context, config *ProjectConfig) error {
	continueAdding := true
	selectedEnvironments := config.EnvironmentNames

	for continueAdding {
		var componentName string
		var addAnother bool

		err := c.Form(
			nabat.WithFormTitle(StepComponents),
			nabat.WithFormField(&componentName, "Component Name",
				"Enter the name of the component (e.g., web, api, worker, db)",
				nabat.WithHint("web"),
				nabat.WithValidate(func(s string) error {
					return validateComponentNameUnique(s, config.Components)
				}),
			),
			nabat.WithFormField(&addAnother, "Add another component?", "",
				nabat.WithAffirmative("Yes, add another"),
				nabat.WithNegative("No, I'm done"),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to collect component name: %w", err)
		}

		if componentName != "" {
			var component spec.Component
			component, err = collectComponentDetails(c, componentName, selectedEnvironments)
			if err != nil {
				return fmt.Errorf("failed to collect details for component %s: %w", componentName, err)
			}
			config.Components[componentName] = component
		}

		continueAdding = addAnother

		if !addAnother && len(config.Components) == 0 {
			c.Warn("You must add at least one component to your project.")
			continueAdding = true
		}
	}

	return nil
}

// collectComponentDetails collects a component's configuration in two parts:
// essentials, which are always asked, and an advanced block behind a single
// gate.
func collectComponentDetails(c *nabat.Context, componentName string, availableEnvironments []string) (spec.Component, error) {
	component := spec.Component{}

	if err := collectComponentEssentials(c, &component, componentName); err != nil {
		return component, err
	}

	if err := collectComponentAdvanced(c, &component, componentName, availableEnvironments); err != nil {
		return component, err
	}

	return component, nil
}

// needsHealthCheckQuestion reports whether the health-check question
// applies: a health probe needs a named port to attach to.
func needsHealthCheckQuestion(component spec.Component) bool {
	return component.ListensOnPort()
}

// collectComponentEssentials asks the questions every component needs
// regardless of role: role, image, resources, and (for service components
// only) port and expose.
func collectComponentEssentials(c *nabat.Context, component *spec.Component, componentName string) error {
	if err := collectComponentRole(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component role: %w", err)
	}

	if err := collectComponentImage(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component image: %w", err)
	}

	if component.Role.IsService() {
		if err := collectComponentPort(c, component, componentName); err != nil {
			return fmt.Errorf("failed to collect component port: %w", err)
		}
	}

	if err := collectComponentResources(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component resources: %w", err)
	}

	if component.Role.IsService() {
		if err := collectComponentExpose(c, component, componentName); err != nil {
			return fmt.Errorf("failed to collect component expose: %w", err)
		}
	}

	return nil
}

// collectComponentAdvanced asks the remaining, optional questions behind a
// single "configure advanced options" gate.
func collectComponentAdvanced(c *nabat.Context, component *spec.Component, componentName string, availableEnvironments []string) error {
	advanced, err := c.Confirm(
		fmt.Sprintf("Configure advanced options for %s?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, use defaults"),
	)
	if err != nil {
		return fmt.Errorf("failed to get advanced options preference: %w", err)
	}

	if !advanced {
		// Zero values for every advanced field: FillSpecWithDefaults later
		// applies the schema defaults (e.g. kind: stateless), and a nil
		// Environments means "active everywhere" per the schema, rather
		// than the older behavior of always writing an explicit list.
		return nil
	}

	if err = collectComponentKind(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component kind: %w", err)
	}

	if component.Expose != nil {
		if err = collectComponentExposeOptions(c, component, componentName); err != nil {
			return fmt.Errorf("failed to collect component expose options: %w", err)
		}
	}

	if err = collectComponentCommand(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component command: %w", err)
	}

	if err = collectComponentArgs(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component args: %w", err)
	}

	if err = collectComponentConfigFiles(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component config files: %w", err)
	}

	if err = collectComponentAutoscaling(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component autoscaling: %w", err)
	}

	if err = collectComponentEnvironmentVariables(c, component, componentName); err != nil {
		return fmt.Errorf("failed to collect component environment variables: %w", err)
	}

	if needsHealthCheckQuestion(*component) {
		if err = collectComponentHealth(c, component, componentName); err != nil {
			return fmt.Errorf("failed to collect component health check: %w", err)
		}
	}

	if err = collectComponentEnvironments(c, component, componentName, availableEnvironments); err != nil {
		return fmt.Errorf("failed to collect component environments: %w", err)
	}

	return nil
}

func collectComponentRole(c *nabat.Context, component *spec.Component, componentName string) error {
	labels := make([]string, 0, len(roleOrder))
	for _, r := range roleOrder {
		labels = append(labels, roleLabels[r])
	}

	choice, err := nabat.Select(c,
		fmt.Sprintf("Role for %s - how does this component run?", componentName),
		labels,
		roleLabels[spec.ComponentRoleService],
	)
	if err != nil {
		return fmt.Errorf("failed to collect component role: %w", err)
	}

	role, ok := roleFromLabel(choice)
	if !ok {
		return fmt.Errorf("unrecognized role selection %q", choice)
	}
	component.Role = role
	return nil
}

func collectComponentKind(c *nabat.Context, component *spec.Component, componentName string) error {
	labels := make([]string, 0, len(kindOrder))
	for _, k := range kindOrder {
		labels = append(labels, kindLabels[k])
	}

	choice, err := nabat.Select(c,
		fmt.Sprintf("Kind for %s - does it keep data on disk?", componentName),
		labels,
		kindLabels[spec.ComponentKindStateless],
	)
	if err != nil {
		return fmt.Errorf("failed to collect component kind: %w", err)
	}

	kind, ok := kindFromLabel(choice)
	if !ok {
		return fmt.Errorf("unrecognized kind selection %q", choice)
	}
	component.Kind = kind
	return nil
}

func collectComponentImage(c *nabat.Context, component *spec.Component, componentName string) error {
	image, err := c.Input(
		fmt.Sprintf("Image for %s", componentName),
		nabat.WithHint("nginx:latest"),
		nabat.WithValidate(func(s string) error {
			return util.ValidateNonEmpty(s, "image")
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component image: %w", err)
	}
	component.Image = image
	return nil
}

func collectComponentPort(c *nabat.Context, component *spec.Component, componentName string) error {
	addPort, err := c.Confirm(
		fmt.Sprintf("Add port for %s? Does this component need to expose a port?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to collect port preference: %w", err)
	}

	if addPort {
		var portStr string
		portStr, err = c.Input(
			fmt.Sprintf("Port for %s", componentName),
			nabat.WithHint("8080"),
			nabat.WithValidate(spec.ValidatePort),
		)
		if err != nil {
			return fmt.Errorf("failed to collect port number: %w", err)
		}
		port, atoiErr := strconv.Atoi(portStr)
		if atoiErr != nil {
			return fmt.Errorf("invalid port number: %w", atoiErr)
		}
		component.Port = port
	}

	return nil
}

// collectComponentResources asks one merged question for a component's
// resources: pick a preset or fall through to a manual form.
func collectComponentResources(c *nabat.Context, component *spec.Component, componentName string) error {
	labels := make([]string, 0, len(presetOrder)+1)
	for _, p := range presetOrder {
		labels = append(labels, presetLabel(p))
	}
	labels = append(labels, customResourcesLabel)

	choice, err := nabat.Select(c,
		fmt.Sprintf("Resources for %s — Select a resource preset or enter custom values", componentName),
		labels,
		presetLabel(spec.ResourcePresetSmall),
	)
	if err != nil {
		return fmt.Errorf("failed to collect resources: %w", err)
	}

	if choice == customResourcesLabel {
		// Mutually exclusive with ResourcePreset below: setting both fields
		// fails spec.ValidateComponentResources.
		return collectComponentCustomResources(c, component, componentName)
	}

	preset, ok := presetFromLabel(choice)
	if !ok {
		return fmt.Errorf("unrecognized resource selection %q", choice)
	}
	component.ResourcePreset = preset
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
	)
	if err != nil {
		return fmt.Errorf("failed to get command preference: %w", err)
	}

	if addCommand {
		commandStr, inputErr := c.Input(
			fmt.Sprintf("Command for %s — Command to run in the container (space-separated)", componentName),
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
	)
	if err != nil {
		return fmt.Errorf("failed to get args preference: %w", err)
	}

	if addArgs {
		argsStr, inputErr := c.Input(
			fmt.Sprintf("Arguments for %s — Arguments to pass to the command (space-separated)", componentName),
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

		// Initialize the bound variables so the TTY form shows the defaults
		// prefilled. nabat.WithDefault only sets the non-interactive
		// fallback value; it does not prefill the widget.
		minReplicasStr := strconv.Itoa(DefaultMinReplicas)
		maxReplicasStr := strconv.Itoa(DefaultMaxReplicas)
		err = c.Form(
			nabat.WithFormTitle(fmt.Sprintf("Autoscaling Configuration for %s", componentName)),
			nabat.WithFormField(&minReplicasStr, "Minimum Replicas",
				"Minimum number of replicas to maintain",
				nabat.WithHint(strconv.Itoa(DefaultMinReplicas)),
				nabat.WithDefault(strconv.Itoa(DefaultMinReplicas)),
				nabat.WithValidate(func(s string) error { return util.ValidatePositiveInteger(s, "minimum replicas") }),
			),
			nabat.WithFormField(&maxReplicasStr, "Maximum Replicas",
				"Maximum number of replicas allowed",
				nabat.WithHint(strconv.Itoa(DefaultMaxReplicas)),
				nabat.WithDefault(strconv.Itoa(DefaultMaxReplicas)),
				nabat.WithValidate(func(s string) error { return util.ValidatePositiveInteger(s, "maximum replicas") }),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to get autoscaling config: %w", err)
		}

		if err = util.ValidateMinMaxReplicas(minReplicasStr, maxReplicasStr); err != nil {
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
				Target: DefaultCPUThreshold,
			},
		}

		component.Autoscaling = autoscaling
	}

	return nil
}

// collectComponentCustomResources collects explicit CPU/memory/storage
// values. Called only when the merged resources select (see
// collectComponentResources) resolves to customResourcesLabel.
func collectComponentCustomResources(c *nabat.Context, component *spec.Component, componentName string) error {
	var cpu, memory, ephemeralStorage string
	err := c.Form(
		nabat.WithFormTitle(fmt.Sprintf("Custom Resources for %s", componentName)),
		nabat.WithFormField(&cpu, "CPU",
			"CPU resource (e.g., 500m, 1)",
			nabat.WithHint("500m"),
			nabat.WithValidate(func(s string) error { return util.ValidateResourceString(s, "CPU") }),
		),
		nabat.WithFormField(&memory, "Memory",
			"Memory resource (e.g., 512Mi, 1Gi)",
			nabat.WithHint("512Mi"),
			nabat.WithValidate(func(s string) error { return util.ValidateResourceString(s, "Memory") }),
		),
		nabat.WithFormField(&ephemeralStorage, "Ephemeral Storage",
			"Ephemeral storage (e.g., 1Gi, 2Gi)",
			nabat.WithHint("1Gi"),
			nabat.WithValidate(func(s string) error { return util.ValidateResourceString(s, "EphemeralStorage") }),
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

func collectComponentExpose(c *nabat.Context, component *spec.Component, componentName string) error {
	addExpose, err := c.Confirm(
		fmt.Sprintf("Expose %s on the internet? It gets its own hostname (%s.<domain>) and HTTPS from the platform", componentName, componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get expose preference: %w", err)
	}

	if addExpose {
		// Zero value = all defaults; it is saved as `expose: true`. The
		// advanced gate offers domain/subdomain customization.
		component.Expose = &spec.Expose{}
	}
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
		component.Environments = availableEnvironments
		return nil
	}

	selectedEnvs, err := nabat.MultiSelect(c,
		fmt.Sprintf("Environment Selection for %s — Select one or more environments for this component", componentName),
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
