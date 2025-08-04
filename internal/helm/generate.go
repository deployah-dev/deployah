package helm

import (
	"embed"
	"regexp"
	"strings"

	"github.com/deployah-dev/deployah/internal/manifest"
)

//go:embed chart/templates/chart/**
var ChartTemplateFS embed.FS

// parseContainerImage parses a container image reference and returns the image repository and tag/digest.
// It handles various formats including:
// - simple: nginx:1.21
// - with registry port: registry.example.com:5000/nginx:1.21
// - with digest: nginx@sha256:abc123...
// - with registry port and digest: registry.example.com:5000/nginx@sha256:abc123...
// - with registry port and tag: registry.example.com:5000/nginx:1.21
func parseContainerImage(imageRef string) (repository, tag string) {
	if imageRef == "" {
		return "", ""
	}

	// Check if this is a digest reference (contains @)
	if strings.Contains(imageRef, "@") {
		parts := strings.SplitN(imageRef, "@", 2)
		if len(parts) == 2 {
			return parts[0], parts[1] // repository, digest
		}
	}

	// For tag references, we need to be careful about registry ports
	// We want to split on the last colon that's not part of a port number

	// First, let's check if there's a tag separator
	lastColonIndex := strings.LastIndex(imageRef, ":")
	if lastColonIndex == -1 {
		// No colon found, return the whole string as repository with no tag
		return imageRef, ""
	}

	// Check if the part after the last colon looks like a port number or tag
	// Port numbers are typically numeric and at the beginning of the path
	// Tags can contain alphanumeric characters, dots, dashes, underscores
	potentialTag := imageRef[lastColonIndex+1:]

	// If the potential tag contains a slash, it's likely part of a registry port
	// e.g., "registry.example.com:5000/nginx" - the "5000/nginx" part contains a slash
	if strings.Contains(potentialTag, "/") {
		// This is likely a registry port, not a tag
		return imageRef, ""
	}

	// Check if it's a valid tag format (not just a port number)
	// Tags typically contain letters, numbers, dots, dashes, underscores
	// Port numbers are purely numeric
	tagPattern := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if tagPattern.MatchString(potentialTag) && !regexp.MustCompile(`^\d+$`).MatchString(potentialTag) {
		// This looks like a tag, not a port
		return imageRef[:lastColonIndex], potentialTag
	}

	// If we get here, it's likely a registry with port but no tag
	return imageRef, ""
}

// ChartData holds the values to substitute in the templates, including arbitrary values for values.yaml
// .Values can be used in values.yaml.gotmpl for flexible templating
// You can add more fields for project, component, etc.
type ChartData struct {
	Chart struct {
		Name        string
		Description string
		Version     string
		AppVersion  string
	}
	Values map[string]any // For values.yaml templating
}

func MapManifestToChartValues(m *manifest.Manifest) (map[string]any, error) {
	values := make(map[string]any)

	for name, component := range m.Components {
		// TODO: Add support for component roles such as "service", "worker", and "job"
		// TODO: Implement handling for component envFile
		//   Exclude Deployah-specific environment variables (those prefixed with DPY_VAR_) and provide the remaining variables to the component
		// TODO: Implement handling for component configFile
		//   Use a deep merge to merge the component's configFile with the environment's configFile
		//   Precedence:
		//   - config.yaml (base)
		//   - config.production.yaml (environment-specific)
		//   - config.api.yaml (component-specific)
		//   - config.api.production.yaml (environment and component-specific)
		// TODO: Add support for component environments
		//   The values must correspond to environment names defined in your external configuration.
		//   The environments are defined in the manifest.Environments field.
		// TODO: Add support for component kind
		//   The kind of component. Use 'stateless' Deployment type and 'stateful' StatefulSet type.
		// TODO: Add support for autoscaling
		// TODO: Add support for component env, The user can specify the environment variables for the component e.g. NODE_ENV=roduction

		image := ""
		tag := ""

		// Parse container image reference (supports registry ports and digest references)
		if component.Image != "" {
			image, tag = parseContainerImage(component.Image)
		}

		resources := map[string]any{}

		// Check if resources are provided (now already resolved from presets if applicable)
		hasResources := component.Resources.CPU != "" || component.Resources.Memory != "" || component.Resources.EphemeralStorage != ""

		if hasResources {
			// Use the resolved resource values for both requests and limits
			resources["requests"] = map[string]any{
				"cpu":              component.Resources.CPU,
				"memory":           component.Resources.Memory,
				"ephemeralStorage": component.Resources.EphemeralStorage,
			}
			resources["limits"] = map[string]any{
				"cpu":              component.Resources.CPU,
				"memory":           component.Resources.Memory,
				"ephemeralStorage": component.Resources.EphemeralStorage,
			}
		}

		ingress := map[string]any{}
		if component.Ingress.Host != "" {
			ingress = map[string]any{
				"enabled": true,
				"host":    component.Ingress.Host,
				"tls":     component.Ingress.TLS,
			}
		}

		autoscaling := map[string]any{}
		if component.Autoscaling.Enabled {
			autoscaling = map[string]any{
				"enabled":     true,
				"minReplicas": component.Autoscaling.MinReplicas,
				"maxReplicas": component.Autoscaling.MaxReplicas,
				"metrics":     component.Autoscaling.Metrics,
			}
		}

		values[name] = map[string]any{
			"image": map[string]any{
				"repository": image,
				"tag":        tag,
			},
			"command": component.Command,
			"args":    component.Args,
			// TODO: Add support for specifying the protocol (such as TCP or UDP) for each port in the Deployah specification, for example: "8080/TCP"
			// By default, the protocol is TCP.
			"ports": []map[string]any{
				{
					"name":          "http",
					"containerPort": component.Port,
					"protocol":      "TCP",
				},
			},
			"resources":   resources,
			"ingress":     ingress,
			"autoscaling": autoscaling,
		}
	}

	return values, nil
}
