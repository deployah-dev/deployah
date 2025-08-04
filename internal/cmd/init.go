package cmd

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// Constants for default values and validation
const (
	DefaultOutputFile   = ".deployah.yaml"
	MinPortNumber       = 1
	MaxPortNumber       = 65535
	DefaultCPUThreshold = 75
	DefaultMinReplicas  = 2
	DefaultMaxReplicas  = 5
)

// Step progress indicators
const (
	StepProjectName  = "Step 1/4: Project Name"
	StepEnvironments = "Step 2/4: Environments"
	StepComponents   = "Step 3/4: Components"
	StepSummary      = "Step 4/4: Summary"
)

// ProjectConfig holds the collected configuration data
type ProjectConfig struct {
	Name         string
	Environments []manifest.Environment
	Components   map[string]manifest.Component
	OutputPath   string
}

// validateEnvironmentNameUnique validates that the environment name is unique and valid
func validateEnvironmentNameUnique(name string, existing []string) error {
	if err := manifest.ValidateEnvName(name); err != nil {
		return err
	}
	if slices.Contains(existing, name) {
		return fmt.Errorf("environment '%s' already exists", name)
	}
	return nil
}

// validateComponentNameUnique validates that the component name is unique and valid
func validateComponentNameUnique(name string, existing map[string]manifest.Component) error {
	if err := manifest.ValidateComponentName(name); err != nil {
		return err
	}
	if _, exists := existing[name]; exists {
		return fmt.Errorf("component '%s' already exists", name)
	}
	return nil
}

// validatePortNumber validates that the port number is a valid number between 1 and 65535
func validatePortNumber(portStr string) error {
	if portStr == "" {
		return fmt.Errorf("port cannot be empty")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("port '%s' is invalid: must be a valid number", portStr)
	}
	if port < MinPortNumber || port > MaxPortNumber {
		return fmt.Errorf("port %d is invalid: must be between %d and %d", port, MinPortNumber, MaxPortNumber)
	}
	return nil
}

// validatePositiveInteger validates that the value is a positive integer
func validatePositiveInteger(value string, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
	}
	val, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s '%s' is invalid: must be a positive integer", fieldName, value)
	}
	if val < 1 {
		return fmt.Errorf("%s %d is invalid: must be a positive integer", fieldName, val)
	}
	return nil
}

// validateNonEmpty validates that the value is not empty
func validateNonEmpty(value string, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", fieldName)
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

// createInputGroup creates an input group for a form
func createInputGroup(title, placeholder, description string, validator func(string) error, value *string) *huh.Group {
	input := huh.NewInput().
		Title(title).
		Placeholder(placeholder).
		Description(description).
		Value(value)

	if validator != nil {
		input.Validate(validator)
	}

	return huh.NewGroup(input)
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

// createSelectGroup creates a select group for a form
func createSelectGroup(title, description string, options []huh.Option[string], value *string) *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Description(description).
			Options(options...).
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

// createInputForm creates an input form
func createInputForm(title, placeholder, description string, validator func(string) error, value *string) *huh.Form {
	return huh.NewForm(createInputGroup(title, placeholder, description, validator, value))
}

// createConfirmForm creates a confirm form
func createConfirmForm(title, description, affirmative, negative string, value *bool) *huh.Form {
	return huh.NewForm(createConfirmGroup(title, description, affirmative, negative, value))
}

// createSelectForm creates a select form
func createSelectForm(title, description string, options []huh.Option[string], value *string) *huh.Form {
	return huh.NewForm(createSelectGroup(title, description, options, value))
}

// createMultiSelectForm creates a multi-select form
func createMultiSelectForm(title, description string, options []huh.Option[string], value *[]string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(title).
				Description(description).
				Options(options...).
				Value(value),
		),
	)
}

// createNoteForm creates a note form
func createNoteForm(title, description string) *huh.Form {
	return huh.NewForm(createNoteGroup(title, description))
}

// NewInitCommand creates and returns a new cobra command for initializing Deployah configuration.
func NewInitCommand() *cobra.Command {
	initCommand := &cobra.Command{
		Use:   "init [flags]",
		Short: "Initialize Deployah configuration for a new project",
		RunE:  runInit,
	}

	initCommand.Flags().StringP("output", "o", DefaultOutputFile, "The output file path.")

	return initCommand
}

// runInit is the main function for the init command
func runInit(cmd *cobra.Command, _ []string) error {
	logger := GetLogger(cmd)
	logger.Info("Starting project initialization", "cmd", "init")

	// Get output file path from flags
	outputPath, _ := cmd.Flags().GetString("output")

	config := &ProjectConfig{
		OutputPath: outputPath,
		Components: make(map[string]manifest.Component),
	}

	// Collect project configuration step by step
	if err := collectProjectName(config); err != nil {
		return err
	}

	if err := collectEnvironments(config); err != nil {
		return err
	}

	if err := collectComponents(config); err != nil {
		return err
	}

	if err := showSummaryAndSave(config); err != nil {
		return err
	}

	logger.Info("Project initialization completed successfully",
		"project", config.Name,
		"environments", len(config.Environments),
		"output", config.OutputPath)

	return nil
}

// collectProjectName collects the project name from user input
func collectProjectName(config *ProjectConfig) error {
	form := createInputForm(
		StepProjectName,
		"my-awesome-project",
		"What is the name of your project? Use lowercase letters, numbers, and dashes only.",
		manifest.ValidateProjectName,
		&config.Name,
	)

	return collectWithForm(form, "failed to get project details from user")
}

// collectEnvironments collects environment configuration from user input
func collectEnvironments(config *ProjectConfig) error {
	var selectedEnvironments []string
	var continueAddingEnvironments bool = true

	// Collect environment names
	for continueAddingEnvironments {
		var envName string
		var addAnother bool

		// Environment name form
		envNameGroup := createInputGroup(
			StepEnvironments,
			"dev",
			"Enter the name of an environment (e.g., development, staging, production, review/*)",
			func(s string) error {
				return validateEnvironmentNameUnique(s, selectedEnvironments)
			},
			&envName,
		)

		// Add another environment confirmation
		continueGroup := createConfirmGroup(
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

		if err := collectWithForm(envForm, "failed to get environment name"); err != nil {
			return err
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
		return env, err
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
			return nil, err
		}

		if varName != "" && varValue != "" {
			variables[varName] = varValue
		}
	}

	return variables, nil
}

// collectComponents collects component configuration from user input
func collectComponents(config *ProjectConfig) error {
	var continueAddingComponents bool = true
	selectedEnvironments := make([]string, len(config.Environments))
	for i, env := range config.Environments {
		selectedEnvironments[i] = env.Name
	}

	for continueAddingComponents {
		var componentName string
		var addAnotherComponent bool

		// Component name form
		compNameGroup := createInputGroup(
			"Component Name",
			"web",
			"Enter the name of the component (e.g., web, api, worker, db)",
			func(s string) error {
				return validateComponentNameUnique(s, config.Components)
			},
			&componentName,
		)

		continueGroup := createConfirmGroup(
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

		if err := collectWithForm(compForm, "failed to get component name"); err != nil {
			return err
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

	return nil
}

// collectComponentDetails collects detailed configuration for a single component
func collectComponentDetails(componentName string, availableEnvironments []string) (manifest.Component, error) {
	component := manifest.Component{}

	// Component role selection
	if err := collectComponentRole(&component, componentName); err != nil {
		return component, err
	}

	// Component kind selection
	if err := collectComponentKind(&component, componentName); err != nil {
		return component, err
	}

	// Image configuration
	if err := collectComponentImage(&component, componentName); err != nil {
		return component, err
	}

	// Port configuration
	if err := collectComponentPort(&component, componentName); err != nil {
		return component, err
	}

	// Resource preset selection
	if err := collectComponentResourcePreset(&component, componentName); err != nil {
		return component, err
	}

	// Component-specific configuration files
	if err := collectComponentConfigFiles(&component, componentName); err != nil {
		return component, err
	}

	// Command and arguments
	if err := collectComponentCommand(&component, componentName); err != nil {
		return component, err
	}

	if err := collectComponentArgs(&component, componentName); err != nil {
		return component, err
	}

	// Autoscaling configuration
	if err := collectComponentAutoscaling(&component, componentName); err != nil {
		return component, err
	}

	// Custom resources
	if err := collectComponentCustomResources(&component, componentName); err != nil {
		return component, err
	}

	// Ingress configuration
	if err := collectComponentIngress(&component, componentName); err != nil {
		return component, err
	}

	// Component environment variables
	if err := collectComponentEnvironmentVariables(&component, componentName); err != nil {
		return component, err
	}

	// Environment selection
	if err := collectComponentEnvironments(&component, componentName, availableEnvironments); err != nil {
		return component, err
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

	roleForm := createSelectForm(
		fmt.Sprintf("Role for %s", componentName),
		"What role does this component play in your application?",
		options,
		&roleChoice,
	)

	if err := collectWithForm(roleForm, "failed to get component role"); err != nil {
		return err
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

	kindForm := createSelectForm(
		fmt.Sprintf("Kind for %s", componentName),
		"What kind of component is this?",
		options,
		&kindChoice,
	)

	if err := collectWithForm(kindForm, "failed to get component kind"); err != nil {
		return err
	}

	component.Kind = manifest.ComponentKind(kindChoice)
	return nil
}

// collectComponentImage collects the component image
func collectComponentImage(component *manifest.Component, componentName string) error {
	var image string
	imageForm := createInputForm(
		fmt.Sprintf("Image for %s", componentName),
		"nginx:latest",
		"Docker image to use for this component",
		func(s string) error {
			return validateNonEmpty(s, "image")
		},
		&image,
	)

	if err := collectWithForm(imageForm, "failed to get component image"); err != nil {
		return err
	}

	component.Image = image
	return nil
}

// collectComponentPort collects the component port configuration
func collectComponentPort(component *manifest.Component, componentName string) error {
	var addPort bool
	portForm := createConfirmForm(
		fmt.Sprintf("Add port for %s?", componentName),
		"Does this component need to expose a port?",
		"Yes",
		"No",
		&addPort,
	)

	if err := collectWithForm(portForm, "failed to get port preference"); err != nil {
		return err
	}

	if addPort {
		var portStr string
		portInputForm := createInputForm(
			fmt.Sprintf("Port for %s", componentName),
			"8080",
			"Port number to expose",
			validatePortNumber,
			&portStr,
		)

		if err := collectWithForm(portInputForm, "failed to get port number"); err != nil {
			return err
		}

		port, _ := strconv.Atoi(portStr)
		component.Port = port
	}

	return nil
}

// collectComponentResourcePreset collects the component resource preset
func collectComponentResourcePreset(component *manifest.Component, componentName string) error {
	var resourcePreset string

	// Step 1: Clean preset selection
	options := []huh.Option[string]{
		huh.NewOption("Nano - Minimal resources", string(manifest.ResourcePresetNano)),
		huh.NewOption("Micro - Very light workloads", string(manifest.ResourcePresetMicro)),
		huh.NewOption("Small - Light workloads", string(manifest.ResourcePresetSmall)),
		huh.NewOption("Medium - Moderate workloads", string(manifest.ResourcePresetMedium)),
		huh.NewOption("Large - Heavy workloads", string(manifest.ResourcePresetLarge)),
		huh.NewOption("XLarge - Very heavy workloads", string(manifest.ResourcePresetXLarge)),
		huh.NewOption("2XLarge - Extremely heavy workloads", string(manifest.ResourcePreset2XLarge)),
	}

	resourceForm := createSelectForm(
		fmt.Sprintf("Resource Preset for %s", componentName),
		"Select a resource preset for this component",
		options,
		&resourcePreset,
	)

	if err := collectWithForm(resourceForm, "failed to get resource preset"); err != nil {
		return err
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
	configFilesForm := createConfirmForm(
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
	commandForm := createConfirmForm(
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
		commandInputForm := createInputForm(
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
	argsForm := createConfirmForm(
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
		argsInputForm := createInputForm(
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
	autoscalingForm := createConfirmForm(
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
					Validate(func(s string) error {
						return validatePositiveInteger(s, "minimum replicas")
					}).
					Value(&minReplicasStr),

				huh.NewInput().
					Title(fmt.Sprintf("Maximum Replicas for %s", componentName)).
					Placeholder(strconv.Itoa(DefaultMaxReplicas)).
					Description("Maximum number of replicas allowed").
					Validate(func(s string) error {
						return validatePositiveInteger(s, "maximum replicas")
					}).
					Value(&maxReplicasStr),
			),
		)

		if err := autoscalingConfigForm.Run(); err != nil {
			return fmt.Errorf("failed to get autoscaling config: %w", err)
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
					Value(&cpu),

				huh.NewInput().
					Title(fmt.Sprintf("Memory for %s", componentName)).
					Placeholder("512Mi").
					Description("Memory resource (e.g., 512Mi, 1Gi)").
					Value(&memory),

				huh.NewInput().
					Title(fmt.Sprintf("Ephemeral Storage for %s", componentName)).
					Placeholder("1Gi").
					Description("Ephemeral storage (e.g., 1Gi, 2Gi)").
					Value(&ephemeralStorage),
			),
		)

		if err := resourcesForm.Run(); err != nil {
			return fmt.Errorf("failed to get custom resources: %w", err)
		}

		resources := manifest.Resources{}
		if cpu != "" {
			resources.CPU = cpu
		}
		if memory != "" {
			resources.Memory = memory
		}
		if ephemeralStorage != "" {
			resources.EphemeralStorage = ephemeralStorage
		}

		component.Resources = resources
	}

	return nil
}

// collectComponentIngress collects the component ingress configuration
func collectComponentIngress(component *manifest.Component, componentName string) error {
	var addIngress bool
	ingressForm := createConfirmForm(
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
					Validate(func(s string) error {
						return validateNonEmpty(s, "host")
					}).
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

		component.Ingress = manifest.Ingress{
			Host: host,
			TLS:  tls,
		}
	}

	return nil
}

// collectComponentEnvironmentVariables collects component-specific environment variables
func collectComponentEnvironmentVariables(component *manifest.Component, componentName string) error {
	var addComponentEnvVars bool
	componentEnvForm := createConfirmForm(
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
	envForm := createMultiSelectForm(
		fmt.Sprintf("Environment Selection for %s", componentName),
		"Select one or more environments for this component",
		options,
		&selectedComponentEnvs,
	)

	if err := collectWithForm(envForm, "failed to get environment selection"); err != nil {
		return err
	}

	// Ensure at least one environment is selected
	if len(selectedComponentEnvs) == 0 {
		return fmt.Errorf("at least one environment must be selected for component %s", componentName)
	}

	component.Environments = selectedComponentEnvs
	return nil
}

// showSummaryAndSave displays a simple summary and saves the manifest
func showSummaryAndSave(config *ProjectConfig) error {
	// Build simple summary
	var summary strings.Builder

	summary.WriteString("Configuration Summary:\n\n")

	// Project info
	summary.WriteString(fmt.Sprintf("Project: %s\n", config.Name))
	summary.WriteString(fmt.Sprintf("Output: %s\n\n", config.OutputPath))

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
	summary.WriteString("Next steps:\n")
	summary.WriteString("  1. Review: cat " + config.OutputPath + "\n")
	summary.WriteString("  2. Validate: deployah validate\n")
	summary.WriteString("  3. Deploy: deployah deploy\n")

	summaryForm := createNoteForm(StepSummary, summary.String())

	if err := collectWithForm(summaryForm, "failed to show summary"); err != nil {
		return err
	}

	// Perform end-to-end validation before saving
	if err := validateManifestAndEnvironments(config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Create the manifest
	manifestData := manifest.Manifest{
		ApiVersion:   "v1-alpha.1",
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	// Save the manifest to file
	if err := manifest.Save(&manifestData, config.OutputPath); err != nil {
		return fmt.Errorf("failed to save manifest to %s: %w", config.OutputPath, err)
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
