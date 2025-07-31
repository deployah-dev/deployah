package helm

import (
	"embed"
	"strings"

	"github.com/deployah-dev/deployah/internal/manifest"
)

//go:embed chart/templates/chart/**
var ChartTemplateFS embed.FS

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

		// TODO: Implement support for image digests in addition to tags
		if component.Image != "" {
			split := strings.Split(component.Image, ":")
			if len(split) == 2 {
				image = split[0]
				tag = split[1]
			}
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
