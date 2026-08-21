package initialize

import (
	"errors"
	"fmt"
	"os"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
)

const (
	// DefaultEnvironmentName is the Kind environment offered in the wizard.
	DefaultEnvironmentName = "local"
)

// Locked user-facing copy. Tests assert these strings so a later edit fails.
const (
	promptLocalKind   = "Set up a local Kind cluster (deployah cluster up)?"
	promptOtherEnvs   = "Other environments (comma-separated). Empty is fine if you chose local."
	descComponentName = "e.g. web or api"
	promptImageFmt    = "Container image for %s (registry/name:tag), e.g. nginx:latest"
	promptAdvancedFmt = "Configure advanced options for %s? (kind, health, env vars, scaling). You can edit YAML later."
	promptExposeFmt   = "Give %s a public URL (%s.%s.nip.io) with HTTPS?"
)

// errNotInteractive is returned when init is run without a TTY.
var errNotInteractive = errors.New("deployah init is interactive: run it from a terminal")

// Options holds command-line flags for init.
type Options struct {
	DryRun bool `nabat:"dry-run"`
	Force  bool `nabat:"force"`
}

// ProjectConfig holds the collected configuration data.
type ProjectConfig struct {
	Name string
	// EnvironmentNames are registered in the platform file, not in the
	// generated spec: the spec's environments map is overrides-only.
	EnvironmentNames []string
	Components       map[string]spec.Component
	SpecPath         string
	PlatformPath     string
	DryRun           bool
}

// Register adds the init command to app.
func Register(app *nabat.App) {
	app.MustCommand("init",
		nabat.WithDescription("Creates deployah.yaml and a platform file so you can deploy."),
		nabat.WithLongDescription("Creates deployah.yaml and a platform file so you can deploy. Init is interactive and must run from a terminal."),
		nabat.WithFlag("dry-run", false, nabat.WithUsage("Preview the generated spec without saving it")),
		nabat.WithFlag("force", false, nabat.WithUsage("Skip the overwrite prompt if the spec already exists")),
		nabat.WithExample(`
# Initialize a new project
deployah init

# Preview the generated spec without saving it
deployah init --dry-run

# Skip the overwrite prompt when a spec already exists
deployah init --force`),
		nabat.WithRun(runInit),
	)
}

// runInit is the entry point for the init command.
func runInit(c *nabat.Context) error {
	// TODO(nabat): replace with nabat.WithInteractive when
	// https://github.com/nabat-dev/nabat/issues/4 ships.
	if !c.IsInteractive() {
		return errNotInteractive
	}

	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options failed: %w", err)
	}

	c.Logger().Debug("starting project initialization")

	sess := session.FromContext(c)
	specPath, platformPath := sess.SpecPath(), sess.PlatformPath()

	proceed, overwriteErr := checkOverwrite(c, specPath, opts.DryRun || opts.Force)
	if overwriteErr != nil {
		return overwriteErr
	}
	if !proceed {
		return nil
	}

	config := &ProjectConfig{
		SpecPath:     specPath,
		PlatformPath: platformPath,
		Components:   make(map[string]spec.Component),
		DryRun:       opts.DryRun,
	}

	if err := collectProjectName(c, config); err != nil {
		return err
	}

	if err := collectEnvironments(c, config); err != nil {
		return err
	}

	if err := collectComponents(c, config); err != nil {
		return err
	}

	saved, err := showSummaryAndSave(c, config)
	if err != nil {
		return err
	}
	if !saved {
		return nil
	}
	printInitCompleted(c, config)
	return nil
}

// collectProjectName prompts the user for a project name.
func collectProjectName(c *nabat.Context, config *ProjectConfig) error {
	name, err := c.Input(
		"Project name",
		nabat.WithHint("my-awesome-project"),
		nabat.WithValidate(spec.ValidateProjectName),
		nabat.WithInlineString(),
	)
	if err != nil {
		return fmt.Errorf("failed to collect project name: %w", err)
	}
	config.Name = name
	return nil
}

// printInitCompleted prints a success message when the init command completes.
func printInitCompleted(c *nabat.Context, config *ProjectConfig) {
	if config.DryRun {
		c.Success("Project initialization completed (dry-run mode)",
			"project", config.Name,
			"environments", len(config.EnvironmentNames),
			"components", len(config.Components))
		return
	}
	c.Success("Project initialization completed",
		"project", config.Name,
		"environments", len(config.EnvironmentNames),
		"components", len(config.Components),
		"spec", config.SpecPath)
}

// checkOverwrite guards against silently clobbering an existing spec file.
func checkOverwrite(c *nabat.Context, specPath string, skipPrompt bool) (bool, error) {
	if _, statErr := os.Stat(specPath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("stat %s: %w", specPath, statErr)
	}

	overwrite, confirmErr := c.Confirm(
		fmt.Sprintf("%s already exists. Overwrite it?", specPath),
		nabat.WithAffirmative("Yes, overwrite"),
		nabat.WithNegative("No, cancel"),
		nabat.WithYes(skipPrompt),
		nabat.WithBypassHint("--force"),
	)
	if confirmErr != nil {
		return false, confirmErr
	}
	if !overwrite {
		c.Info("Cancelled: " + specPath + " was not modified.")
		return false, nil
	}
	return true, nil
}
