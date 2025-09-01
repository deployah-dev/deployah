package initilaize

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/deployah-dev/deployah/internal/logging"
	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/deployah-dev/deployah/internal/ui"
	"github.com/deployah-dev/deployah/internal/util"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// Constants for default values and validation
const (
	DefaultOutputFile   = "deployah.yaml"
	DefaultCPUThreshold = 75
	DefaultMinReplicas  = 2
	DefaultMaxReplicas  = 5
)

// Dry-run mode constants
const (
	DryRunPreviewHeader = "=== DRY RUN MODE - PREVIEW OF GENERATED MANIFEST ===\n"
	DryRunPreviewFooter = "\n=== END PREVIEW ===\n\nThis is a preview. Use --dry-run=false to actually save the configuration."
)

// Step progress indicators
const (
	StepProjectName  = "Step 1/4: Project Name"
	StepEnvironments = "Step 2/4: Environments"
	StepComponents   = "Step 3/4: Components"
	StepSummary      = "Step 4/4: Summary"
)

// Progress tracking moved to internal/ui

// ProjectConfig holds the collected configuration data
type ProjectConfig struct {
	Name         string
	Environments []manifest.Environment
	Components   map[string]manifest.Component
	OutputPath   string
	DryRun       bool
}

// validateEnvironmentNameUnique validates that the environment name is unique and valid
func validateEnvironmentNameUnique(name string, existing []string) error {
	if err := manifest.ValidateEnvName(name); err != nil {
		return fmt.Errorf("failed to validate environment name: %w", err)
	}
	if slices.Contains(existing, name) {
		return fmt.Errorf("environment '%s' already exists", name)
	}
	return nil
}

// validateComponentNameUnique validates that the component name is unique and valid
func validateComponentNameUnique(name string, existing map[string]manifest.Component) error {
	if err := manifest.ValidateComponentName(name); err != nil {
		return fmt.Errorf("failed to validate component name: %w", err)
	}
	if _, exists := existing[name]; exists {
		return fmt.Errorf("component '%s' already exists", name)
	}
	return nil
}

// validateResourcePreset validates that the resource preset is valid
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

// collectWithForm is a generic form collection helper to reduce code duplication
func collectWithForm(form *huh.Form, errorMsg string) error {
	if err := form.Run(); err != nil {
		return fmt.Errorf("%s: %w", errorMsg, err)
	}
	return nil
}

// createConfirmGroup creates a confirm group for a form
func createConfirmGroup(title, description, affirmative, negative string, value *bool) *huh.Group {
	return huh.NewGroup(
		huh.NewConfirm().
			Title(title).
			Description(description).
			Affirmative(affirmative).
			Negative(negative).
			Value(value),
	)
}

// createNoteGroup creates a note group for a form
func createNoteGroup(title, description string) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title(title).
			Description(description),
	)
}

// createConfirmForm creates a confirm form
func createConfirmForm(title, description, affirmative, negative string, value *bool) *huh.Form {
	return huh.NewForm(createConfirmGroup(title, description, affirmative, negative, value))
}

// createNoteForm creates a note form
func createNoteForm(title, description string) *huh.Form {
	return huh.NewForm(createNoteGroup(title, description))
}

// New creates and returns a new cobra command for initializing Deployah configuration.
func New() *cobra.Command {
	initCommand := &cobra.Command{
		Use:     "init",
		Aliases: []string{"initialize"},
		Short:   "Initialize Deployah configuration for a new project",
		Long:    `Initialize Deployah configuration for a new project`,
		RunE:    runInit,
		Example: `
# Initialize a new project
deployah init

# Initialize a new project and save the configuration to a file
deployah init --output deployah.yaml

# Preview the generated manifest without saving it
deployah init --dry-run
		`,
	}

	initCommand.Flags().StringP("output", "o", DefaultOutputFile, "The output file path.")
	initCommand.Flags().BoolP("dry-run", "d", false, "Preview the generated manifest without saving it")

	return initCommand
}

// runInit is the main function for the init command
func runInit(cmd *cobra.Command, _ []string) error {
	logger := logging.GetLogger(cmd)
	logger.Info("Starting project initialization", "cmd", "init")

	// Get flags
	outputPath, _ := cmd.Flags().GetString("output")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	config := &ProjectConfig{
		OutputPath: outputPath,
		Components: make(map[string]manifest.Component),
		DryRun:     dryRun,
	}

	progress := ui.NewProgressTracker()

	// Collect project configuration step by step with better error handling
	if err := collectProjectName(config, progress); err != nil {
		return fmt.Errorf("failed to collect project name: %w", err)
	}

	if err := collectEnvironments(config, progress); err != nil {
		return fmt.Errorf("failed to collect environments: %w", err)
	}

	if err := collectComponents(config, progress); err != nil {
		return fmt.Errorf("failed to collect components: %w", err)
	}

	if err := showSummaryAndSave(config, progress); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	if config.DryRun {
		logger.Info("Project initialization completed (dry-run mode)",
			"project", config.Name,
			"environments", len(config.Environments),
			"components", len(config.Components),
			"output", config.OutputPath)
	} else {
		logger.Info("Project initialization completed successfully",
			"project", config.Name,
			"environments", len(config.Environments),
			"components", len(config.Components),
			"output", config.OutputPath)
	}

	return nil
}

// collectProjectName collects the project name from user input
func collectProjectName(config *ProjectConfig, progress *ui.ProgressTracker) error {
	form := ui.CreateInputForm(
		progress.GetCurrentStep(),
		"my-awesome-project",
		"What is the name of your project? Use lowercase letters, numbers, and dashes only.",
		manifest.ValidateProjectName,
		&config.Name,
	)

	err := ui.CollectWithForm(form, "failed to get project details from user")
	if err == nil {
		progress.NextStep()
	}
	return err
}

// collectEnvironments collects environment configuration from user input
func collectEnvironments(config *ProjectConfig, progress *ui.ProgressTracker) error {
	var selectedEnvironments []string
	var continueAddingEnvironments bool = true

	// Collect environment names
	for continueAddingEnvironments {
		var envName string
		var addAnother bool

		// Environment name form
		envNameGroup := ui.CreateInputGroup(
			StepEnvironments,
			"dev",
			"Enter the name of an environment (e.g., development, staging, production, review/*)",
			func(s string) error {
				return validateEnvironmentNameUnique(s, selectedEnvironments)
			},
			&envName,
		)

		// Add another environment confirmation
		continueGroup := ui.CreateConfirmGroup(
			"Add another environment?",
			"",
			"Yes, add another",
			"No, I'm done",
			&addAnother,
		)

		// Combine forms
		envForm := huh.NewForm(
			envNameGroup,
			continueGroup,
		)

		if err := ui.CollectWithForm(envForm, "failed to get environment name"); err != nil {
			return fmt.Errorf("failed to collect environment name: %w", err)
		}

		if envName != "" {
			selectedEnvironments = append(selectedEnvironments, envName)
		}

		continueAddingEnvironments = addAnother
	}

	// Collect details for each environment
	for _, envName := range selectedEnvironments {
		env, err := collectEnvironmentDetails(envName)
		if err != nil {
			return fmt.Errorf("failed to collect details for environment %s: %w", envName, err)
		}
		config.Environments = append(config.Environments, env)
	}

	progress.NextStep()
	return nil
}

// collectEnvironmentDetails collects detailed configuration for a single environment
func collectEnvironmentDetails(envName string) (manifest.Environment, error) {
	env := manifest.Environment{Name: envName}

	// Environment files and variables form
	var addVariables bool
	combinedForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Environment File for %s", envName)).
				Placeholder(fmt.Sprintf(".env.%s", strings.TrimSuffix(envName, "/*"))).
				Description("Path to the environment file (optional, defaults to .env.{name})").
				Value(&env.EnvFile),

			huh.NewInput().
				Title(fmt.Sprintf("Config File for %s", envName)).
				Placeholder(fmt.Sprintf("config.%s.yaml", strings.TrimSuffix(envName, "/*"))).
				Description("Path to the configuration file (optional, defaults to config.{name}.yaml)").
				Value(&env.ConfigFile),
		),

		createConfirmGroup(
			fmt.Sprintf("Add custom variables for %s?", envName),
			"You can add environment-specific variables that will be used as template variables.",
			"Yes, add variables",
			"No, skip variables",
			&addVariables,
		),
	)

	if err := collectWithForm(combinedForm, fmt.Sprintf("failed to get environment details for %s", envName)); err != nil {
		return env, fmt.Errorf("failed to collect environment details: %w", err)
	}

	// Collect variables if requested
	if addVariables {
		variables, err := collectEnvironmentVariables()
		if err != nil {
			return env, fmt.Errorf("failed to collect variables for environment %s: %w", envName, err)
		}
		env.Variables = variables
	}

	return env, nil
}

// collectEnvironmentVariables collects environment variables from user input
func collectEnvironmentVariables() (map[string]string, error) {
	variables := make(map[string]string)
	var continueAdding bool = true

	for continueAdding {
		var varName, varValue string

		// Variable form
		varNameInput := huh.NewInput().
			Title("Variable Name").
			Placeholder("APP_ENV").
			Description("Environment variable name (uppercase with underscores)").
			Validate(manifest.ValidateEnvVarName).
			Value(&varName)

		varValueInput := huh.NewInput().
			Title("Variable Value").
			Placeholder("production").
			Description("Value for the environment variable").
			Value(&varValue)

		varGroup := huh.NewGroup(varNameInput, varValueInput)

		continueGroup := createConfirmGroup(
			"Add another variable?",
			"",
			"Yes",
			"No, I'm done",
			&continueAdding,
		)

		// Combine forms
		varForm := huh.NewForm(
			varGroup,
			continueGroup,
		)

		if err := collectWithForm(varForm, "failed to get variable details"); err != nil {
			return nil, fmt.Errorf("failed to collect variable details: %w", err)
		}

		if varName != "" && varValue != "" {
			variables[varName] = varValue
		}
	}

	return variables, nil
}

// collectComponents collects component configuration from user input
func collectComponents(config *ProjectConfig, progress *ui.ProgressTracker) error {
	var continueAddingComponents bool = true
	selectedEnvironments := make([]string, len(config.Environments))
	for i, env := range config.Environments {
		selectedEnvironments[i] = env.Name
	}

	for continueAddingComponents {
		var componentName string
		var addAnotherComponent bool

		// Component name form
		compNameGroup := ui.CreateInputGroup(
			"Component Name",
			"web",
			"Enter the name of the component (e.g., web, api, worker, db)",
			func(s string) error {
				return validateComponentNameUnique(s, config.Components)
			},
			&componentName,
		)

		continueGroup := ui.CreateConfirmGroup(
			"Add another component?",
			"",
			"Yes, add another",
			"No, I'm done",
			&addAnotherComponent,
		)

		// Combine forms
		compForm := huh.NewForm(
			compNameGroup,
			continueGroup,
		)

		if err := ui.CollectWithForm(compForm, "failed to get component name"); err != nil {
			return fmt.Errorf("failed to collect component name: %w", err)
		}

		if componentName != "" {
			component, err := collectComponentDetails(componentName, selectedEnvironments)
			if err != nil {
				return fmt.Errorf("failed to collect details for component %s: %w", componentName, err)
			}
			config.Components[componentName] = component
		}

		continueAddingComponents = addAnotherComponent

		// If user chose not to add another component, check if at least one exists
		if !addAnotherComponent && len(config.Components) == 0 {
			fmt.Println("\n⚠️  You must add at least one component to your project.")
			continueAddingComponents = true
		}
	}

	progress.NextStep()
	return nil
}

// collectComponentDetails collects detailed configuration for a single component
func collectComponentDetails(componentName string, availableEnvironments []string) (manifest.Component, error) {
	component := manifest.Component{}

	// Component role selection
	if err := collectComponentRole(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component role: %w", err)
	}

	// Component kind selection
	if err := collectComponentKind(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component kind: %w", err)
	}

	// Image configuration
	if err := collectComponentImage(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component image: %w", err)
	}

	// Port configuration
	if err := collectComponentPort(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component port: %w", err)
	}

	// Resource preset selection
	if err := collectComponentResourcePreset(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component resource preset: %w", err)
	}

	// Component-specific configuration files
	if err := collectComponentConfigFiles(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component config files: %w", err)
	}

	// Command and arguments
	if err := collectComponentCommand(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component command: %w", err)
	}

	if err := collectComponentArgs(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component args: %w", err)
	}

	// Autoscaling configuration
	if err := collectComponentAutoscaling(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component autoscaling: %w", err)
	}

	// Custom resources
	if err := collectComponentCustomResources(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component custom resources: %w", err)
	}

	// Ingress configuration
	if err := collectComponentIngress(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component ingress: %w", err)
	}

	// Component environment variables
	if err := collectComponentEnvironmentVariables(&component, componentName); err != nil {
		return component, fmt.Errorf("failed to collect component environment variables: %w", err)
	}

	// Environment selection
	if err := collectComponentEnvironments(&component, componentName, availableEnvironments); err != nil {
		return component, fmt.Errorf("failed to collect component environments: %w", err)
	}

	return component, nil
}

// collectComponentRole collects the component role
func collectComponentRole(component *manifest.Component, componentName string) error {
	var roleChoice string
	options := []huh.Option[string]{
		huh.NewOption("Service - externally accessible (web APIs, frontend apps)", string(manifest.ComponentRoleService)),
		huh.NewOption("Worker - long-running or background tasks (queue workers, processors)", string(manifest.ComponentRoleWorker)),
		huh.NewOption("Job - one-off tasks (migrations, batch processing)", string(manifest.ComponentRoleJob)),
	}

	roleForm := ui.CreateSelectForm(
		fmt.Sprintf("Role for %s", componentName),
		"What role does this component play in your application?",
		options,
		&roleChoice,
	)

	if err := ui.CollectWithForm(roleForm, "failed to get component role"); err != nil {
		return fmt.Errorf("failed to collect component role: %w", err)
	}

	component.Role = manifest.ComponentRole(roleChoice)
	return nil
}

// collectComponentKind collects the component kind
func collectComponentKind(component *manifest.Component, componentName string) error {
	var kindChoice string
	options := []huh.Option[string]{
		huh.NewOption("Stateless", string(manifest.ComponentKindStateless)),
		huh.NewOption("Stateful", string(manifest.ComponentKindStateful)),
	}

	kindForm := ui.CreateSelectForm(
		fmt.Sprintf("Kind for %s", componentName),
		"What kind of component is this?",
		options,
		&kindChoice,
	)

	if err := ui.CollectWithForm(kindForm, "failed to get component kind"); err != nil {
		return fmt.Errorf("failed to collect component kind: %w", err)
	}

	component.Kind = manifest.ComponentKind(kindChoice)
	return nil
}

// collectComponentImage collects the component image
func collectComponentImage(component *manifest.Component, componentName string) error {
	var image string
	imageForm := ui.CreateInputForm(
		fmt.Sprintf("Image for %s", componentName),
		"nginx:latest",
		"Docker image to use for this component",
		func(s string) error {
			return util.ValidateNonEmpty(s, "image")
		},
		&image,
	)

	if err := ui.CollectWithForm(imageForm, "failed to get component image"); err != nil {
		return fmt.Errorf("failed to collect component image: %w", err)
	}

	component.Image = image
	return nil
}

// collectComponentPort collects the component port configuration
func collectComponentPort(component *manifest.Component, componentName string) error {
	var addPort bool
	portForm := ui.CreateConfirmForm(
		fmt.Sprintf("Add port for %s?", componentName),
		"Does this component need to expose a port?",
		"Yes",
		"No",
		&addPort,
	)

	if err := ui.CollectWithForm(portForm, "failed to get port preference"); err != nil {
		return fmt.Errorf("failed to collect port preference: %w", err)
	}

	if addPort {
		var portStr string
		portInputForm := ui.CreateInputForm(
			fmt.Sprintf("Port for %s", componentName),
			"8080",
			"Port number to expose, must be between 1024 and 65535",
			manifest.ValidatePort,
			&portStr,
		)

		if err := ui.CollectWithForm(portInputForm, "failed to get port number"); err != nil {
			return fmt.Errorf("failed to collect port number: %w", err)
		}

		port, _ := strconv.Atoi(portStr)
		component.Port = port
	}

	return nil
}

// collectComponentResourcePreset collects the component resource preset
func collectComponentResourcePreset(component *manifest.Component, componentName string) error {
	var resourcePreset string

	// Step 1: Clean preset selection with detailed descriptions
	options := []huh.Option[string]{
		huh.NewOption("Nano    - Minimal resources (50m CPU, 64Mi RAM)", string(manifest.ResourcePresetNano)),
		huh.NewOption("Micro   - Very light workloads (100m CPU, 128Mi RAM)", string(manifest.ResourcePresetMicro)),
		huh.NewOption("Small   - Light workloads (250m CPU, 256Mi RAM)", string(manifest.ResourcePresetSmall)),
		huh.NewOption("Medium  - Moderate workloads (500m CPU, 512Mi RAM)", string(manifest.ResourcePresetMedium)),
		huh.NewOption("Large   - Heavy workloads (1000m CPU, 1Gi RAM)", string(manifest.ResourcePresetLarge)),
		huh.NewOption("XLarge  - Very heavy workloads (2000m CPU, 2Gi RAM)", string(manifest.ResourcePresetXLarge)),
		huh.NewOption("2XLarge - Extremely heavy workloads (4000m CPU, 4Gi RAM)", string(manifest.ResourcePreset2XLarge)),
	}

	resourceForm := ui.CreateSelectForm(
		fmt.Sprintf("Resource Preset for %s", componentName),
		"Select a resource preset for this component",
		options,
		&resourcePreset,
	)

	if err := ui.CollectWithForm(resourceForm, "failed to get resource preset"); err != nil {
		return fmt.Errorf("failed to collect resource preset: %w", err)
	}

	preset := manifest.ResourcePreset(resourcePreset)
	if err := validateResourcePreset(preset); err != nil {
		return fmt.Errorf("invalid resource preset selected: %w", err)
	}

	component.ResourcePreset = preset
	return nil
}

// collectComponentConfigFiles collects component-specific configuration files
func collectComponentConfigFiles(component *manifest.Component, componentName string) error {
	var addConfigFiles bool
	configFilesForm := ui.CreateConfirmForm(
		fmt.Sprintf("Add component-specific config files for %s?", componentName),
		"Would you like to specify custom environment and config files for this component?",
		"Yes",
		"No, use defaults",
		&addConfigFiles,
	)

	if err := configFilesForm.Run(); err != nil {
		return fmt.Errorf("failed to get config files preference: %w", err)
	}

	if addConfigFiles {
		var envFile, configFile string
		configInputForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(fmt.Sprintf("Environment File for %s", componentName)).
					Placeholder(fmt.Sprintf(".env.%s", componentName)).
					Description("Component-specific environment file (optional)").
					Value(&envFile),

				huh.NewInput().
					Title(fmt.Sprintf("Config File for %s", componentName)).
					Placeholder(fmt.Sprintf("config.%s.yaml", componentName)).
					Description("Component-specific configuration file (optional)").
					Value(&configFile),
			),
		)

		if err := configInputForm.Run(); err != nil {
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

// collectComponentCommand collects the component command
func collectComponentCommand(component *manifest.Component, componentName string) error {
	var addCommand bool
	commandForm := ui.CreateConfirmForm(
		fmt.Sprintf("Add custom command for %s?", componentName),
		"Would you like to override the container's default command?",
		"Yes",
		"No, use image default",
		&addCommand,
	)

	if err := commandForm.Run(); err != nil {
		return fmt.Errorf("failed to get command preference: %w", err)
	}

	if addCommand {
		var commandStr string
		commandInputForm := ui.CreateInputForm(
			fmt.Sprintf("Command for %s", componentName),
			"python app.py",
			"Command to run in the container (space-separated)",
			nil,
			&commandStr,
		)

		if err := commandInputForm.Run(); err != nil {
			return fmt.Errorf("failed to get command: %w", err)
		}

		if commandStr != "" {
			component.Command = strings.Fields(commandStr)
		}
	}

	return nil
}

// collectComponentArgs collects the component arguments
func collectComponentArgs(component *manifest.Component, componentName string) error {
	var addArgs bool
	argsForm := ui.CreateConfirmForm(
		fmt.Sprintf("Add arguments for %s?", componentName),
		"Would you like to add arguments to the command?",
		"Yes",
		"No",
		&addArgs,
	)

	if err := argsForm.Run(); err != nil {
		return fmt.Errorf("failed to get args preference: %w", err)
	}

	if addArgs {
		var argsStr string
		argsInputForm := ui.CreateInputForm(
			fmt.Sprintf("Arguments for %s", componentName),
			"--port 8080 --debug",
			"Arguments to pass to the command (space-separated)",
			nil,
			&argsStr,
		)

		if err := argsInputForm.Run(); err != nil {
			return fmt.Errorf("failed to get arguments: %w", err)
		}

		if argsStr != "" {
			component.Args = strings.Fields(argsStr)
		}
	}

	return nil
}

// collectComponentAutoscaling collects the component autoscaling configuration
func collectComponentAutoscaling(component *manifest.Component, componentName string) error {
	var addAutoscaling bool
	autoscalingForm := ui.CreateConfirmForm(
		fmt.Sprintf("Enable autoscaling for %s?", componentName),
		"Would you like to enable automatic scaling based on resource usage?",
		"Yes",
		"No",
		&addAutoscaling,
	)

	if err := autoscalingForm.Run(); err != nil {
		return fmt.Errorf("failed to get autoscaling preference: %w", err)
	}

	if addAutoscaling {
		autoscaling := &manifest.Autoscaling{
			Enabled: true,
		}

		var minReplicasStr, maxReplicasStr string
		autoscalingConfigForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(fmt.Sprintf("Minimum Replicas for %s", componentName)).
					Placeholder(strconv.Itoa(DefaultMinReplicas)).
					Description("Minimum number of replicas to maintain").
					Validate(func(s string) error { return util.ValidatePositiveInteger(s, "minimum replicas") }).
					Value(&minReplicasStr),

				huh.NewInput().
					Title(fmt.Sprintf("Maximum Replicas for %s", componentName)).
					Placeholder(strconv.Itoa(DefaultMaxReplicas)).
					Description("Maximum number of replicas allowed").
					Validate(func(s string) error { return util.ValidatePositiveInteger(s, "maximum replicas") }).
					Value(&maxReplicasStr),
			),
		)

		if err := autoscalingConfigForm.Run(); err != nil {
			return fmt.Errorf("failed to get autoscaling config: %w", err)
		}

		// Cross-field validation after both fields are collected
		if err := util.ValidateMinMaxReplicas(minReplicasStr, maxReplicasStr); err != nil {
			return fmt.Errorf("autoscaling configuration error: %w", err)
		}

		minReplicas, _ := strconv.Atoi(minReplicasStr)
		maxReplicas, _ := strconv.Atoi(maxReplicasStr)
		autoscaling.MinReplicas = minReplicas
		autoscaling.MaxReplicas = maxReplicas

		// Default metrics
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

// collectComponentCustomResources collects custom resource specifications
func collectComponentCustomResources(component *manifest.Component, componentName string) error {
	var addCustomResources bool
	customResourcesForm := createConfirmForm(
		fmt.Sprintf("Add custom resources for %s?", componentName),
		"Would you like to specify custom CPU, memory, or storage requirements?",
		"Yes",
		"No, use resource preset",
		&addCustomResources,
	)

	if err := customResourcesForm.Run(); err != nil {
		return fmt.Errorf("failed to get custom resources preference: %w", err)
	}

	if addCustomResources {
		var cpu, memory, ephemeralStorage string
		resourcesForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(fmt.Sprintf("CPU for %s", componentName)).
					Placeholder("500m").
					Description("CPU resource (e.g., 500m, 1)").
					Validate(func(s string) error { return util.ValidateResourceString(s, "CPU") }).
					Value(&cpu),

				huh.NewInput().
					Title(fmt.Sprintf("Memory for %s", componentName)).
					Placeholder("512Mi").
					Description("Memory resource (e.g., 512Mi, 1Gi)").
					Validate(func(s string) error { return util.ValidateResourceString(s, "Memory") }).
					Value(&memory),

				huh.NewInput().
					Title(fmt.Sprintf("Ephemeral Storage for %s", componentName)).
					Placeholder("1Gi").
					Description("Ephemeral storage (e.g., 1Gi, 2Gi)").
					Validate(func(s string) error { return util.ValidateResourceString(s, "EphemeralStorage") }).
					Value(&ephemeralStorage),
			),
		)

		if err := resourcesForm.Run(); err != nil {
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

// collectComponentIngress collects the component ingress configuration
func collectComponentIngress(component *manifest.Component, componentName string) error {
	var addIngress bool
	ingressForm := ui.CreateConfirmForm(
		fmt.Sprintf("Add ingress for %s?", componentName),
		"Would you like to expose this component via HTTP/HTTPS?",
		"Yes",
		"No",
		&addIngress,
	)

	if err := ingressForm.Run(); err != nil {
		return fmt.Errorf("failed to get ingress preference: %w", err)
	}

	if addIngress {
		var host string
		var tls bool
		ingressConfigForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(fmt.Sprintf("Host for %s", componentName)).
					Placeholder("api.example.com").
					Description("Hostname for external access").
					Validate(manifest.ValidateHostname).
					Value(&host),

				huh.NewConfirm().
					Title(fmt.Sprintf("Enable TLS for %s?", componentName)).
					Description("Enable HTTPS with TLS certificate").
					Affirmative("Yes").
					Negative("No").
					Value(&tls),
			),
		)

		if err := ingressConfigForm.Run(); err != nil {
			return fmt.Errorf("failed to get ingress config: %w", err)
		}

		// Only set ingress if host is provided
		if host != "" {
			component.Ingress = &manifest.Ingress{
				Host: host,
				TLS:  tls,
			}
		}
	}

	return nil
}

// collectComponentEnvironmentVariables collects component-specific environment variables
func collectComponentEnvironmentVariables(component *manifest.Component, componentName string) error {
	var addComponentEnvVars bool
	componentEnvForm := ui.CreateConfirmForm(
		fmt.Sprintf("Add environment variables for %s?", componentName),
		"Would you like to add component-specific environment variables?",
		"Yes",
		"No",
		&addComponentEnvVars,
	)

	if err := componentEnvForm.Run(); err != nil {
		return fmt.Errorf("failed to get component env preference: %w", err)
	}

	if addComponentEnvVars {
		envVars, err := collectEnvironmentVariables()
		if err != nil {
			return fmt.Errorf("failed to collect component environment variables: %w", err)
		}
		component.Env = envVars
	}

	return nil
}

// collectComponentEnvironments collects the environments where the component should be deployed
func collectComponentEnvironments(component *manifest.Component, componentName string, availableEnvironments []string) error {
	// Ensure we have at least one environment
	if len(availableEnvironments) == 0 {
		return fmt.Errorf("no environments available for component deployment")
	}

	// If there's only one environment, automatically select it
	if len(availableEnvironments) == 1 {
		component.Environments = availableEnvironments
		return nil
	}

	var selectedComponentEnvs []string

	// Create options from available environments
	options := make([]huh.Option[string], len(availableEnvironments))
	for i, env := range availableEnvironments {
		options[i] = huh.NewOption(env, env)
	}

	// Create multi-select form
	envForm := ui.CreateMultiSelectForm(
		fmt.Sprintf("Environment Selection for %s", componentName),
		"Select one or more environments for this component",
		options,
		&selectedComponentEnvs,
	)

	if err := ui.CollectWithForm(envForm, "failed to get environment selection"); err != nil {
		return fmt.Errorf("failed to collect environment selection: %w", err)
	}

	// Ensure at least one environment is selected
	if len(selectedComponentEnvs) == 0 {
		return fmt.Errorf("at least one environment must be selected for component %s", componentName)
	}

	component.Environments = selectedComponentEnvs
	return nil
}

// showSummaryAndSave displays a simple summary and saves the manifest
func showSummaryAndSave(config *ProjectConfig, progress *ui.ProgressTracker) error {
	// Build simple summary
	var summary strings.Builder

	summary.WriteString("Configuration Summary:\n\n")

	// Project info
	summary.WriteString(fmt.Sprintf("Project: %s\n", config.Name))
	summary.WriteString(fmt.Sprintf("Output: %s\n", config.OutputPath))
	if config.DryRun {
		summary.WriteString("Mode: DRY RUN (preview only)\n")
	} else {
		summary.WriteString("Mode: SAVE CONFIGURATION\n")
	}
	summary.WriteString("\n")

	// Environments
	summary.WriteString("Environments:\n")
	if len(config.Environments) == 0 {
		summary.WriteString("  (none)\n")
	} else {
		for _, env := range config.Environments {
			summary.WriteString(fmt.Sprintf("  - %s\n", env.Name))
		}
	}
	summary.WriteString("\n")

	// Components
	summary.WriteString("Components:\n")
	if len(config.Components) == 0 {
		summary.WriteString("  (none)\n")
	} else {
		for name, comp := range config.Components {
			envList := strings.Join(comp.Environments, ", ")
			if envList == "" {
				envList = "none"
			}
			summary.WriteString(fmt.Sprintf("  - %s (%s) -> %s\n", name, comp.Role, envList))
		}
	}
	summary.WriteString("\n")

	// Next steps
	if config.DryRun {
		summary.WriteString("Next steps:\n")
		summary.WriteString("  1. Review the preview below\n")
		summary.WriteString("  2. Run without --dry-run to save the configuration\n")
		summary.WriteString("  3. Validate: deployah validate\n")
		summary.WriteString("  4. Deploy: deployah deploy\n")
	} else {
		summary.WriteString("Next steps:\n")
		summary.WriteString("  1. Review: cat " + config.OutputPath + "\n")
		summary.WriteString("  2. Validate: deployah validate\n")
		summary.WriteString("  3. Deploy: deployah deploy\n")
	}

	summaryForm := createNoteForm(progress.GetCurrentStep(), summary.String())

	if err := collectWithForm(summaryForm, "failed to show summary"); err != nil {
		return fmt.Errorf("failed to show summary: %w", err)
	}

	// Perform end-to-end validation before saving
	if err := validateManifestAndEnvironments(config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Create the manifest with proper defaults
	manifestData := manifest.Manifest{
		ApiVersion:   "v1-alpha.1",
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	// Apply defaults to the manifest
	if err := manifest.FillManifestWithDefaults(&manifestData, manifestData.ApiVersion); err != nil {
		return fmt.Errorf("failed to apply defaults to manifest: %w", err)
	}

	if config.DryRun {
		// Show preview instead of saving
		return showManifestPreview(&manifestData)
	} else {
		// Save the manifest to file
		if err := manifest.Save(&manifestData, config.OutputPath); err != nil {
			return fmt.Errorf("failed to save manifest to %s: %w", config.OutputPath, err)
		}
	}

	return nil
}

// validateManifestAndEnvironments performs end-to-end validation by building the manifest
// and environments objects and validating them against the JSON schema
func validateManifestAndEnvironments(config *ProjectConfig) error {
	// Create the manifest object as it would be written to file
	manifestData := manifest.Manifest{
		ApiVersion:   "v1-alpha.1", // Use latest schema version
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	// Convert to YAML and back to map[string]any for validation
	manifestBytes, err := yaml.Marshal(&manifestData)
	if err != nil {
		return fmt.Errorf("failed to convert manifest to YAML: %w", err)
	}

	var manifestObj map[string]any
	if err := yaml.Unmarshal(manifestBytes, &manifestObj); err != nil {
		return fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	// Validate the manifest against the schema
	if err := manifest.ValidateManifest(manifestObj, manifestData.ApiVersion); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	// Note: Environment validation would be done here if we had environment files
	// For now, the environments are embedded in the manifest so they're already validated

	return nil
}

// showManifestPreview displays a preview of the generated manifest in dry-run mode
func showManifestPreview(manifestData *manifest.Manifest) error {
	// Convert manifest to YAML for display
	manifestYAML, err := yaml.Marshal(manifestData)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest for preview: %w", err)
	}

	// Create preview content
	var preview strings.Builder
	preview.WriteString(DryRunPreviewHeader)
	preview.WriteString(string(manifestYAML))
	preview.WriteString(DryRunPreviewFooter)

	// Display the preview
	fmt.Println(preview.String())

	return nil
}
