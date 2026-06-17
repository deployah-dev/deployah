package validate

import (
	"fmt"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/runtime"
)

// Options holds command-line flags for validate.
type Options struct {
	Environment string `nabat:"environment"`
}

// Register adds the validate command to app.
func Register(app *nabat.App) {
	app.MustCommand("validate",
		nabat.WithDescription("Validate a Deployah spec for a specific environment"),
		nabat.WithLongDescription("Validate a Deployah spec for a specific environment against the JSON schema."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to validate"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. production"))),
		nabat.WithExample(`
# Validate a spec for a specific environment using the default spec path (./deployah.yaml)
deployah validate production

# Validate a spec for a specific environment with an explicit spec path
deployah validate staging -s ./path/to/deployah.yaml`),
		nabat.WithRun(runValidate),
	)
}

func runValidate(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	rt := runtime.FromContext(c)

	m, err := rt.Manifest(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	c.Success("Spec validated", "project", m.Project, "environment", opts.Environment)

	return nil
}
