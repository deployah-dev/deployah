package initialize

import (
	"fmt"
	"strings"

	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/spec"
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

	if err = validateSpecAndEnvironments(config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	specData := spec.Spec{
		APIVersion:   "v1-alpha.1",
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	if err = spec.FillSpecWithDefaults(&specData, specData.APIVersion); err != nil {
		return fmt.Errorf("failed to apply defaults to spec: %w", err)
	}

	if config.DryRun {
		return showSpecPreview(c, &specData)
	}

	if err = spec.Save(&specData, config.OutputPath); err != nil {
		return fmt.Errorf("failed to save spec to %s: %w", config.OutputPath, err)
	}

	return nil
}

func validateSpecAndEnvironments(config *ProjectConfig) error {
	specData := spec.Spec{
		APIVersion:   "v1-alpha.1",
		Project:      config.Name,
		Environments: config.Environments,
		Components:   config.Components,
	}

	specBytes, err := yaml.Marshal(&specData)
	if err != nil {
		return fmt.Errorf("failed to convert spec to YAML: %w", err)
	}

	var specObj map[string]any
	if err = yaml.Unmarshal(specBytes, &specObj); err != nil {
		return fmt.Errorf("failed to parse spec YAML: %w", err)
	}

	if err = spec.ValidateSpec(specObj, specData.APIVersion); err != nil {
		return fmt.Errorf("spec validation failed: %w", err)
	}

	return nil
}

func showSpecPreview(c *nabat.Context, specData *spec.Spec) error {
	specYAML, err := yaml.Marshal(specData)
	if err != nil {
		return fmt.Errorf("failed to marshal spec for preview: %w", err)
	}

	var preview strings.Builder
	preview.WriteString(DryRunPreviewHeader)
	if _, writeErr := preview.Write(specYAML); writeErr != nil {
		return fmt.Errorf("failed to write spec preview: %w", writeErr)
	}
	preview.WriteString(DryRunPreviewFooter)

	c.Println(preview.String())

	return nil
}
