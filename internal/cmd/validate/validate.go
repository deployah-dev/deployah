package validate

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
)

// Options holds command-line flags for validate.
type Options struct {
	Environment string `nabat:"environment"`
}

// Register adds the validate command to app.
func Register(app *nabat.App) {
	app.MustCommand("validate",
		nabat.WithDescription("Validate a Deployah spec"),
		nabat.WithLongDescription("Validate a Deployah spec against the JSON schema. "+
			"Without --environment, validates the manifest only (offline, fast). "+
			"With --environment, also loads the platform file and runs cross-file resolution validation."),
		nabat.WithArg("environment", "", nabat.WithUsage("Environment to validate (optional; enables cross-file resolution check)"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. production"))),
		nabat.WithExample(`
# Validate manifest schema only (offline, no environment required)
deployah validate

# Validate manifest + cross-file resolution against the platform file
deployah validate production

# Validate with an explicit spec path
deployah validate staging -s ./path/to/deployah.yaml`),
		nabat.WithRun(runValidate),
	)
}

func runValidate(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	rt := session.FromContext(c)

	if opts.Environment == "" {
		return runManifestOnly(c, rt)
	}
	return runCrossFile(c, rt, opts.Environment)
}

// runManifestOnly validates only the manifest schema without environment
// resolution. It applies type-aware sentinel substitution so ${VAR} tokens
// do not cause false format-assertion failures, while literal typos still fail.
func runManifestOnly(c *nabat.Context, rt *session.Session) error {
	specPath := rt.SpecPath()
	if specPath == "" {
		specPath = spec.DefaultSpecPath
	}

	data, err := os.ReadFile(specPath) // #nosec G304
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	// Sentinel-substitute ${VAR} tokens so format assertions still fire for
	// literal values while tolerating unresolved placeholders.
	data = spec.SentinelSubstituteRaw(data)

	var specObj map[string]any
	if err = yaml.Unmarshal(data, &specObj); err != nil {
		return fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	version, err := spec.ValidateAPIVersion(specObj)
	if err != nil {
		return fmt.Errorf("invalid apiVersion: %w", err)
	}

	if err = spec.ValidateEnvironments(specObj, version); err != nil {
		return fmt.Errorf("environments invalid: %w", err)
	}

	if err = spec.ValidateSpec(specObj, version); err != nil {
		return fmt.Errorf("manifest invalid: %w", err)
	}

	c.Success("Manifest schema valid")
	c.Println("Hint: run 'deployah validate <environment>' to also check cross-file resolution against the platform file.")

	return nil
}

// runCrossFile validates manifest schema AND loads the platform file to run
// full cross-file resolution validation for the named environment.
func runCrossFile(c *nabat.Context, rt *session.Session, environment string) error {
	rawSpec, err := rt.ParseManifest()
	if err != nil {
		return fmt.Errorf("manifest invalid: %w", err)
	}

	platform, platformErr := rt.Platform()
	if platformErr != nil {
		return fmt.Errorf("platform file error: %w", platformErr)
	}
	if platform == nil {
		return fmt.Errorf(
			"cross-file validation requires a platform file; "+
				"create %s or pass --platform-file",
			spec.DefaultPlatformPath,
		)
	}

	substReport := spec.PrescanSubstitutionReport(rawSpec)
	envIdentity := spec.NormalizeEnv(environment)
	resolvedSpec, report, resolveErr := spec.Resolve(rawSpec, platform, envIdentity, substReport)
	if resolveErr != nil {
		return fmt.Errorf("resolution failed: %w", resolveErr)
	}

	_ = resolvedSpec

	// Surface any warnings from the resolution report.
	for _, w := range report.Warnings {
		c.Warn(w)
	}

	c.Success("Manifest and platform valid", "project", rawSpec.Project, "environment", environment)

	return nil
}
