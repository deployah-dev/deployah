package initialize

import (
	"fmt"
	"os"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
)

const (
	// DefaultOutputFile is the default spec path written by init.
	DefaultOutputFile = "deployah.yaml"
	// DefaultCPUThreshold is the default HPA CPU target percentage.
	DefaultCPUThreshold = 75
	// DefaultMinReplicas is the default minimum replica count for HPA.
	DefaultMinReplicas = 2
	// DefaultMaxReplicas is the default maximum replica count for HPA.
	DefaultMaxReplicas = 5
)

const (
	// DefaultEnvironmentName is used when init runs without prompting.
	DefaultEnvironmentName = "local"
	// DefaultComponentName is the single component created when init runs
	// without prompting.
	DefaultComponentName = "app"
	// DefaultComponentImage is an unresolvable placeholder (no registry, no
	// tag), so a forgotten replacement fails fast at the first deploy
	// instead of shipping a real-looking image that isn't theirs.
	DefaultComponentImage = "REPLACE_ME"
)

const (
	// StepProjectName is the prompt title for the project name step.
	StepProjectName = "Step 1/3: Project Name"
	// StepEnvironments is the prompt title for the environments step.
	StepEnvironments = "Step 2/3: Environments"
	// StepComponents is the prompt title for the components step.
	StepComponents = "Step 3/3: Components"
)

// Options holds command-line flags for init.
type Options struct {
	Output       string   `nabat:"output"`
	DryRun       bool     `nabat:"dry-run"`
	Force        bool     `nabat:"force"`
	Project      string   `nabat:"project"`
	Environments []string `nabat:"environments"`
	Set          []string `nabat:"set"`
	Defaults     bool     `nabat:"defaults"`
}

// ProjectConfig holds the collected configuration data
type ProjectConfig struct {
	Name string
	// EnvironmentNames are registered in the platform file, not in the
	// generated spec: the spec's environments map is overrides-only.
	EnvironmentNames []string
	Components       map[string]spec.Component
	OutputPath       string
	DryRun           bool
	// Sets holds --set overrides (Helm-style dotted paths, e.g.
	// "components.web.image=nginx:1.25"), applied on top of Name and
	// Components right before validation.
	Sets []string
}

// Register adds the init command to app.
func Register(app *nabat.App) {
	app.MustCommand("init",
		nabat.WithDescription("Create a new Deployah spec for a project"),
		nabat.WithLongDescription("Create a new Deployah spec for a project"),
		nabat.WithAliases("initialize"),
		nabat.WithFlag("output", DefaultOutputFile, nabat.WithShort('o'), nabat.WithUsage("The output file path.")),
		nabat.WithFlag("dry-run", false, nabat.WithUsage("Preview the generated spec without saving it")),
		nabat.WithFlag("force", false, nabat.WithUsage("Overwrite the output file if it already exists")),
		nabat.WithFlag("project", "", nabat.WithUsage("Project name (skips the project name prompt)"), nabat.WithEnv("project")),
		nabat.WithFlag("environments", []string{}, nabat.WithUsage("Comma-separated environment names, e.g. local,production (skips the environments prompt)"), nabat.WithEnv("environments")),
		nabat.WithFlag("set", []string{}, nabat.WithUsage("Set a value on the generated spec using a Helm-style dotted path, e.g. components.web.image=nginx:1.25 or components.web.port=8080 (repeatable). Values are coerced to int/number/bool only where the manifest schema declares that field's type; everything else stays a string.")),
		nabat.WithFlag("defaults", false, nabat.WithUsage("Skip every prompt and use built-in defaults")),
		nabat.WithExample(`
# Initialize a new project
deployah init

# Initialize a new project and save the spec to a file
deployah init --output deployah.yaml

# Preview the generated spec without saving it
deployah init --dry-run

# Overwrite an existing spec without a confirmation prompt
deployah init --force

# Skip every prompt and use built-in defaults
deployah init --defaults --force

# Non-interactive: set project, environments, and a component field directly
deployah init --defaults --force --project shop --environments local,production \
  --set components.app.image=nginx:1.25`),
		nabat.WithRun(runInit),
	)
}

func runInit(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options failed: %w", err)
	}

	c.Logger().Debug("starting project initialization")

	proceed, err := checkOverwrite(c, opts)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	// Without a TTY, prompts have no way to ask a human and nabat errors on
	// any field without a fallback. Falling back to the same defaults
	// --defaults uses makes `deployah init < /dev/null` behave like
	// `deployah init --defaults` instead of failing on the first prompt.
	useDefaults := opts.Defaults || !c.IsInteractive()

	config := &ProjectConfig{
		OutputPath: opts.Output,
		Components: make(map[string]spec.Component),
		DryRun:     opts.DryRun,
		Sets:       opts.Set,
	}

	if err = resolveProjectName(c, config, opts.Project, useDefaults); err != nil {
		return fmt.Errorf("failed to resolve project name: %w", err)
	}

	if err = resolveEnvironments(c, config, opts.Environments, useDefaults); err != nil {
		return fmt.Errorf("failed to resolve environments: %w", err)
	}

	if err = resolveComponents(c, config, useDefaults); err != nil {
		return fmt.Errorf("failed to resolve components: %w", err)
	}

	if err = showSummaryAndSave(c, config); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	if config.DryRun {
		c.Success("Project initialization completed (dry-run mode)",
			"project", config.Name,
			"environments", len(config.EnvironmentNames),
			"components", len(config.Components))
	} else {
		c.Success("Project initialization completed",
			"project", config.Name,
			"environments", len(config.EnvironmentNames),
			"components", len(config.Components),
			"output", config.OutputPath)
	}

	return nil
}

// checkOverwrite guards against silently clobbering an existing spec file.
// It reports whether the caller should proceed with initialization.
func checkOverwrite(c *nabat.Context, opts *Options) (proceed bool, err error) {
	confirmOverwrite := func(prompt string) (bool, error) {
		return c.Confirm(prompt,
			nabat.WithAffirmative("Yes, overwrite"),
			nabat.WithNegative("No, cancel"),
		)
	}
	proceed, cancelMsg, err := resolveOverwrite(opts.Output, opts.Force, c.IsInteractive(), confirmOverwrite)
	if err != nil {
		return false, err
	}
	if cancelMsg != "" {
		c.Info(cancelMsg)
	}
	return proceed, nil
}

// resolveOverwrite implements the overwrite decision matrix, taking confirm
// as a func so the file-exists x force x interactive matrix can be unit
// tested without simulating a real TTY session.
func resolveOverwrite(path string, force, interactive bool, confirm func(prompt string) (bool, error)) (proceed bool, cancelMsg string, err error) {
	if force {
		return true, "", nil
	}

	if _, statErr := os.Stat(path); statErr != nil {
		return true, "", nil
	}

	if !interactive {
		// Fail closed with a plain, grep-friendly error instead of letting
		// nabat's Confirm crash with "requires interactive terminal".
		return false, "", fmt.Errorf("%s already exists; pass --force to overwrite", path)
	}

	// Only reached on the interactive, file-exists, non-force path.
	overwrite, confirmErr := confirm(fmt.Sprintf("%s already exists. Overwrite it?", path))
	if confirmErr != nil {
		return false, "", fmt.Errorf("failed to confirm overwrite: %w", confirmErr)
	}
	if !overwrite {
		// cancelMsg is non-empty only here, so the caller can print it.
		return false, "Cancelled: " + path + " was not modified.", nil
	}

	return true, "", nil
}
