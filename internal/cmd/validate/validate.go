package validate

import (
	"fmt"
	"os"
	"strings"

	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"

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
			"Without an environment, validates the manifest (offline, fast) and, when a "+
			"platform file exists, cross-checks expose.domain keys and environment names against it. "+
			"With an environment, also runs resolution for that environment, including runtime env files. "+
			"A platform file is required only when the spec uses platform-owned features such as profiles or expose."),
		nabat.WithArg("environment", "", nabat.WithUsage("Environment to validate (optional; enables cross-file resolution check)"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. production"))),
		nabat.WithExample(`
# Validate manifest schema only (offline, no environment required)
deployah validate

# Validate manifest + environment resolution (runtime env; platform only if required)
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

	// Cross-field and platform checks are best-effort here: they need the
	// raw (pre-substitution) spec, so a manifest that only unmarshals after
	// ${VAR} substitution skips them with a warning.
	rawSpec, _, parseErr := spec.ParseManifest(specPath)
	if parseErr != nil {
		c.Warn(fmt.Sprintf("skipping cross-field checks: %v", parseErr))
	} else {
		if compErr := spec.ValidateSpecComponents(rawSpec); compErr != nil {
			return compErr
		}
		if taskErr := spec.ValidateSpecTasks(rawSpec); taskErr != nil {
			return taskErr
		}
		platform, platformErr := rt.Platform()
		if platformErr != nil {
			return fmt.Errorf("platform file error: %w", platformErr)
		}
		if platform != nil {
			problems, warnings := spec.CrossCheckPlatformReferences(rawSpec, platform)
			for _, w := range warnings {
				c.Warn(w)
			}
			if len(problems) > 0 {
				return fmt.Errorf("platform cross-check failed:\n  - %s", strings.Join(problems, "\n  - "))
			}
		}
	}

	c.Success("Manifest schema valid")
	c.Println("Hint: run 'deployah validate <environment>' to also check environment resolution, including runtime env files.")

	return nil
}

// runCrossFile validates the substituted spec and runs [spec.Resolve] for
// the named environment. A missing platform file is allowed when the spec
// does not use platform-owned features.
func runCrossFile(c *nabat.Context, rt *session.Session, environment string) error {
	rawSpec, _, err := spec.ParseManifest(rt.SpecPath())
	if err != nil {
		return fmt.Errorf("manifest invalid: %w", err)
	}

	platform, platformErr := rt.Platform()
	if platformErr != nil {
		return fmt.Errorf("platform file error: %w", platformErr)
	}

	substReport := spec.PrescanSubstitutionReport(rawSpec)

	loaded, err := spec.Load(c, rt.SpecPath(), environment, platform)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	envIdentity := spec.NormalizeEnv(environment)
	_, report, resolveErr := spec.Resolve(loaded, platform, envIdentity, substReport)
	if resolveErr != nil {
		if report != nil && report.ErrorCode != "" {
			return fmt.Errorf("resolution failed (%s): %w", report.ErrorCode, resolveErr)
		}
		return fmt.Errorf("resolution failed: %w", resolveErr)
	}

	for _, w := range report.Warnings {
		c.Warn(w)
	}

	c.Success("Manifest and environment valid", "project", loaded.Project, "environment", environment)

	return nil
}
