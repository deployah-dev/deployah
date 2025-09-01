package shell

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/deployah-dev/deployah/internal/ui"
)

// selectComponentInteractively shows an interactive selection for components
func selectComponentInteractively(components []string) (string, error) {
	if len(components) == 1 {
		return components[0], nil
	}

	var componentChoice string
	options := make([]huh.Option[string], len(components))

	for i, name := range components {
		// Since we don't have manifest info, just show the component name
		options[i] = huh.NewOption(name, name)
	}

	componentForm := ui.CreateSelectForm(
		"Select Component",
		"Choose which component to connect to:",
		options,
		&componentChoice,
	)

	if err := ui.CollectWithForm(componentForm, "failed to select component"); err != nil {
		return "", fmt.Errorf("failed to select component: %w", err)
	}

	return componentChoice, nil
}

// selectContainerInteractively shows an interactive selection for containers
func selectContainerInteractively(containers []string, componentName string) (string, error) {
	if len(containers) == 1 {
		return containers[0], nil
	}

	var containerChoice string
	options := make([]huh.Option[string], len(containers))

	for i, container := range containers {
		description := container
		if container == componentName {
			description += " (main container)"
		}
		options[i] = huh.NewOption(description, container)
	}

	containerForm := ui.CreateSelectForm(
		"Select Container",
		fmt.Sprintf("Choose which container in component '%s' to connect to:", componentName),
		options,
		&containerChoice,
	)

	if err := ui.CollectWithForm(containerForm, "failed to select container"); err != nil {
		return "", fmt.Errorf("failed to select container: %w", err)
	}

	return containerChoice, nil
}

// selectEnvironmentInteractively shows an interactive selection for environments
func selectEnvironmentInteractively(environments []string, projectName, componentName string) (string, error) {
	if len(environments) == 1 {
		return environments[0], nil
	}

	var environmentChoice string
	options := make([]huh.Option[string], len(environments))

	for i, name := range environments {
		options[i] = huh.NewOption(name, name)
	}

	environmentForm := ui.CreateSelectForm(
		"Select Environment",
		fmt.Sprintf("Choose which environment for project '%s' component '%s' to connect to:", projectName, componentName),
		options,
		&environmentChoice,
	)

	if err := ui.CollectWithForm(environmentForm, "failed to select environment"); err != nil {
		return "", fmt.Errorf("failed to select environment: %w", err)
	}

	return environmentChoice, nil
}
