package helm

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/distribution/reference"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/intstr"

	"deployah.dev/deployah/internal/spec"

	sprig "github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
)

// ChartTemplateFS embeds the chart directory. Underscore-prefixed templates
// were renamed so directory embedding includes them without explicit listing.
//
//go:embed chart
var ChartTemplateFS embed.FS

// parseContainerImage parses a container image reference and returns the
// repository and tag or digest.
func parseContainerImage(imageRef string) (repository, tagOrDigest string) {
	if imageRef == "" {
		return "", ""
	}

	parsed, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
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

// ChartData holds values substituted in Helm chart templates.
type ChartData struct {
	// Chart holds metadata rendered into Chart.yaml.
	Chart struct {
		Name        string
		Description string
		Version     string
		AppVersion  string
	}
	// Values is the data map for values.yaml templating.
	Values map[string]any
	// Spec is the source Deployah spec for dynamic sub-charts.
	Spec *spec.Spec
}

// GenerateReleaseName returns the Helm release name for project and
// environment. Format: PROJECT_NAME-ENVIRONMENT_NAME.
func GenerateReleaseName(projectName, environmentName string) string {
	// K8sSafe: wildcard names like "review/pr-42" contain "/", which is
	// illegal in release names and label values.
	return projectName + "-" + spec.NormalizeEnv(environmentName).K8sSafe
}

// PrepareChart expands the embedded chart into a temporary directory,
// rendering .gotmpl files with Go templates and Sprig functions, and returns
// the prepared chart root directory (cached across calls for identical charts).
func PrepareChart(ctx context.Context, manifest *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) (string, error) {
	// Generate comprehensive cache key based on resolved spec (or raw spec if
	// no platform resolution was performed) and embedded chart templates.
	cacheKey, err := GenerateCacheKey(manifest, resolved)
	if err != nil {
		return "", fmt.Errorf("failed to generate cache key: %w", err)
	}

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
	chartData.Spec = manifest

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
			return os.MkdirAll(dstPath, 0o750)
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
			if writeErr := os.WriteFile(dstPath, buf.Bytes(), 0o600); writeErr != nil {
				return fmt.Errorf("failed to write rendered template %s: %w", dstPath, writeErr)
			}
			return nil
		}

		// Prepend "_" so Helm sees files go:embed excludes (those starting with "_").
		if strings.Contains(path, "templates/") {
			ext := filepath.Ext(d.Name())
			base := strings.TrimSuffix(d.Name(), ext)
			if slices.Contains([]string{".yaml", ".tpl", ".txt"}, ext) {
				dstDir := filepath.Dir(dstPath)
				dstPath = filepath.Join(dstDir, "_"+base+ext)
			}
		}

		if writeErr := os.WriteFile(dstPath, data, 0o600); writeErr != nil {
			return fmt.Errorf("failed to write file %s: %w", dstPath, writeErr)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to expand embedded chart: %w", err)
	}

	if err = createComponentSubCharts(tmpDir, manifest); err != nil {
		return "", fmt.Errorf("failed to create component sub-charts: %w", err)
	}

	values, err := MapSpecToChartValues(manifest, desiredEnvironment, resolved)
	if err != nil {
		return "", fmt.Errorf("failed to map spec to chart values: %w", err)
	}
	valuesYAML, err := yaml.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("failed to marshal values to YAML: %w", err)
	}

	if writeErr := os.WriteFile(filepath.Join(tmpDir, "values.yaml"), valuesYAML, 0o600); writeErr != nil {
		return "", fmt.Errorf("failed to write values.yaml: %w", writeErr)
	}

	// tmpDir must never be handed out directly; return a copy so the cache
	// entry survives caller cleanup.
	SetCachedChart(cacheKey, tmpDir)

	return CreateChartCopy(tmpDir)
}

// createComponentSubCharts creates sub-chart directories for each component
func createComponentSubCharts(chartDir string, manifest *spec.Spec) error {
	chartsDir := filepath.Join(chartDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o750); err != nil {
		return fmt.Errorf("failed to create charts directory: %w", err)
	}

	for componentName := range manifest.Components {
		componentChartDir := filepath.Join(chartsDir, componentName)
		if err := os.MkdirAll(componentChartDir, 0o750); err != nil {
			return fmt.Errorf("failed to create component chart directory for %s: %w", componentName, err)
		}

		if err := createComponentChartYAML(componentChartDir, componentName); err != nil {
			return fmt.Errorf("failed to create Chart.yaml for component %s: %w", componentName, err)
		}

		templatesDir := filepath.Join(componentChartDir, "templates")
		if err := os.MkdirAll(templatesDir, 0o750); err != nil {
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

	return os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o600)
}

// createComponentAppTemplate creates the app.yaml template file
func createComponentAppTemplate(templatesDir string) error {
	appTemplate := `{{- include "deployah.app" . -}}`

	return os.WriteFile(filepath.Join(templatesDir, "app.yaml"), []byte(appTemplate), 0o600)
}

// resolvedSchemaVersion is the current version of the deployah.resolved values
// block. Consumers must check this to handle future shape changes.
const resolvedSchemaVersion = "1"

// MapSpecToChartValues converts a spec into Helm chart values for the given
// environment (resolved, if non-nil, supplies FQDN/TLS) and writes a
// deployah.resolved block so the hostname guard can compare across deploys.
func MapSpecToChartValues(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) (map[string]any, error) {
	values := make(map[string]any)
	// Track resolved per-component data for the deployah.resolved block.
	resolvedComponents := make(map[string]any)

	for componentName, component := range m.Components {
		// Skip component if it is not deployed in the desired environment
		// (empty filter = active everywhere). Same matcher as spec.Resolve,
		// so wildcard deploys agree on the active component set.
		if len(component.Environments) > 0 {
			if _, ok := spec.MatchEnvKey(desiredEnvironment, component.Environments); !ok {
				continue
			}
		}

		componentValues := map[string]any{
			"commonLabels": map[string]string{
				"deployah.dev/project":     m.Project,
				"deployah.dev/component":   componentName,
				"deployah.dev/environment": spec.NormalizeEnv(desiredEnvironment).K8sSafe,
			},
		}

		if !component.Role.IsService() {
			// TODO: Add support for component roles such as "worker", and "job"
			return nil, fmt.Errorf("role %s is not supported yet", component.Role)
		}
		// TODO: Implement handling for component envFile
		//   Exclude Deployah-specific environment variables (those prefixed with DPY_VAR_) and provide the remaining variables to the component

		// TODO: Implement component configFile -- deep-merge config.yaml <
		// config.<env>.yaml < config.<component>.yaml < config.<component>.<env>.yaml.

		if component.Kind == spec.ComponentKindStateful {
			// TODO: Add support for stateful components
			return nil, fmt.Errorf("stateful components are not supported yet")
		}

		// TODO: Add support for component env, The user can specify the environment variables for the component e.g. NODE_ENV=roduction

		image := ""
		tag := ""

		if component.Image != "" {
			image, tag = parseContainerImage(component.Image)
		}

		resources := map[string]any{}

		// Map spec-level resolved resources (defaults/presets applied already by spec package).
		// Only set the requests keys the spec actually provides: a field left
		// unset is genuinely absent, not an empty-string request.
		requests := map[string]any{}
		if component.Resources.CPU != nil && !component.Resources.CPU.IsZero() {
			requests["cpu"] = component.Resources.CPU.String()
		}
		if component.Resources.Memory != nil && !component.Resources.Memory.IsZero() {
			requests["memory"] = component.Resources.Memory.String()
		}
		if component.Resources.EphemeralStorage != nil && !component.Resources.EphemeralStorage.IsZero() {
			requests["ephemeral-storage"] = component.Resources.EphemeralStorage.String()
		}
		if len(requests) > 0 {
			resources["requests"] = requests
		}

		componentValues["resources"] = resources

		if component.Expose != nil {
			ingressVals := map[string]any{"enabled": true}
			if resolved != nil {
				if rc, ok := resolved.Components[componentName]; ok && rc.FQDN != "" {
					ingressVals["hostname"] = rc.FQDN
					switch rc.TLSMode {
					case spec.TLSModeSelfSigned:
						ingressVals["tls"] = true
						if len(rc.TLSCertPEM) == 0 || len(rc.TLSKeyPEM) == 0 {
							return nil, fmt.Errorf("component %s: self-signed TLS certificate not materialized before render", componentName)
						}
						ingressVals["secrets"] = []map[string]any{
							{
								"name":        rc.FQDN + "-tls",
								"certificate": string(rc.TLSCertPEM),
								"key":         string(rc.TLSKeyPEM),
							},
						}
					case spec.TLSModeSecretName:
						ingressVals["tls"] = true
						ingressVals["existingSecret"] = rc.TLSSecretName
					case spec.TLSModeCertManager:
						ingressVals["tls"] = true
						ingressVals["annotations"] = map[string]string{
							"cert-manager.io/cluster-issuer": rc.TLSIssuer,
						}
					default:
						ingressVals["tls"] = false
					}
					resolvedComponents[componentName] = map[string]any{
						"fqdn":    rc.FQDN,
						"tlsMode": string(rc.TLSMode),
					}
				}
			}
			componentValues["ingress"] = ingressVals
		}

		if component.Autoscaling != nil && component.Autoscaling.Enabled {
			componentValues["autoscaling"] = buildAutoscalingValues(component.Autoscaling)
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

		// Worker and job roles do not expose ports.
		if component.ListensOnPort() {
			componentValues["ports"] = []map[string]any{
				{
					"name":          "http",
					"containerPort": component.Port,
					// TODO: Add support for protocol
					"protocol": "TCP",
				},
			}
		}

		if component.Role.IsService() {
			probes, probeErr := buildProbeValues(component)
			if probeErr != nil {
				return nil, fmt.Errorf("component %s: probes: %w", componentName, probeErr)
			}
			maps.Copy(componentValues, probes)
		}

		if resolved != nil {
			if rc, ok := resolved.Components[componentName]; ok && rc.MergedProfile != nil {
				if err := applyMergedProfile(componentValues, rc.MergedProfile); err != nil {
					return nil, fmt.Errorf("component %s: apply profile values: %w", componentName, err)
				}
				entry, hasEntry := resolvedComponents[componentName].(map[string]any)
				if !hasEntry {
					entry = map[string]any{}
					resolvedComponents[componentName] = entry
				}
				if len(rc.Profiles) > 0 {
					entry["profiles"] = rc.Profiles
				}
				if rc.StorageClass != "" {
					entry["storageClass"] = rc.StorageClass
				}
			}
		}

		values[componentName] = componentValues
	}

	// Write the deployah.resolved block so the hostname guard can compare
	// values across deploys.
	if len(resolvedComponents) > 0 {
		values["deployah"] = map[string]any{
			"resolved": map[string]any{
				"schemaVersion": resolvedSchemaVersion,
				"components":    resolvedComponents,
			},
		}
	}

	return values, nil
}

// buildProbeValues builds startup/readiness/liveness probe values from the
// component's health config.
func buildProbeValues(component spec.Component) (map[string]any, error) {
	h := component.Health

	readyDisabled := h != nil && h.Ready != nil && h.Ready.Disabled
	aliveDisabled := h != nil && h.Alive != nil && h.Alive.Disabled

	// Nothing to emit when both sides are explicitly off.
	if readyDisabled && aliveDisabled {
		return map[string]any{}, nil
	}

	result := make(map[string]any)

	// Determine the HTTP path for the ready and alive checks.
	var readyPath string
	if h != nil && h.Ready != nil && !h.Ready.Disabled {
		readyPath = h.Ready.Path
	}
	var alivePath string
	if h != nil && h.Alive != nil && !h.Alive.Disabled {
		alivePath = h.Alive.Path
	}

	// Startup probe is active whenever at least one side is not disabled. It
	// inherits the ready path if set (so it validates the same HTTP endpoint),
	// otherwise falls back to TCP.
	startup, err := buildStartupProbe(readyPath)
	if err != nil {
		return nil, fmt.Errorf("startupProbe: %w", err)
	}
	result["startupProbe"] = startup

	// Readiness probe.
	if !readyDisabled {
		readiness, readinessErr := buildReadinessProbe(readyPath)
		if readinessErr != nil {
			return nil, fmt.Errorf("readinessProbe: %w", readinessErr)
		}
		result["readinessProbe"] = readiness
	}

	// Liveness probe.
	if !aliveDisabled {
		var interval, restartAfter string
		if h != nil && h.Alive != nil {
			interval = h.Alive.Interval
			restartAfter = h.Alive.RestartAfter
		}
		liveness, livenessErr := buildLivenessProbe(alivePath, interval, restartAfter)
		if livenessErr != nil {
			return nil, fmt.Errorf("livenessProbe: %w", livenessErr)
		}
		result["livenessProbe"] = liveness
	}

	return result, nil
}

// buildStartupProbe constructs the startup probe map for the Helm values.
// When path is non-empty the probe uses HTTP; otherwise it uses TCP.
func buildStartupProbe(path string) (map[string]any, error) {
	return probeValues(corev1.Probe{
		ProbeHandler:     probeHandler(path),
		PeriodSeconds:    int32(spec.DefaultStartupProbePeriod),
		FailureThreshold: int32(spec.DefaultStartupProbeFailureThreshold),
		TimeoutSeconds:   int32(spec.DefaultStartupProbeTimeout),
	})
}

// buildReadinessProbe constructs the readiness probe map for the Helm values.
// When path is non-empty the probe uses HTTP; otherwise it uses TCP.
func buildReadinessProbe(path string) (map[string]any, error) {
	return probeValues(corev1.Probe{
		ProbeHandler:     probeHandler(path),
		PeriodSeconds:    int32(spec.DefaultReadinessProbePeriod),
		FailureThreshold: int32(spec.DefaultReadinessProbeFailureThreshold),
		TimeoutSeconds:   int32(spec.DefaultReadinessProbeTimeout),
	})
}

// buildLivenessProbe constructs the liveness probe map for the Helm values.
// When path is non-empty the probe uses HTTP; otherwise it uses TCP.
// interval and restartAfter are duration strings; each defaults when empty.
// failureThreshold = ceil(restartAfterSec / intervalSec).
func buildLivenessProbe(path, interval, restartAfter string) (map[string]any, error) {
	if interval == "" {
		interval = spec.DefaultLivenessInterval
	}
	if restartAfter == "" {
		restartAfter = spec.DefaultLivenessRestartAfter
	}

	// Errors here are already caught by ValidateComponentHealth; use the
	// numeric constants as fallback so the caller always gets a valid map.
	intervalSec, err := spec.ParseDuration(interval)
	if err != nil || intervalSec <= 0 {
		intervalSec = spec.DefaultLivenessProbePeriod
	}
	restartSec, err := spec.ParseDuration(restartAfter)
	if err != nil || restartSec <= 0 {
		restartSec = spec.DefaultLivenessRestartAfterSec
	}

	// Round up so the real restart window is never shorter than requested.
	failureThreshold := min(max(int(math.Ceil(float64(restartSec)/float64(intervalSec))), 1), math.MaxInt32)

	return probeValues(corev1.Probe{
		ProbeHandler:  probeHandler(path),
		PeriodSeconds: int32(intervalSec),
		// failureThreshold is clamped to math.MaxInt32 above.
		FailureThreshold: int32(failureThreshold), //nolint:gosec
		TimeoutSeconds:   int32(spec.DefaultLivenessProbeTimeout),
	})
}

func probeHandler(path string) corev1.ProbeHandler {
	port := intstr.FromString("http")
	if path != "" {
		return corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: path, Port: port},
		}
	}
	return corev1.ProbeHandler{
		TCPSocket: &corev1.TCPSocketAction{Port: port},
	}
}

// probeValues converts a corev1.Probe into Helm values and sets enabled.
func probeValues(probe corev1.Probe) (map[string]any, error) {
	m, err := toValuesMap(probe)
	if err != nil {
		return nil, err
	}
	m["enabled"] = true
	return m, nil
}

// buildAutoscalingValues translates the spec Autoscaling configuration into
// the Helm values map consumed by the embedded hpa.yaml template. Known
// metric types (cpu, memory) become targetCPU/targetMemory; duplicates let
// the last one win.
func buildAutoscalingValues(a *spec.Autoscaling) map[string]any {
	v := map[string]any{
		"enabled":     true,
		"minReplicas": a.MinReplicas,
		"maxReplicas": a.MaxReplicas,
	}
	for _, m := range a.Metrics {
		switch m.Type {
		case spec.MetricTypeCPU:
			v["targetCPU"] = m.Target
		case spec.MetricTypeMemory:
			v["targetMemory"] = m.Target
		}
	}
	return v
}

// applyMergedProfile writes resolved profile fields into component Helm values.
func applyMergedProfile(componentValues map[string]any, profile *spec.PlatformProfile) error {
	if profile == nil {
		return nil
	}
	if len(profile.NodeSelector) > 0 {
		componentValues["nodeSelector"] = maps.Clone(profile.NodeSelector)
	}
	if len(profile.Tolerations) > 0 {
		vals, err := toValuesSlice(profile.Tolerations)
		if err != nil {
			return fmt.Errorf("tolerations: %w", err)
		}
		componentValues["tolerations"] = vals
	}
	if len(profile.PodLabels) > 0 {
		labels, ok := componentValues["commonLabels"].(map[string]string)
		if !ok {
			labels = map[string]string{}
		} else {
			labels = maps.Clone(labels)
		}
		maps.Copy(labels, profile.PodLabels)
		componentValues["commonLabels"] = labels
		componentValues["podLabels"] = maps.Clone(profile.PodLabels)
	}
	if len(profile.PodAnnotations) > 0 {
		componentValues["podAnnotations"] = maps.Clone(profile.PodAnnotations)
	}
	if profile.SecurityContext != nil {
		psc, err := toValuesMap(profile.SecurityContext)
		if err != nil {
			return fmt.Errorf("securityContext: %w", err)
		}
		psc["enabled"] = true
		componentValues["podSecurityContext"] = psc
	}
	if profile.ContainerSecurityContext != nil {
		csc, err := toValuesMap(profile.ContainerSecurityContext)
		if err != nil {
			return fmt.Errorf("containerSecurityContext: %w", err)
		}
		csc["enabled"] = true
		componentValues["containerSecurityContext"] = csc
	}
	return nil
}

// toValuesMap JSON-roundtrips v into a Helm-friendly map[string]any.
func toValuesMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if unmarshalErr := json.Unmarshal(data, &out); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	if out == nil {
		out = map[string]any{}
	}
	normalizeJSONNumbers(out)
	return out, nil
}

// toValuesSlice JSON-roundtrips v into a Helm-friendly []any.
func toValuesSlice(v any) ([]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out []any
	if unmarshalErr := json.Unmarshal(data, &out); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	normalizeJSONNumbers(out)
	return out, nil
}

// normalizeJSONNumbers converts JSON float64 whole numbers to int so Helm
// values match the previous hand-built maps (periodSeconds: 5, not 5.0).
func normalizeJSONNumbers(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch n := child.(type) {
			case float64:
				if n == float64(int(n)) {
					t[k] = int(n)
				}
			default:
				normalizeJSONNumbers(child)
			}
		}
	case []any:
		for i, child := range t {
			switch n := child.(type) {
			case float64:
				if n == float64(int(n)) {
					t[i] = int(n)
				}
			default:
				normalizeJSONNumbers(child)
			}
		}
	}
}
