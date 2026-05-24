package shell

import (
	"fmt"

	"nabat.dev/nabat"
)

// selectComponentInteractively shows an interactive selection for components
func selectComponentInteractively(c *nabat.Context, components []string) (string, error) {
	if len(components) == 1 {
		return components[0], nil
	}

	return nabat.Select(c,
		"Select Component — Choose which component to connect to:",
		components,
		components[0],
	)
}

// selectContainerInteractively shows an interactive selection for containers
func selectContainerInteractively(c *nabat.Context, containers []string, componentName string) (string, error) {
	if len(containers) == 1 {
		return containers[0], nil
	}

	defaultContainer := containers[0]
	for _, container := range containers {
		if container == componentName {
			defaultContainer = container
			break
		}
	}

	return nabat.Select(c,
		fmt.Sprintf("Select Container — Choose which container in component '%s' to connect to:", componentName),
		containers,
		defaultContainer,
	)
}

// selectEnvironmentInteractively shows an interactive selection for environments
func selectEnvironmentInteractively(c *nabat.Context, environments []string, projectName, componentName string) (string, error) {
	if len(environments) == 1 {
		return environments[0], nil
	}

	return nabat.Select(c,
		fmt.Sprintf("Select Environment — Choose which environment for project '%s' component '%s' to connect to:", projectName, componentName),
		environments,
		environments[0],
	)
}
