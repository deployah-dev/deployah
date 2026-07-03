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
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
)

// Options holds command-line flags for resolve.
type Options struct {
	Environment string `nabat:"environment"`
	Output      string `nabat:"output"`
}

// Register adds the resolve command to app.
func Register(app *nabat.App) {
	app.MustCommand("resolve",
		nabat.WithDescription("Show the fully resolved configuration for an environment"),
		nabat.WithLongDescription(`Show the fully resolved configuration for a given environment.

resolve is offline: it never contacts a Kubernetes cluster. It loads the
platform file (deployah.platform.yaml) when present and performs full
resolution, including FQDN construction and TLS mode selection. When the
platform file is absent the output is partial and includes PLATFORM_NOT_FOUND.

Use --output json for byte-stable, machine-readable output.
`),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to resolve"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. production"))),
		nabat.WithFlag("output", "text", nabat.WithShort('o'), nabat.WithUsage("Output format: text or json")),
		nabat.WithExample(`
# Resolve the production environment
deployah resolve production

# Machine-readable JSON output
deployah resolve production --output json`),
		nabat.WithRun(runResolve),
	)
}

func runResolve(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	sess := session.FromContext(c)

	// Load and parse the manifest (no envsubst, no cluster).
	rawSpec, _, err := spec.ParseManifest(sess.SpecPath())
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	// Resolve the environment from the manifest.
	matchedKey, _, envErr := spec.ResolveEnvironment(rawSpec.Environments, opts.Environment)
	envIdentity := spec.NormalizeEnv(opts.Environment)
	if envErr == nil && matchedKey != "" {
		envIdentity = spec.NormalizeEnv(matchedKey)
	}

	// Load platform (degraded when absent).
	platform, platformErr := sess.Platform()
	if platformErr != nil {
		// Platform file was found but failed to load: hard error.
		return fmt.Errorf("load platform: %w", platformErr)
	}

	// Build substitution report (pre-scan for ${VAR} tokens).
	substReport := spec.PrescanSubstitutionReport(rawSpec)

	// Run resolution (display mode: degrades gracefully when platform is nil).
	resolved, report, resolveErr := spec.ResolveForDisplay(rawSpec, platform, envIdentity, substReport)
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

// outputText writes a human-readable resolution summary to stdout.
func outputText(c *nabat.Context, resolved *spec.ResolvedSpec, report *spec.ResolutionReport) error {
	if report.ErrorCode != "" {
		c.Println(fmt.Sprintf("Resolution error [%s]: %s", report.ErrorCode, report.ErrorMessage))
		return nil
	}

	c.Println(fmt.Sprintf("Environment: %s", report.Env.Original))
	if resolved.KubeContext != "" {
		c.Println(fmt.Sprintf("Context:     %s", resolved.KubeContext))
	}

	// Sort components for deterministic output.
	names := make([]string, 0, len(resolved.Components))
	for n := range resolved.Components {
		names = append(names, n)
	}
	sort.Strings(names)

	hasExposed := false
	for _, name := range names {
		if resolved.Components[name].FQDN != "" {
			hasExposed = true
			break
		}
	}
	if !hasExposed {
		c.Println("Components: (none with expose blocks)")
	} else {
		c.Println("\nComponents:")
		for _, name := range names {
			rc := resolved.Components[name]
			if rc.FQDN == "" {
				continue
			}
			c.Println(fmt.Sprintf("  %s:", name))
			c.Println(fmt.Sprintf("    hostname: %s", rc.FQDN))
			if rc.TLSMode != "" {
				c.Println(fmt.Sprintf("    tls.mode: %s", rc.TLSMode))
			}
			if rc.TLSIssuer != "" {
				c.Println(fmt.Sprintf("    tls.issuer: %s", rc.TLSIssuer))
			}
			if rc.TLSSecretName != "" {
				c.Println(fmt.Sprintf("    tls.secretName: %s", rc.TLSSecretName))
			}
		}
	}

	if len(report.Warnings) > 0 {
		c.Println("\nWarnings:")
		for _, w := range report.Warnings {
			c.Println(fmt.Sprintf("  - %s", w))
		}
	}
	return nil
}

// jsonResolveOutput is the stable JSON representation of a resolution result.
// Fields are in a fixed order; encoding/json sorts map keys since Go 1.12,
// so the output is byte-stable for the same input.
type jsonResolveOutput struct {
	Environment  string                   `json:"environment"`
	Context      string                   `json:"context,omitempty"`
	Components   map[string]jsonComponent `json:"components"`
	Warnings     []string                 `json:"warnings,omitempty"`
	ErrorCode    string                   `json:"error_code,omitempty"`
	ErrorMessage string                   `json:"error_message,omitempty"`
}

type jsonComponent struct {
	FQDN          string `json:"fqdn,omitempty"`
	TLSMode       string `json:"tls_mode,omitempty"`
	TLSIssuer     string `json:"tls_issuer,omitempty"`
	TLSSecretName string `json:"tls_secret_name,omitempty"`
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
			FQDN:          rc.FQDN,
			TLSMode:       string(rc.TLSMode),
			TLSIssuer:     rc.TLSIssuer,
			TLSSecretName: rc.TLSSecretName,
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	c.Println(string(data))
	return nil
}
