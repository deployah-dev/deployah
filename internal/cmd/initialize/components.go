package initialize

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"deployah.dev/deployah/internal/manifest"
	"deployah.dev/deployah/internal/util"
	"nabat.dev/nabat"
)

func validateComponentNameUnique(name string, existing map[string]manifest.Component) error {
	if err := manifest.ValidateComponentName(name); err != nil {
		return fmt.Errorf("failed to validate component name: %w", err)
	}
	if _, exists := existing[name]; exists {
		return fmt.Errorf("component '%s' already exists", name)
	}
	return nil
}

func validateResourcePreset(preset manifest.ResourcePreset) error {
	validPresets := []manifest.ResourcePreset{
		manifest.ResourcePresetNano,
		manifest.ResourcePresetMicro,
		manifest.ResourcePresetSmall,
		manifest.ResourcePresetMedium,
		manifest.ResourcePresetLarge,
		manifest.ResourcePresetXLarge,
		manifest.ResourcePreset2XLarge,
	}

	if slices.Contains(validPresets, preset) {
		return nil
	}

	return fmt.Errorf("invalid resource preset '%s': must be one of %v", preset, validPresets)
}

func collectComponents(c *nabat.Context, config *ProjectConfig) error {
	continueAdding := true
	selectedEnvironments := make([]string, len(config.Environments))
	for i, env := range config.Environments {
		selectedEnvironments[i] = env.Name
	}

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
			component, err := collectComponentDetails(c, componentName, selectedEnvironments)
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

func collectComponentDetails(c *nabat.Context, componentName string, availableEnvironments []string) (manifest.Component, error) {
	component := manifest.Component{}

	if err := collectComponentRole(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component role: %w", err)
	}

	if err := collectComponentKind(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component kind: %w", err)
	}

	if err := collectComponentImage(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component image: %w", err)
	}

	if err := collectComponentPort(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component port: %w", err)
	}

	if err := collectComponentResourcePreset(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component resource preset: %w", err)
	}

	if err := collectComponentConfigFiles(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component config files: %w", err)
	}

	if err := collectComponentCommand(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component command: %w", err)
	}

	if err := collectComponentArgs(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component args: %w", err)
	}

	if err := collectComponentAutoscaling(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component autoscaling: %w", err)
	}

	if err := collectComponentCustomResources(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component custom resources: %w", err)
	}

	if err := collectComponentIngress(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component ingress: %w", err)
	}

	if err := collectComponentEnvironmentVariables(c, &component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component environment variables: %w", err)
	}

	if err := collectComponentEnvironments(c, &component, componentName, availableEnvironments); err != nil {
		return component, fmt.Errorf("failed to collect component environments: %w", err)
	}

	return component, nil
}

func collectComponentRole(c *nabat.Context, component *manifest.Component, componentName string) error {
	roles := []string{
		string(manifest.ComponentRoleService),
		string(manifest.ComponentRoleWorker),
		string(manifest.ComponentRoleJob),
	}

	roleChoice, err := nabat.Select(c,
		fmt.Sprintf("Role for %s — What role does this component play in your application?", componentName),
		roles,
		string(manifest.ComponentRoleService),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component role: %w", err)
	}

	component.Role = manifest.ComponentRole(roleChoice)
	return nil
}

func collectComponentKind(c *nabat.Context, component *manifest.Component, componentName string) error {
	kinds := []string{
		string(manifest.ComponentKindStateless),
		string(manifest.ComponentKindStateful),
	}

	kindChoice, err := nabat.Select(c,
		fmt.Sprintf("Kind for %s — What kind of component is this?", componentName),
		kinds,
		string(manifest.ComponentKindStateless),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component kind: %w", err)
	}

	component.Kind = manifest.ComponentKind(kindChoice)
	return nil
}

func collectComponentImage(c *nabat.Context, component *manifest.Component, componentName string) error {
	var image string
	err := c.Form(
		nabat.WithFormField(&image, fmt.Sprintf("Image for %s", componentName),
			"Docker image to use for this component",
			nabat.WithHint("nginx:latest"),
			nabat.WithValidate(func(s string) error {
				return util.ValidateNonEmpty(s, "image")
			}),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to collect component image: %w", err)
	}

	component.Image = image
	return nil
}

func collectComponentPort(c *nabat.Context, component *manifest.Component, componentName string) error {
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
		err := c.Form(
			nabat.WithFormField(&portStr, fmt.Sprintf("Port for %s", componentName),
				"Port number to expose, must be between 1024 and 65535",
				nabat.WithHint("8080"),
				nabat.WithValidate(manifest.ValidatePort),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to collect port number: %w", err)
		}

		port, _ := strconv.Atoi(portStr)
		component.Port = port
	}

	return nil
}

func collectComponentResourcePreset(c *nabat.Context, component *manifest.Component, componentName string) error {
	presets := []string{
		string(manifest.ResourcePresetNano),
		string(manifest.ResourcePresetMicro),
		string(manifest.ResourcePresetSmall),
		string(manifest.ResourcePresetMedium),
		string(manifest.ResourcePresetLarge),
		string(manifest.ResourcePresetXLarge),
		string(manifest.ResourcePreset2XLarge),
	}

	resourcePreset, err := nabat.Select(c,
		fmt.Sprintf("Resource Preset for %s — Select a resource preset for this component", componentName),
		presets,
		string(manifest.ResourcePresetSmall),
	)
	if err != nil {
		return fmt.Errorf("failed to collect resource preset: %w", err)
	}

	preset := manifest.ResourcePreset(resourcePreset)
	if err := validateResourcePreset(preset); err != nil {
		return fmt.Errorf("invalid resource preset selected: %w", err)
	}

	component.ResourcePreset = preset
	return nil
}

func collectComponentConfigFiles(c *nabat.Context, component *manifest.Component, componentName string) error {
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
		err := c.Form(
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

func collectComponentCommand(c *nabat.Context, component *manifest.Component, componentName string) error {
	addCommand, err := c.Confirm(
		fmt.Sprintf("Add custom command for %s? Would you like to override the container's default command?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, use image default"),
	)
	if err != nil {
		return fmt.Errorf("failed to get command preference: %w", err)
	}

	if addCommand {
		commandStr, err := c.Input(
			fmt.Sprintf("Command for %s — Command to run in the container (space-separated)", componentName),
			nabat.WithHint("python app.py"),
		)
		if err != nil {
			return fmt.Errorf("failed to get command: %w", err)
		}

		if commandStr != "" {
			component.Command = strings.Fields(commandStr)
		}
	}

	return nil
}

func collectComponentArgs(c *nabat.Context, component *manifest.Component, componentName string) error {
	addArgs, err := c.Confirm(
		fmt.Sprintf("Add arguments for %s? Would you like to add arguments to the command?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get args preference: %w", err)
	}

	if addArgs {
		argsStr, err := c.Input(
			fmt.Sprintf("Arguments for %s — Arguments to pass to the command (space-separated)", componentName),
			nabat.WithHint("--port 8080 --debug"),
		)
		if err != nil {
			return fmt.Errorf("failed to get arguments: %w", err)
		}

		if argsStr != "" {
			component.Args = strings.Fields(argsStr)
		}
	}

	return nil
}

func collectComponentAutoscaling(c *nabat.Context, component *manifest.Component, componentName string) error {
	addAutoscaling, err := c.Confirm(
		fmt.Sprintf("Enable autoscaling for %s? Would you like to enable automatic scaling based on resource usage?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get autoscaling preference: %w", err)
	}

	if addAutoscaling {
		autoscaling := &manifest.Autoscaling{
			Enabled: true,
		}

		var minReplicasStr, maxReplicasStr string
		err := c.Form(
			nabat.WithFormTitle(fmt.Sprintf("Autoscaling Configuration for %s", componentName)),
			nabat.WithFormField(&minReplicasStr, "Minimum Replicas",
				"Minimum number of replicas to maintain",
				nabat.WithHint(strconv.Itoa(DefaultMinReplicas)),
				nabat.WithValidate(func(s string) error { return util.ValidatePositiveInteger(s, "minimum replicas") }),
			),
			nabat.WithFormField(&maxReplicasStr, "Maximum Replicas",
				"Maximum number of replicas allowed",
				nabat.WithHint(strconv.Itoa(DefaultMaxReplicas)),
				nabat.WithValidate(func(s string) error { return util.ValidatePositiveInteger(s, "maximum replicas") }),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to get autoscaling config: %w", err)
		}

		if err := util.ValidateMinMaxReplicas(minReplicasStr, maxReplicasStr); err != nil {
			return fmt.Errorf("autoscaling configuration error: %w", err)
		}

		minReplicas, _ := strconv.Atoi(minReplicasStr)
		maxReplicas, _ := strconv.Atoi(maxReplicasStr)
		autoscaling.MinReplicas = minReplicas
		autoscaling.MaxReplicas = maxReplicas

		autoscaling.Metrics = []manifest.Metric{
			{
				Type:   manifest.MetricTypeCPU,
				Target: DefaultCPUThreshold,
			},
		}

		component.Autoscaling = autoscaling
	}

	return nil
}

func collectComponentCustomResources(c *nabat.Context, component *manifest.Component, componentName string) error {
	addCustomResources, err := c.Confirm(
		fmt.Sprintf("Add custom resources for %s? Would you like to specify custom CPU, memory, or storage requirements?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No, use resource preset"),
	)
	if err != nil {
		return fmt.Errorf("failed to get custom resources preference: %w", err)
	}

	if addCustomResources {
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

		resources := manifest.Resources{}
		if cpu != "" {
			resources.CPU = &cpu
		}
		if memory != "" {
			resources.Memory = &memory
		}
		if ephemeralStorage != "" {
			resources.EphemeralStorage = &ephemeralStorage
		}

		component.Resources = resources
	}

	return nil
}

func collectComponentIngress(c *nabat.Context, component *manifest.Component, componentName string) error {
	addIngress, err := c.Confirm(
		fmt.Sprintf("Add ingress for %s? Would you like to expose this component via HTTP/HTTPS?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get ingress preference: %w", err)
	}

	if addIngress {
		var host string
		var tls bool
		err := c.Form(
			nabat.WithFormField(&host, fmt.Sprintf("Host for %s", componentName),
				"Hostname for external access",
				nabat.WithHint("api.example.com"),
				nabat.WithValidate(manifest.ValidateHostname),
			),
			nabat.WithFormField(&tls, fmt.Sprintf("Enable TLS for %s?", componentName),
				"Enable HTTPS with TLS certificate",
				nabat.WithAffirmative("Yes"),
				nabat.WithNegative("No"),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to get ingress config: %w", err)
		}

		if host != "" {
			component.Ingress = &manifest.Ingress{
				Host: host,
				TLS:  tls,
			}
		}
	}

	return nil
}

func collectComponentEnvironmentVariables(c *nabat.Context, component *manifest.Component, componentName string) error {
	addComponentEnvVars, err := c.Confirm(
		fmt.Sprintf("Add environment variables for %s? Would you like to add component-specific environment variables?", componentName),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
	)
	if err != nil {
		return fmt.Errorf("failed to get component env preference: %w", err)
	}

	if addComponentEnvVars {
		envVars, err := collectEnvironmentVariables(c)
		if err != nil {
			return fmt.Errorf("failed to collect component environment variables: %w", err)
		}
		component.Env = envVars
	}

	return nil
}

func collectComponentEnvironments(c *nabat.Context, component *manifest.Component, componentName string, availableEnvironments []string) error {
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
