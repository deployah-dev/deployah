package initialize

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/renameio/v2"
	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/localkube"
	"deployah.dev/deployah/internal/spec"
)

// showSummaryAndSave previews the spec and writes files unless the user
// declines Save. saved is false when Save is declined so the caller can
// skip the "initialization completed" line.
func showSummaryAndSave(c *nabat.Context, config *ProjectConfig) (saved bool, err error) {
	specData, err := buildValidatedSpec(config)
	if err != nil {
		return false, fmt.Errorf("configuration validation failed: %w", err)
	}

	specYAML, err := marshalSpecYAML(specData)
	if err != nil {
		return false, fmt.Errorf("failed to marshal spec for preview: %w", err)
	}

	if err = c.Highlight(string(specYAML), "yaml"); err != nil {
		return false, fmt.Errorf("failed to render spec preview: %w", err)
	}

	if config.DryRun {
		c.Info("dry-run mode; no files written")
		c.Println("Run again without --dry-run to save the configuration.")
		return true, nil
	}

	// WithPrefill(true) seeds the TTY on Yes. --force does not skip Save.
	save, err := c.Confirm(
		fmt.Sprintf("Save to %s?", config.SpecPath),
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
		nabat.WithPrefill(true),
	)
	if err != nil {
		return false, fmt.Errorf("failed to confirm save: %w", err)
	}
	if !save {
		c.Info("aborted; no files written")
		return false, nil
	}

	if err = writeSpecFile(specData, config.SpecPath); err != nil {
		return false, fmt.Errorf("failed to save spec to %s: %w", config.SpecPath, err)
	}
	c.Success("Created " + config.SpecPath + " (" + spec.CurrentManifestVersion + ")")

	platformFile := platformPath(config)
	envNames := slices.Sorted(slices.Values(config.EnvironmentNames))

	created, added, platformErr := spec.EnsurePlatformEnvironments(platformFile, localkube.DefaultIngressIP, envNames)
	if platformErr != nil {
		return false, fmt.Errorf("saved spec but failed to update platform file: %w", platformErr)
	}
	switch {
	case created:
		c.Success(fmt.Sprintf("Created %s (%s) with %s",
			platformFile, spec.CurrentPlatformVersion, joinWithAnd(envNames)))
	case len(added) > 0:
		c.Success(fmt.Sprintf("Added missing environments to %s: %s",
			platformFile, joinWithAnd(added)))
	default:
		c.Info(platformFile + " already has the selected environments; no changes made.")
	}

	var platform *spec.PlatformConfig
	if _, statErr := os.Stat(platformFile); statErr == nil {
		loaded, loadErr := spec.LoadPlatform(platformFile)
		if loadErr != nil {
			c.Warn(fmt.Sprintf("failed to read %s: %v", platformFile, loadErr))
		} else {
			platform = loaded
			printPlatformSummary(c, platformFile, envNames, platform)
		}
	}
	warnExposeWithoutDomains(c, config, platform)

	createdExtras, scaffoldErr := scaffoldExtrasDirs(filepath.Dir(config.SpecPath))
	switch {
	case scaffoldErr != nil:
		c.Warn(fmt.Sprintf("failed to create .deployah extras directories: %v", scaffoldErr))
	case createdExtras:
		c.Success("Created .deployah/manifests/ and .deployah/crds/")
	default:
		c.Info(".deployah/manifests/ and .deployah/crds/ already exist; no changes made.")
	}

	printNextSteps(c, config)
	return true, nil
}

func platformPath(config *ProjectConfig) string {
	if config.PlatformPath != "" {
		return config.PlatformPath
	}
	return filepath.Join(filepath.Dir(config.SpecPath), spec.DefaultPlatformPath)
}

func printPlatformSummary(c *nabat.Context, path string, envNames []string, platform *spec.PlatformConfig) {
	c.Println("Platform: " + path)
	c.Println("Environments: " + strings.Join(envNames, ", "))
	if platform == nil {
		return
	}
	local, ok := platform.Environments[DefaultEnvironmentName]
	if !ok {
		return
	}
	domain := localDomain(local)
	if local.Context == "" && domain == "" {
		return
	}
	parts := make([]string, 0, 2)
	if local.Context != "" {
		parts = append(parts, "context "+local.Context)
	}
	if domain != "" {
		parts = append(parts, "domain "+domain)
	}
	c.Println(DefaultEnvironmentName + ": " + strings.Join(parts, ", "))
}

func localDomain(env spec.PlatformEnvironment) string {
	for _, domain := range env.Domains {
		if domain.BaseDomain != "" {
			return domain.BaseDomain
		}
	}
	return ""
}

func warnExposeWithoutDomains(c *nabat.Context, config *ProjectConfig, platform *spec.PlatformConfig) {
	hasExpose := false
	for _, component := range config.Components {
		if component.Expose != nil {
			hasExpose = true
			break
		}
	}
	if !hasExpose {
		return
	}
	var missing []string
	for _, name := range config.EnvironmentNames {
		if name == DefaultEnvironmentName {
			continue
		}
		if platform == nil {
			missing = append(missing, name)
			continue
		}
		pe, ok := platform.Environments[name]
		if !ok || len(pe.Domains) == 0 {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	c.Warn(fmt.Sprintf(
		"%s %s no domains yet; exposed components will not get a public URL there until you add domains in the platform file.",
		joinWithAnd(missing), verbHave(len(missing)),
	))
}

func verbHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

func printNextSteps(c *nabat.Context, config *ProjectConfig) {
	c.Println("Next steps:")
	if slices.Contains(config.EnvironmentNames, DefaultEnvironmentName) {
		c.Println("  1. deployah cluster up")
		c.Println("  2. deployah deploy local")
		return
	}
	env := ""
	if len(config.EnvironmentNames) > 0 {
		env = config.EnvironmentNames[0]
	}
	c.Println("  1. Fill in context and domains in " + platformPath(config))
	if env != "" {
		c.Println("  2. deployah deploy " + env)
	}
}

const (
	manifestsREADME = `# Extra manifests

Put raw Kubernetes YAML here. Deployah loads it into the same Helm release
as your generated resources.

- Common files (all environments): place *.yaml / *.yml in this directory
- Per environment: use a subdirectory named after a declared environment
  key (for example manifests/prod/extra.yaml)
- Files are applied literally: no Helm templating and no env substitution
- CustomResourceDefinition belongs in ../crds/, not here
`
	crdsREADME = `# Extra CRDs

Put CustomResourceDefinition YAML here. Deployah applies these to the
cluster before the Helm release, then waits for each CRD to become
Established.

- Shared across all environments (no per-env subdirectories)
- Install policy: deployah deploy --crds create|create-replace
- CRDs are never deleted on uninstall
`
)

// scaffoldExtrasDirs creates .deployah/manifests and .deployah/crds with
// short README files. Returns whether anything new was written.
func scaffoldExtrasDirs(specDir string) (created bool, err error) {
	root := filepath.Join(specDir, spec.DeployahConfigDir)
	entries := []struct {
		dir  string
		file string
		body string
	}{
		{filepath.Join(root, spec.ManifestsDir), "README.md", manifestsREADME},
		{filepath.Join(root, spec.CRDsDir), "README.md", crdsREADME},
	}
	for _, e := range entries {
		if _, statErr := os.Stat(e.dir); os.IsNotExist(statErr) {
			created = true
		}
		if mkdirErr := os.MkdirAll(e.dir, 0o750); mkdirErr != nil {
			return false, fmt.Errorf("create %s: %w", e.dir, mkdirErr)
		}
		path := filepath.Join(e.dir, e.file)
		if _, statErr := os.Stat(path); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("stat %s: %w", path, statErr)
		}
		if writeErr := os.WriteFile(path, []byte(e.body), 0o600); writeErr != nil {
			return false, fmt.Errorf("write %s: %w", path, writeErr)
		}
		created = true
	}
	return created, nil
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

// sparseSpec builds the spec that preview and disk receive: answers only,
// with schema defaults stripped so omitempty drops them.
func sparseSpec(config *ProjectConfig) spec.Spec {
	components := make(map[string]spec.Component, len(config.Components))
	for name, component := range config.Components {
		omitSchemaDefaults(&component)
		components[name] = component
	}
	return spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    config.Name,
		Components: components,
	}
}

func omitSchemaDefaults(component *spec.Component) {
	if component.Role == spec.ComponentRoleService {
		component.Role = ""
	}
	if component.Port == spec.DefaultServicePort {
		component.Port = 0
	}
}

func cloneSpec(in *spec.Spec) (*spec.Spec, error) {
	data, err := yaml.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("failed to clone spec: %w", err)
	}
	var out spec.Spec
	if err = yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to clone spec: %w", err)
	}
	return &out, nil
}

func marshalSpecYAML(specData *spec.Spec) ([]byte, error) {
	data, err := yaml.Marshal(specData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec to YAML: %w", err)
	}
	return append([]byte(spec.SchemaModeline(spec.ManifestSchemaURL())), data...), nil
}

func writeSpecFile(specData *spec.Spec, path string) error {
	if path == "" {
		path = spec.DefaultSpecPath
	}
	data, err := marshalSpecYAML(specData)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err = os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	if err = renameio.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write spec to %s: %w", path, err)
	}
	return nil
}

// buildValidatedSpec assembles a sparse [spec.Spec] from config, then fills
// and validates a clone. Preview and disk get the sparse value.
func buildValidatedSpec(config *ProjectConfig) (*spec.Spec, error) {
	sparse := sparseSpec(config)
	clone, err := cloneSpec(&sparse)
	if err != nil {
		return nil, err
	}
	if err = spec.FillSpecWithDefaults(clone, clone.APIVersion); err != nil {
		return nil, fmt.Errorf("failed to apply defaults to spec: %w", err)
	}

	specBytes, err := yaml.Marshal(clone)
	if err != nil {
		return nil, fmt.Errorf("failed to convert spec to YAML: %w", err)
	}
	var specObj map[string]any
	if err = yaml.Unmarshal(specBytes, &specObj); err != nil {
		return nil, fmt.Errorf("failed to parse spec YAML: %w", err)
	}
	if err = spec.ValidateSpec(specObj, clone.APIVersion); err != nil {
		return nil, fmt.Errorf("spec validation failed: %w", err)
	}
	if err = spec.ValidateSpecComponents(clone); err != nil {
		return nil, fmt.Errorf("component validation failed: %w", err)
	}
	if err = spec.ValidateSpecTasks(clone); err != nil {
		return nil, fmt.Errorf("task validation failed: %w", err)
	}
	return &sparse, nil
}
