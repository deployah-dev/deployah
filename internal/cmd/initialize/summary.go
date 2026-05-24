package initialize

import (
	"fmt"
	"strings"

	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/manifest"
)

func showSummaryAndSave(c *nabat.Context, config *ProjectConfig) error {
	var summary strings.Builder

	summary.WriteString("Configuration Summary:\n\n")
	fmt.Fprintf(&summary, "Project: %s\n", config.Name)
	fmt.Fprintf(&summary, "Output: %s\n", config.OutputPath)
	if config.DryRun {
		summary.WriteString("Mode: DRY RUN (preview only)\n")
	} else {
		summary.WriteString("Mode: SAVE CONFIGURATION\n")
	}
	summary.WriteString("\n")

	summary.WriteString("Environments:\n")
	if len(config.Environments) == 0 {
		summary.WriteString("  (none)\n")
	} else {
		for _, env := range config.Environments {
			fmt.Fprintf(&summary, "  - %s\n", env.Name)
		}
	}
	summary.WriteString("\n")

	summary.WriteString("Components:\n")
	if len(config.Components) == 0 {
		summary.WriteString("  (none)\n")
	} else {
		for name, comp := range config.Components {
			envList := strings.Join(comp.Environments, ", ")
			if envList == "" {
				envList = "none"
			}
			fmt.Fprintf(&summary, "  - %s (%s) -> %s\n", name, comp.Role, envList)
		}
	}
	summary.WriteString("\n")

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

	err := c.Form(
		nabat.WithFormNote(StepSummary, summary.String()),
	)
	if err != nil {
		return fmt.Errorf("failed to show summary: %w", err)
	}

	if err = validateManifestAndEnvironments(config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	manifestData := manifest.Manifest{
		APIVersion:   "v1-alpha.1",
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	if err = manifest.FillManifestWithDefaults(&manifestData, manifestData.APIVersion); err != nil {
		return fmt.Errorf("failed to apply defaults to manifest: %w", err)
	}

	if config.DryRun {
		return showManifestPreview(c, &manifestData)
	}

	if err = manifest.Save(&manifestData, config.OutputPath); err != nil {
		return fmt.Errorf("failed to save manifest to %s: %w", config.OutputPath, err)
	}

	return nil
}

func validateManifestAndEnvironments(config *ProjectConfig) error {
	manifestData := manifest.Manifest{
		APIVersion:   "v1-alpha.1",
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	manifestBytes, err := yaml.Marshal(&manifestData)
	if err != nil {
		return fmt.Errorf("failed to convert manifest to YAML: %w", err)
	}

	var manifestObj map[string]any
	if err = yaml.Unmarshal(manifestBytes, &manifestObj); err != nil {
		return fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	if err = manifest.ValidateManifest(manifestObj, manifestData.APIVersion); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	return nil
}

func showManifestPreview(c *nabat.Context, manifestData *manifest.Manifest) error {
	manifestYAML, err := yaml.Marshal(manifestData)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest for preview: %w", err)
	}

	var preview strings.Builder
	preview.WriteString(DryRunPreviewHeader)
	if _, writeErr := preview.Write(manifestYAML); writeErr != nil {
		return fmt.Errorf("failed to write manifest preview: %w", writeErr)
	}
	preview.WriteString(DryRunPreviewFooter)

	c.Println(preview.String())

	return nil
}
