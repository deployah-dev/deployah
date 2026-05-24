package initialize

import (
	"fmt"

	"github.com/deployah-dev/deployah/internal/manifest"
	"nabat.dev/nabat"
)

const (
	DefaultOutputFile   = "deployah.yaml"
	DefaultCPUThreshold = 75
	DefaultMinReplicas  = 2
	DefaultMaxReplicas  = 5
)

const (
	DryRunPreviewHeader = "=== DRY RUN MODE - PREVIEW OF GENERATED MANIFEST ===\n"
	DryRunPreviewFooter = "\n=== END PREVIEW ===\n\nThis is a preview. Use --dry-run=false to actually save the configuration."
)

const (
	StepProjectName  = "Step 1/4: Project Name"
	StepEnvironments = "Step 2/4: Environments"
	StepComponents   = "Step 3/4: Components"
	StepSummary      = "Step 4/4: Summary"
)

type Options struct {
	Output string `nabat:"output"`
	DryRun bool   `nabat:"dry-run"`
}

// ProjectConfig holds the collected configuration data
type ProjectConfig struct {
	Name         string
	Environments []manifest.Environment
	Components   map[string]manifest.Component
	OutputPath   string
	DryRun       bool
}

func Register(app *nabat.App) {
	app.MustCommand("init",
		nabat.WithDescription("Initialize Deployah configuration for a new project"),
		nabat.WithLongDescription("Initialize Deployah configuration for a new project"),
		nabat.WithAliases("initialize"),
		nabat.WithFlag("output", DefaultOutputFile, nabat.WithShort('o'), nabat.WithUsage("The output file path.")),
		nabat.WithFlag("dry-run", false, nabat.WithShort('d'), nabat.WithUsage("Preview the generated manifest without saving it")),
		nabat.WithExample(`
# Initialize a new project
deployah init

# Initialize a new project and save the configuration to a file
deployah init --output deployah.yaml

# Preview the generated manifest without saving it
deployah init --dry-run`),
		nabat.WithRun(runInit),
	)
}

func runInit(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options failed: %w", err)
	}

	c.Logger().Debug("starting project initialization")

	config := &ProjectConfig{
		OutputPath: opts.Output,
		Components: make(map[string]manifest.Component),
		DryRun:     opts.DryRun,
	}

	if err := collectProjectName(c, config); err != nil {
		return fmt.Errorf("failed to collect project name: %w", err)
	}

	if err := collectEnvironments(c, config); err != nil {
		return fmt.Errorf("failed to collect environments: %w", err)
	}

	if err := collectComponents(c, config); err != nil {
		return fmt.Errorf("failed to collect components: %w", err)
	}

	if err := showSummaryAndSave(c, config); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	if config.DryRun {
		c.Success("Project initialization completed (dry-run mode)",
			"project", config.Name,
			"environments", len(config.Environments),
			"components", len(config.Components),
			"output", config.OutputPath)
	} else {
		c.Success("Project initialization completed",
			"project", config.Name,
			"environments", len(config.Environments),
			"components", len(config.Components),
			"output", config.OutputPath)
	}

	return nil
}
