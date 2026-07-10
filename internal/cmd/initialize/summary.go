package initialize

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"helm.sh/helm/v4/pkg/strvals"
	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/spec"
)

func showSummaryAndSave(c *nabat.Context, config *ProjectConfig) error {
	specData, err := buildValidatedSpec(config)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	specYAML, err := yaml.Marshal(specData)
	if err != nil {
		return fmt.Errorf("failed to marshal spec for preview: %w", err)
	}

	if err = c.Highlight(string(specYAML), "yaml"); err != nil {
		return fmt.Errorf("failed to render spec preview: %w", err)
	}

	if config.DryRun {
		c.Info("dry-run mode; no files written")
		c.Println("Run again without --dry-run to save the configuration.")
		return nil
	}

	// WithDefault(true) lets non-interactive / CI mode save without prompting.
	save, err := c.Confirm(
		fmt.Sprintf("Save to %s?", config.OutputPath),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
		nabat.WithDefault(true),
	)
	if err != nil {
		return fmt.Errorf("failed to confirm save: %w", err)
	}
	if !save {
		c.Info("aborted; no files written")
		return nil
	}

	if err = spec.Save(specData, config.OutputPath); err != nil {
		return fmt.Errorf("failed to save spec to %s: %w", config.OutputPath, err)
	}
	c.Success("Created " + config.OutputPath + " (" + spec.CurrentManifestVersion + ")")

	// The platform file owns which environments exist, so every selected
	// name is registered there; an existing file is never overwritten.
	platformPath := filepath.Join(filepath.Dir(config.OutputPath), spec.DefaultPlatformPath)
	envNames := slices.Sorted(slices.Values(config.EnvironmentNames))

	created, platformErr := spec.ScaffoldPlatformFile(platformPath, "127.0.0.1", envNames)
	switch {
	case platformErr != nil:
		// Non-fatal: print warning and continue.
		c.Warn(fmt.Sprintf("failed to write platform file: %v", platformErr))
	case created:
		c.Success(fmt.Sprintf("Created %s (%s) with %s",
			platformPath, spec.CurrentPlatformVersion, joinWithAnd(envNames)))
	default:
		c.Info(platformPath + " already exists; no changes made.")
	}
	printPlatformEnvironmentGuidance(c, platformPath, envNames)

	c.Println("Next steps:")
	c.Println("  1. Review: cat " + config.OutputPath)
	c.Println("  2. Validate: deployah validate")
	c.Println("  3. Deploy: deployah deploy")

	return nil
}

// printPlatformEnvironmentGuidance reports env names with no entry in the
// platform file and registered entries with no context, so the user knows
// what to fill in before deploying.
func printPlatformEnvironmentGuidance(c *nabat.Context, platformPath string, envNames []string) {
	var platform *spec.PlatformConfig
	if _, statErr := os.Stat(platformPath); statErr == nil {
		loaded, loadErr := spec.LoadPlatform(platformPath)
		if loadErr != nil {
			c.Warn(fmt.Sprintf("failed to read %s to check environment coverage: %v", platformPath, loadErr))
			return
		}
		platform = loaded
	}

	if missing := spec.MissingPlatformEnvironments(platform, envNames); len(missing) > 0 {
		verb := "has"
		if len(missing) > 1 {
			verb = "have"
		}
		c.Println(fmt.Sprintf(
			"%s %s no platform entry yet.\nAdd them to %s with a context before deploying there.\nSee: https://deployah.dev/docs/platform-file",
			joinWithAnd(missing), verb, platformPath,
		))
	}

	if platform == nil {
		return
	}
	var noContext []string
	for _, name := range envNames {
		if pe, ok := platform.Environments[name]; ok && pe.Context == "" {
			noContext = append(noContext, name)
		}
	}
	if len(noContext) == 0 {
		return
	}
	verb := "has"
	if len(noContext) > 1 {
		verb = "have"
	}
	c.Println(fmt.Sprintf(
		"%s %s no context yet: deploys there follow your current kubeconfig context.\nSet a context in %s before deploying somewhere real.\nSee: https://deployah.dev/docs/platform-file",
		joinWithAnd(noContext), verb, platformPath,
	))
}

// joinWithAnd joins items with commas and a trailing "and", e.g.
// ["a"] -> "a", ["a", "b"] -> "a and b", ["a", "b", "c"] -> "a, b, and c".
func joinWithAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

// expandExposeShorthand rewrites boolean expose values in the raw spec
// object: true becomes an empty object (so --set can address nested fields)
// and false removes the block.
func expandExposeShorthand(specObj map[string]any) {
	comps, ok := specObj["components"].(map[string]any)
	if !ok {
		return
	}
	for _, v := range comps {
		comp, isMap := v.(map[string]any)
		if !isMap {
			continue
		}
		switch comp["expose"] {
		case true:
			comp["expose"] = map[string]any{}
		case false:
			delete(comp, "expose")
		}
	}
}

// buildValidatedSpec assembles a [spec.Spec] from config, merges any --set
// overrides, and validates the result the same way deployah validate would.
func buildValidatedSpec(config *ProjectConfig) (*spec.Spec, error) {
	// Environments are deliberately absent: the platform file registers them.
	specData := spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    config.Name,
		Components: config.Components,
	}

	specBytes, err := yaml.Marshal(&specData)
	if err != nil {
		return nil, fmt.Errorf("failed to convert spec to YAML: %w", err)
	}

	var specObj map[string]any
	if err = yaml.Unmarshal(specBytes, &specObj); err != nil {
		return nil, fmt.Errorf("failed to parse spec YAML: %w", err)
	}

	// Helm-style dotted paths, e.g. "components.web.image=nginx:1.25",
	// merged in before validation below so a wrong path fails with the
	// same schema error deployah validate would give. The expose shorthand
	// is expanded around each set: strvals cannot descend into a bool.
	expandExposeShorthand(specObj)
	for _, kv := range config.Sets {
		if err = strvals.ParseIntoString(kv, specObj); err != nil {
			return nil, fmt.Errorf("--set %q: %w", kv, err)
		}
		if err = spec.CoerceSetValue(kv, specObj, specData.APIVersion); err != nil {
			return nil, fmt.Errorf("--set %q: %w", kv, err)
		}
		expandExposeShorthand(specObj)
	}

	if err = spec.ValidateSpec(specObj, specData.APIVersion); err != nil {
		return nil, fmt.Errorf("spec validation failed: %w", err)
	}

	mergedBytes, err := yaml.Marshal(specObj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert merged spec to YAML: %w", err)
	}
	if err = yaml.Unmarshal(mergedBytes, &specData); err != nil {
		return nil, fmt.Errorf("failed to parse merged spec YAML: %w", err)
	}

	// Schema validation only checks shape; run the cross-field checks (e.g.
	// health checks requiring a port) so init catches the same mistakes
	// deployah validate would, instead of writing a file that fails later
	// at apply time.
	if err = spec.ValidateSpecComponents(&specData); err != nil {
		return nil, fmt.Errorf("component validation failed: %w", err)
	}

	if err = spec.FillSpecWithDefaults(&specData, specData.APIVersion); err != nil {
		return nil, fmt.Errorf("failed to apply defaults to spec: %w", err)
	}

	return &specData, nil
}
