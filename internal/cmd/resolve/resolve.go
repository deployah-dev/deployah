// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package resolve implements the "deployah resolve" command which shows the
// fully resolved configuration for a given environment without contacting a
// cluster. It degrades gracefully when no platform file is present.
package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
)

// Options holds command-line flags for resolve.
type Options struct {
	Environment  string `nabat:"environment"`
	Output       string `nabat:"output"`
	Environments bool   `nabat:"environments"`
}

// Register adds the resolve command to app.
func Register(app *nabat.App) {
	app.MustCommand("resolve",
		nabat.WithDescription("Show the fully resolved configuration for an environment"),
		nabat.WithLongDescription(`Show the fully resolved configuration for a given environment.

resolve is offline: it never contacts a Kubernetes cluster. It loads the
platform file (deployah.platform.yaml) when present and performs full
resolution, including FQDN construction and TLS mode selection. When the
platform file is absent the output is partial and a warning is recorded.

It prints resolved runtime FileValues and ExplicitValues in text and JSON.
Those maps are not a secret store; do not treat resolve output as
secret-safe CI output.

With --environments it instead lists every environment from the spec and
platform files: where each is registered, its context (or the kubeconfig
fallback), its domains, and any spec overrides.

Use --output json for byte-stable, machine-readable output.
`),
		nabat.WithArg("environment", "", nabat.WithUsage("Environment to resolve (omit with --environments)")),
		nabat.WithFlag("output", "text", nabat.WithShort('o'), nabat.WithUsage("Output format: text or json")),
		nabat.WithFlag("environments", false, nabat.WithUsage("List every environment from the spec and platform files instead of resolving one")),
		nabat.WithExample(`
# Resolve the production environment
deployah resolve production

# Machine-readable JSON output
deployah resolve production --output json

# Overview of every environment from both files
deployah resolve --environments`),
		nabat.WithRun(runResolve),
	)
}

func runResolve(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	sess := session.FromContext(c)
	ws := sess.Workspace()

	if opts.Environments {
		return runEnvironmentsOverview(c, sess, opts.Output)
	}
	if opts.Environment == "" {
		return fmt.Errorf("environment argument required; or pass --environments for an overview")
	}

	// Load platform (degraded when absent). It owns the environment registry.
	platform, platformErr := ws.Platform()
	if platformErr != nil {
		// Platform file was found but failed to load: hard error.
		return fmt.Errorf("load platform: %w", platformErr)
	}

	// Keep the user's exact name (wildcard instances included) so warnings
	// and the report match what deploy would do; Resolve prefix-matches
	// registry keys internally.
	envIdentity := spec.NormalizeEnv(opts.Environment)

	manifest, substReport, err := spec.Load(c, ws.SpecPath(), opts.Environment, platform, spec.AllowHookCycleForDisplay())
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	// Run resolution (display mode: degrades gracefully when platform is nil).
	resolved, report, resolveErr := spec.ResolveForDisplay(manifest, platform, envIdentity, substReport)
	if resolveErr != nil {
		if report != nil && report.ErrorCode != "" {
			return fmt.Errorf("resolution failed (%s): %w", report.ErrorCode, resolveErr)
		}
		return fmt.Errorf("resolution failed: %w", resolveErr)
	}

	switch strings.ToLower(opts.Output) {
	case "json":
		return outputJSON(c, resolved, report)
	default:
		return outputText(c, resolved, report)
	}
}

// outputText writes a human-readable resolution summary.
func outputText(c *nabat.Context, resolved *spec.ResolvedSpec, report *spec.ResolutionReport) error {
	c.Println(fmt.Sprintf("Environment: %s", report.Env.Original))
	if resolved.KubeContext != "" {
		c.Println(fmt.Sprintf("Context:     %s", resolved.KubeContext))
	}

	names := slices.Sorted(maps.Keys(resolved.Components))
	visible := make([]string, 0, len(names))
	for _, name := range names {
		if hasDisplayableComponentResolution(resolved.Components[name]) {
			visible = append(visible, name)
		}
	}
	if len(visible) > 0 {
		c.Println("\nComponents:")
		for _, name := range visible {
			rc := resolved.Components[name]
			c.Println(fmt.Sprintf("  %s:", name))
			if rc.FQDN != "" {
				c.Println(fmt.Sprintf("    hostname: %s", rc.FQDN))
			}
			if rc.TLSMode != "" {
				c.Println(fmt.Sprintf("    tls.mode: %s", rc.TLSMode))
			}
			if rc.TLSIssuer != "" {
				c.Println(fmt.Sprintf("    tls.issuer: %s", rc.TLSIssuer))
			}
			if rc.TLSSecretName != "" {
				c.Println(fmt.Sprintf("    tls.secretName: %s", rc.TLSSecretName))
			}
			if len(rc.Profiles) > 0 {
				c.Println(fmt.Sprintf("    profiles: %s", strings.Join(rc.Profiles, ", ")))
			}
			if rc.MergedProfile != nil {
				printMergedProfile(c, rc.MergedProfile)
			}
			if rc.StorageClass != "" {
				c.Println(fmt.Sprintf("    storageClass: %s", rc.StorageClass))
			}
			printRuntimeMaps(c, rc.Runtime)
		}
	}

	taskNames := slices.Sorted(maps.Keys(resolved.Tasks))
	printedTasks := false
	for _, name := range taskNames {
		rt := resolved.Tasks[name]
		if !hasRuntimeMaps(rt.Runtime) {
			continue
		}
		if !printedTasks {
			c.Println("\nTasks:")
			printedTasks = true
		}
		c.Println(fmt.Sprintf("  %s:", name))
		printRuntimeMaps(c, rt.Runtime)
	}

	if len(report.Warnings) > 0 {
		c.Println("\nWarnings:")
		for _, w := range report.Warnings {
			c.Println(fmt.Sprintf("  - %s", w))
		}
	}
	return nil
}

func hasDisplayableComponentResolution(rc spec.ResolvedComponent) bool {
	return rc.FQDN != "" ||
		rc.TLSMode != "" ||
		rc.TLSIssuer != "" ||
		rc.TLSSecretName != "" ||
		len(rc.Profiles) > 0 ||
		rc.MergedProfile != nil ||
		rc.StorageClass != "" ||
		hasRuntimeMaps(rc.Runtime)
}

func hasRuntimeMaps(rt spec.ResolvedRuntimeEnvironment) bool {
	return len(rt.FileValues) > 0 || len(rt.ExplicitValues) > 0
}

func printRuntimeMaps(c *nabat.Context, rt spec.ResolvedRuntimeEnvironment) {
	if len(rt.FileValues) > 0 {
		c.Println("    fileValues:")
		for _, k := range slices.Sorted(maps.Keys(rt.FileValues)) {
			c.Println(fmt.Sprintf("      %s: %s", k, rt.FileValues[k]))
		}
	}
	if len(rt.ExplicitValues) > 0 {
		c.Println("    explicitValues:")
		for _, k := range slices.Sorted(maps.Keys(rt.ExplicitValues)) {
			c.Println(fmt.Sprintf("      %s: %s", k, rt.ExplicitValues[k]))
		}
	}
}

// jsonResolveOutput is the stable JSON representation of a resolution result.
// Fields are in a fixed order; encoding/json sorts map keys since Go 1.12,
// so the output is byte-stable for the same input.
type jsonResolveOutput struct {
	Environment  string                   `json:"environment"`
	Context      string                   `json:"context,omitempty"`
	Components   map[string]jsonComponent `json:"components"`
	Tasks        map[string]jsonRuntime   `json:"tasks,omitempty"`
	Warnings     []string                 `json:"warnings,omitempty"`
	ErrorCode    string                   `json:"error_code,omitempty"`
	ErrorMessage string                   `json:"error_message,omitempty"`
}

type jsonRuntime struct {
	FileValues     map[string]string `json:"file_values,omitempty"`
	ExplicitValues map[string]string `json:"explicit_values,omitempty"`
}

type jsonComponent struct {
	FQDN           string                `json:"fqdn,omitempty"`
	TLSMode        string                `json:"tls_mode,omitempty"`
	TLSIssuer      string                `json:"tls_issuer,omitempty"`
	TLSSecretName  string                `json:"tls_secret_name,omitempty"`
	Profiles       []string              `json:"profiles,omitempty"`
	MergedProfile  *spec.PlatformProfile `json:"merged_profile,omitempty"`
	StorageClass   string                `json:"storage_class,omitempty"`
	FileValues     map[string]string     `json:"file_values,omitempty"`
	ExplicitValues map[string]string     `json:"explicit_values,omitempty"`
}

// printMergedProfile writes key merged profile fields for text output.
func printMergedProfile(c *nabat.Context, p *spec.PlatformProfile) {
	if len(p.NodeSelector) > 0 {
		keys := slices.Sorted(maps.Keys(p.NodeSelector))
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, p.NodeSelector[k]))
		}
		c.Println(fmt.Sprintf("    nodeSelector: %s", strings.Join(parts, ", ")))
	}
	if len(p.Tolerations) > 0 {
		c.Println(fmt.Sprintf("    tolerations: %d", len(p.Tolerations)))
	}
	if len(p.PodLabels) > 0 {
		keys := slices.Sorted(maps.Keys(p.PodLabels))
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, p.PodLabels[k]))
		}
		c.Println(fmt.Sprintf("    podLabels: %s", strings.Join(parts, ", ")))
	}
	if p.AllowedDomains != nil {
		if len(p.AllowedDomains) == 0 {
			c.Println("    allowedDomains: (none)")
		} else {
			c.Println(fmt.Sprintf("    allowedDomains: %s", strings.Join(p.AllowedDomains, ", ")))
		}
	}
	if p.MaxResources != nil {
		parts := make([]string, 0, 2)
		if p.MaxResources.CPU != nil && !p.MaxResources.CPU.IsZero() {
			parts = append(parts, "cpu="+p.MaxResources.CPU.String())
		}
		if p.MaxResources.Memory != nil && !p.MaxResources.Memory.IsZero() {
			parts = append(parts, "memory="+p.MaxResources.Memory.String())
		}
		if len(parts) > 0 {
			c.Println(fmt.Sprintf("    maxResources: %s", strings.Join(parts, ", ")))
		}
	}
}

// envOverviewRow is one environment in the --environments overview.
type envOverviewRow struct {
	Name string `json:"name"`
	// Source is "platform", "spec" (no platform file), or "spec-only"
	// (platform file exists but does not register this name).
	Source          string   `json:"source"`
	Deployable      bool     `json:"deployable"`
	Context         string   `json:"context,omitempty"`
	ContextFallback string   `json:"context_fallback,omitempty"`
	Domains         []string `json:"domains,omitempty"`
	Overrides       []string `json:"overrides,omitempty"`
}

// buildEnvironmentOverview merges environment names from both files into
// sorted overview rows. currentCtx is the kubeconfig current-context, shown
// as the fallback for deployable environments without a context.
func buildEnvironmentOverview(rawSpec *spec.Spec, platform *spec.PlatformConfig, currentCtx string) []envOverviewRow {
	names := make(map[string]bool)
	if platform != nil {
		for n := range platform.Environments {
			names[n] = true
		}
	}
	for n := range rawSpec.Environments {
		names[n] = true
	}

	rows := make([]envOverviewRow, 0, len(names))
	for _, n := range slices.Sorted(maps.Keys(names)) {
		row := envOverviewRow{Name: n}
		if platform == nil {
			row.Source = "spec"
			row.Deployable = true
			row.ContextFallback = currentCtx
		} else if pe, registered := platform.Environments[n]; registered {
			row.Source = "platform"
			row.Deployable = true
			row.Context = pe.Context
			if pe.Context == "" {
				row.ContextFallback = currentCtx
			}
			row.Domains = slices.Sorted(maps.Keys(pe.Domains))
		} else {
			row.Source = "spec-only"
		}
		if env, ok := rawSpec.Environments[n]; ok {
			if env.EnvFile != "" {
				row.Overrides = append(row.Overrides, "envFile")
			}
			if env.ConfigFile != "" {
				row.Overrides = append(row.Overrides, "configFile")
			}
			if len(env.Variables) > 0 {
				row.Overrides = append(row.Overrides, "variables")
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// runEnvironmentsOverview prints every environment from the spec and
// platform files in the requested output format.
func runEnvironmentsOverview(c *nabat.Context, sess *session.Session, output string) error {
	ws := sess.Workspace()
	rawSpec, err := ws.ParseManifest()
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	platform, platformErr := ws.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform: %w", platformErr)
	}

	rows := buildEnvironmentOverview(rawSpec, platform, sess.CurrentKubeContext())

	if strings.EqualFold(output, "json") {
		return c.JSON(map[string][]envOverviewRow{"environments": rows})
	}

	if platform == nil {
		c.Println("No platform file found; the spec's environments act as the registry.")
	}
	if len(rows) == 0 {
		c.Println("No environments are defined in either file.")
		return nil
	}
	c.Println("Environments:")
	for _, r := range rows {
		c.Println("  " + r.Name)
		if r.Source == "spec-only" {
			c.Println("    registered: no — add it to the platform file before deploying there")
			continue
		}
		c.Println("    registered: " + r.Source)
		switch {
		case r.Context != "":
			c.Println("    context:    " + r.Context)
		case r.ContextFallback != "":
			c.Println(fmt.Sprintf("    context:    (none — deploys follow the current kubeconfig context %q)", r.ContextFallback))
		default:
			c.Println("    context:    (none — deploys follow the current kubeconfig context)")
		}
		if len(r.Domains) > 0 {
			c.Println("    domains:    " + strings.Join(r.Domains, ", "))
		}
		if len(r.Overrides) > 0 {
			c.Println("    overrides:  " + strings.Join(r.Overrides, ", "))
		}
	}
	return nil
}

// outputJSON writes the resolution result as byte-stable JSON.
func outputJSON(c *nabat.Context, resolved *spec.ResolvedSpec, report *spec.ResolutionReport) error {
	out := jsonResolveOutput{
		Environment:  report.Env.Original,
		Context:      resolved.KubeContext,
		Components:   make(map[string]jsonComponent),
		Warnings:     report.Warnings,
		ErrorCode:    report.ErrorCode,
		ErrorMessage: report.ErrorMessage,
	}

	for name, rc := range resolved.Components {
		out.Components[name] = jsonComponent{
			FQDN:           rc.FQDN,
			TLSMode:        string(rc.TLSMode),
			TLSIssuer:      rc.TLSIssuer,
			TLSSecretName:  rc.TLSSecretName,
			Profiles:       rc.Profiles,
			MergedProfile:  rc.MergedProfile,
			StorageClass:   rc.StorageClass,
			FileValues:     cloneIfPresent(rc.Runtime.FileValues),
			ExplicitValues: cloneIfPresent(rc.Runtime.ExplicitValues),
		}
	}
	for name, rt := range resolved.Tasks {
		runtime := jsonRuntime{
			FileValues:     cloneIfPresent(rt.Runtime.FileValues),
			ExplicitValues: cloneIfPresent(rt.Runtime.ExplicitValues),
		}
		if len(runtime.FileValues) == 0 && len(runtime.ExplicitValues) == 0 {
			continue
		}
		if out.Tasks == nil {
			out.Tasks = make(map[string]jsonRuntime)
		}
		out.Tasks[name] = runtime
	}

	return c.JSON(out)
}

func cloneIfPresent(vals map[string]string) map[string]string {
	if len(vals) == 0 {
		return nil
	}
	return maps.Clone(vals)
}
