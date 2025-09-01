package helm

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"k8s.io/utils/ptr"

	sprig "github.com/Masterminds/sprig/v3"
	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/distribution/reference"
)

// Embed the embedded chart directory. We renamed underscore-prefixed templates
// so they are included by directory embedding without explicit listing.
//
//go:embed chart
var ChartTemplateFS embed.FS

// parseContainerImage parses a container image reference and returns the image repository and tag/digest.
// It handles various formats including:
// - simple: nginx:1.21
// - with registry port: registry.example.com:5000/nginx:1.21
// - with digest: nginx@sha256:abc123...
// - with registry port and digest: registry.example.com:5000/nginx@sha256:abc123...
// - with registry port and tag: registry.example.com:5000/nginx:1.21
func parseContainerImage(imageRef string) (repository, tagOrDigest string) {
	if imageRef == "" {
		return "", ""
	}

	parsed, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		// Fallback to raw string if parsing fails
		return imageRef, ""
	}

	// Base name without tag/digest (contains registry and path)
	repo := parsed.Name()

	// Prefer digest if present
	if digested, ok := parsed.(reference.Digested); ok {
		return repo, digested.Digest().String()
	}
	if tagged, ok := parsed.(reference.Tagged); ok {
		return repo, tagged.Tag()
	}
	return repo, ""
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
	Values   map[string]any     // For values.yaml templating
	Manifest *manifest.Manifest // Added for dynamic sub-charts
}

// GenerateReleaseName returns the Helm release name derived from project and environment.
// Format: PROJECT_NAME-ENVIRONMENT_NAME
func GenerateReleaseName(projectName, environmentName string) string {
	return projectName + "-" + environmentName
}

// PrepareChart expands the embedded chart into a temporary directory, rendering any
// files with the .gotmpl suffix using Go templates and Sprig functions. It returns the root
// directory path of the prepared chart. Uses caching to avoid regenerating identical charts.
func PrepareChart(ctx context.Context, manifest *manifest.Manifest, desiredEnvironment string) (string, error) {
	// Generate comprehensive cache key based on both manifest and embedded chart templates
	cacheKey, err := GenerateCacheKey(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to generate cache key: %w", err)
	}

	// Try to get cached chart first
	if cachedPath, found := GetCachedChart(cacheKey); found {
		// Return a copy of the cached chart to avoid conflicts with cleanup
		return CreateChartCopy(cachedPath)
	}

	// Cleanup expired cache entries periodically (every 10th call)
	// This is a simple approach to avoid goroutine overhead
	count, _ := GetChartCacheStats()
	if count > 0 && count%10 == 0 {
		go CleanupExpiredCharts()
	}

	const root = "chart"

	tmpDir, err := os.MkdirTemp("", "deployah-chart-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	var chartData ChartData
	chartData.Chart.Name = manifest.Project
	chartData.Chart.Version = "0.1.0"
	chartData.Values = map[string]any{}
	chartData.Manifest = manifest

	err = fs.WalkDir(ChartTemplateFS, root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("failed to walk directory %s: %w", path, walkErr)
		}

		// Skip the root directory itself to avoid creating an extra
		// "chart" directory in the output
		if path == root {
			return nil
		}
		// Compute the path relative to the embedded root
		rel := strings.TrimPrefix(path, root)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		dstPath := filepath.Join(tmpDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		data, readErr := ChartTemplateFS.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("failed to read file %s: %w", path, readErr)
		}

		if strings.HasSuffix(path, ".gotmpl") {
			tpl, tplErr := template.New(filepath.Base(path)).Funcs(sprig.TxtFuncMap()).Parse(string(data))
			if tplErr != nil {
				return fmt.Errorf("failed to parse template %s: %w", path, tplErr)
			}
			var buf bytes.Buffer
			if execErr := tpl.Execute(&buf, chartData); execErr != nil {
				return fmt.Errorf("failed to render template %s: %w", path, execErr)
			}
			dstPath = strings.TrimSuffix(dstPath, ".gotmpl")
			if writeErr := os.WriteFile(dstPath, buf.Bytes(), 0o644); writeErr != nil {
				return fmt.Errorf("failed to write rendered template %s: %w", dstPath, writeErr)
			}
			return nil
		}

		// If the file is inside the templates directory, prepend "_" to the file name
		// for files with .yaml, .tpl, or .txt extensions (excluding .gotmpl or any other templates)
		// This is needed because go:embed does not include files starting with "_", but Helm expects them.
		// We want to ensure that when rendering the chart, files like "_helpers.tpl" or "_NOTES.txt" are present.
		// This logic only applies to files directly under a "templates" directory.
		if strings.Contains(path, "templates/") {
			ext := filepath.Ext(d.Name())
			base := strings.TrimSuffix(d.Name(), ext)
			if slices.Contains([]string{".yaml", ".tpl", ".txt"}, ext) {
				dstDir := filepath.Dir(dstPath)
				dstPath = filepath.Join(dstDir, "_"+base+ext)
			}
		}

		if writeErr := os.WriteFile(dstPath, data, 0o644); writeErr != nil {
			return fmt.Errorf("failed to write file %s: %w", dstPath, writeErr)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to expand embedded chart: %w", err)
	}

	// Create dynamic sub-charts for each component
	if err := createComponentSubCharts(tmpDir, manifest); err != nil {
		return "", fmt.Errorf("failed to create component sub-charts: %w", err)
	}

	// Create a values.yaml file that includes the values for each component
	values, err := MapManifestToChartValues(manifest, desiredEnvironment)
	if err != nil {
		return "", fmt.Errorf("failed to map manifest to chart values: %w", err)
	}
	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("failed to marshal values to YAML: %w", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "values.yaml"), valuesYAML, 0o644); err != nil {
		return "", fmt.Errorf("failed to write values.yaml: %w", err)
	}

	// Cache the generated chart for future use
	SetCachedChart(cacheKey, tmpDir)

	return tmpDir, nil
}

// createComponentSubCharts creates sub-chart directories for each component
func createComponentSubCharts(chartDir string, manifest *manifest.Manifest) error {
	chartsDir := filepath.Join(chartDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create charts directory: %w", err)
	}

	for componentName := range manifest.Components {
		componentChartDir := filepath.Join(chartsDir, componentName)
		if err := os.MkdirAll(componentChartDir, 0o755); err != nil {
			return fmt.Errorf("failed to create component chart directory for %s: %w", componentName, err)
		}

		// Create Chart.yaml for the component
		if err := createComponentChartYAML(componentChartDir, componentName); err != nil {
			return fmt.Errorf("failed to create Chart.yaml for component %s: %w", componentName, err)
		}

		// Create templates directory and app.yaml
		templatesDir := filepath.Join(componentChartDir, "templates")
		if err := os.MkdirAll(templatesDir, 0o755); err != nil {
			return fmt.Errorf("failed to create templates directory for component %s: %w", componentName, err)
		}

		if err := createComponentAppTemplate(templatesDir); err != nil {
			return fmt.Errorf("failed to create app.yaml template for component %s: %w", componentName, err)
		}
	}

	return nil
}

// createComponentChartYAML creates a Chart.yaml file for a component
func createComponentChartYAML(chartDir, componentName string) error {
	chartYAML := fmt.Sprintf(`apiVersion: v2
name: %s
type: application
version: 0.1.0
`, componentName)

	return os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644)
}

// createComponentAppTemplate creates the app.yaml template file
func createComponentAppTemplate(templatesDir string) error {
	appTemplate := `{{- include "deployah.app" . -}}`

	return os.WriteFile(filepath.Join(templatesDir, "app.yaml"), []byte(appTemplate), 0o644)
}

func MapManifestToChartValues(m *manifest.Manifest, desiredEnvironment string) (map[string]any, error) {
	values := make(map[string]any)

	for componentName, component := range m.Components {
		// Skip component if it is not deployed in the desired environment
		if !slices.Contains(component.Environments, desiredEnvironment) {
			continue
		}

		componentValues := map[string]any{
			"commonLabels": map[string]string{
				"deployah.dev/project":     m.Project,
				"deployah.dev/component":   componentName,
				"deployah.dev/environment": desiredEnvironment,
			},
		}

		if component.Role != manifest.ComponentRoleService {
			// TODO: Add support for component roles such as "worker", and "job"
			return nil, fmt.Errorf("role %s is not supported yet", component.Role)
		}
		// TODO: Implement handling for component envFile
		//   Exclude Deployah-specific environment variables (those prefixed with DPY_VAR_) and provide the remaining variables to the component
		
		// TODO: Implement handling for component configFile
		//   Use a deep merge to merge the component's configFile with the environment's configFile
		//   Precedence:
		//   - config.yaml (base)
		//   - config.production.yaml (environment-specific)
		//   - config.api.yaml (component-specific)
		//   - config.api.production.yaml (environment and component-specific)
		
		if component.Kind == manifest.ComponentKindStateful {
			// TODO: Add support for stateful components
			return nil, fmt.Errorf("stateful components are not supported yet")
		}

		if component.Autoscaling != nil && component.Autoscaling.Enabled {
			componentValues["autoscaling"] = map[string]any{
				"enabled":     true,
				"minReplicas": component.Autoscaling.MinReplicas,
				"maxReplicas": component.Autoscaling.MaxReplicas,
				"metrics":     component.Autoscaling.Metrics,
			}
		}
		// TODO: Add support for component env, The user can specify the environment variables for the component e.g. NODE_ENV=roduction

		image := ""
		tag := ""

		// Parse container image reference (supports registry ports and digest references)
		if component.Image != "" {
			image, tag = parseContainerImage(component.Image)
		}

		resources := map[string]any{}

		// Map manifest-level resolved resources (defaults/presets applied already by manifest package)
		if (component.Resources.CPU != nil && *component.Resources.CPU != "") ||
			(component.Resources.Memory != nil && *component.Resources.Memory != "") ||
			(component.Resources.EphemeralStorage != nil && *component.Resources.EphemeralStorage != "") {
			resources["requests"] = map[string]any{
				"cpu":               ptr.Deref(component.Resources.CPU, ""),
				"memory":            ptr.Deref(component.Resources.Memory, ""),
				"ephemeral-storage": ptr.Deref(component.Resources.EphemeralStorage, ""),
			}
		}

		componentValues["resources"] = resources

		if component.Ingress != nil && component.Ingress.Host != "" {
			componentValues["ingress"] = map[string]any{
				"enabled":  true,
				"hostname": component.Ingress.Host,
				"tls":      component.Ingress.TLS,
			}
		}

		if component.Autoscaling != nil && component.Autoscaling.Enabled {
			componentValues["autoscaling"] = map[string]any{
				"enabled":     true,
				"minReplicas": component.Autoscaling.MinReplicas,
				"maxReplicas": component.Autoscaling.MaxReplicas,
				"metrics":     component.Autoscaling.Metrics,
			}
		}

		imageValues := map[string]any{
			"repository": image,
		}
		if strings.HasPrefix(tag, "sha256:") {
			imageValues["digest"] = tag
		} else if tag != "" {
			imageValues["tag"] = tag
		}

		componentValues["image"] = imageValues

		// 
		if len(component.Command) > 0 {
			componentValues["command"] = component.Command
		}
		if len(component.Args) > 0 {
			componentValues["args"] = component.Args
		}

		// Add ports only for service role components with a valid port
		// Worker and job roles do not expose ports (there is no use case for ports for worker or job components)
		if component.Port > 0 && component.Role == manifest.ComponentRoleService {
			componentValues["ports"] = []map[string]any{
				{
					"name":          "http",
					"containerPort": component.Port,
					// TODO: Add support for protocol
					"protocol": "TCP",
				},
			}
		}

		values[componentName] = componentValues
	}

	return values, nil
}
