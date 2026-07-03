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

package spec

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// Resolve processes all components in spec at once for env, combining them
// with the platform configuration. It returns a [ResolvedSpec] containing
// per-component resolved values and a [ResolutionReport] with field provenance.
//
// The platform parameter may be nil only when no component uses an expose block
// (offline manifest-only validation). When any component uses expose and
// platform is nil, Resolve returns a hard error.
//
// substReport identifies which expose.subdomain fields were produced by
// envsubst; the wildcard static-subdomain warning does not fire for those.
func Resolve(
	appSpec *Spec,
	platform *PlatformConfig,
	env EnvIdentity,
	substReport SubstitutionReport,
) (*ResolvedSpec, *ResolutionReport, error) {
	report := &ResolutionReport{Env: env}
	resolved := &ResolvedSpec{
		Spec:       appSpec,
		Env:        env,
		Components: make(map[string]ResolvedComponent),
	}

	// Resolve Kubernetes context from platform.
	var platformEnv *PlatformEnvironment
	if platform != nil {
		keys := make([]string, 0, len(platform.Environments))
		for k := range platform.Environments {
			keys = append(keys, k)
		}
		if matched, ok := matchEnvKey(env.Original, keys); ok {
			pe := platform.Environments[matched]
			platformEnv = &pe
			resolved.KubeContext = pe.Context
			report.Fields = append(report.Fields, ResolvedField{
				Path:   "context",
				Value:  pe.Context,
				Source: fmt.Sprintf("platform environments.%s.context", matched),
			})
		} else if env.Original != "" {
			re := &ResolutionError{
				Code: ErrCodePlatformEnvNotFound,
				Message: fmt.Sprintf(
					"environment %q not found in platform file (available: %s)",
					env.Original, joinStrings(keys),
				),
			}
			report.ErrorCode = re.Code
			report.ErrorMessage = re.Message
			return nil, report, re
		}
	}

	// Resolve each component. Process in sorted order for determinism.
	componentNames := make([]string, 0, len(appSpec.Components))
	for n := range appSpec.Components {
		componentNames = append(componentNames, n)
	}
	sort.Strings(componentNames)

	// Track FQDNs for collision detection (at the FQDN level, not per-domain).
	fqdnOwners := make(map[string]string) // FQDN -> component name

	for _, compName := range componentNames {
		comp := appSpec.Components[compName]

		// Filter by component.Environments if set.
		if len(comp.Environments) > 0 {
			if _, ok := matchEnvKey(env.Original, comp.Environments); !ok {
				continue
			}
		}

		rc, compFields, err := resolveComponent(compName, comp, platformEnv, env, substReport, platform, fqdnOwners)
		if err != nil {
			var re *ResolutionError
			if errors.As(err, &re) {
				report.ErrorCode = re.Code
			} else {
				report.ErrorCode = ErrCodeDomainGap
			}
			report.ErrorMessage = err.Error()
			return nil, report, err
		}

		for _, w := range compFields.warnings {
			report.Warnings = append(report.Warnings, w)
			resolved.Warnings = append(resolved.Warnings, w)
		}
		report.Fields = append(report.Fields, compFields.fields...)

		if rc.FQDN != "" {
			if existing, collision := fqdnOwners[rc.FQDN]; collision {
				msg := fmt.Sprintf("components %q and %q both resolve to hostname %q (error code %s)",
					existing, compName, rc.FQDN, ErrCodeFQDNCollision)
				report.ErrorCode = ErrCodeFQDNCollision
				report.ErrorMessage = msg
				return nil, report, fmt.Errorf("%s", msg)
			}
			fqdnOwners[rc.FQDN] = compName
		}

		resolved.Components[compName] = rc
	}

	return resolved, report, nil
}

type componentResolveResult struct {
	fields   []ResolvedField
	warnings []string
}

// resolveComponent computes the ResolvedComponent for a single component.
func resolveComponent(
	name string,
	comp Component,
	platformEnv *PlatformEnvironment,
	env EnvIdentity,
	substReport SubstitutionReport,
	platform *PlatformConfig,
	fqdnOwners map[string]string,
) (ResolvedComponent, componentResolveResult, error) {
	rc := ResolvedComponent{}
	result := componentResolveResult{}

	if comp.Expose == nil {
		return rc, result, nil
	}

	// Expose block is present: platform file is required.
	if platform == nil || platformEnv == nil {
		return rc, result, &ResolutionError{
			Code: ErrCodePlatformNotFound,
			Message: fmt.Sprintf(
				"component %q uses expose.domain but no platform file was found; "+
					"pass --platform-file or create %s",
				name, DefaultPlatformPath,
			),
		}
	}

	domainKey := comp.Expose.Domain
	domain, ok := platformEnv.Domains[domainKey]
	if !ok {
		return rc, result, &ResolutionError{
			Code: ErrCodeDomainGap,
			Message: fmt.Sprintf(
				"component %q references domain %q but environment %q does not define it",
				name, domainKey, env.Original,
			),
		}
	}

	// Build FQDN.
	var fqdn string
	if comp.Expose.Subdomain == nil {
		// Apex mode.
		fqdn = domain.BaseDomain
		if errs := k8svalidation.IsDNS1123Subdomain(fqdn); len(errs) > 0 {
			return rc, result, &ResolutionError{
				Code: ErrCodeInvalidDNS,
				Message: fmt.Sprintf(
					"component %q: resolved apex FQDN %q is not a valid DNS name: %s",
					name, fqdn, strings.Join(errs, "; "),
				),
			}
		}
		result.fields = append(result.fields, ResolvedField{
			Component: name,
			Path:      "expose.host",
			Value:     fqdn,
			Source:    fmt.Sprintf("platform environments.%s.domains.%s.baseDomain (apex)", env.Original, domainKey),
		})
	} else {
		subdomain := *comp.Expose.Subdomain
		isDynamic := substReport.DynamicSubdomains[name]

		// Skip DNS validation when the subdomain was produced by envsubst
		// (e.g. ${PR_NUMBER}): it may still contain template syntax in
		// offline mode, and at deploy time it will be substituted to a
		// valid label.
		if !isDynamic {
			if errs := k8svalidation.IsDNS1123Label(subdomain); len(errs) > 0 {
				return rc, result, &ResolutionError{
					Code: ErrCodeInvalidDNS,
					Message: fmt.Sprintf(
						"component %q: expose.subdomain %q is not a valid DNS label: %s",
						name, subdomain, strings.Join(errs, "; "),
					),
				}
			}
		}
		fqdn = subdomain + "." + domain.BaseDomain
		if !isDynamic {
			if errs := k8svalidation.IsDNS1123Subdomain(fqdn); len(errs) > 0 {
				return rc, result, &ResolutionError{
					Code: ErrCodeInvalidDNS,
					Message: fmt.Sprintf(
						"component %q: resolved FQDN %q exceeds DNS limits: %s",
						name, fqdn, strings.Join(errs, "; "),
					),
				}
			}
		}
		result.fields = append(result.fields, ResolvedField{
			Component: name,
			Path:      "expose.host",
			Value:     fqdn,
			Source:    fmt.Sprintf("expose.subdomain=%q + platform environments.%s.domains.%s.baseDomain", subdomain, env.Original, domainKey),
		})

		// Wildcard static-subdomain warning.
		isWildcardMatch := env.MapKey != env.Original
		if isWildcardMatch && !isDynamic {
			// Check if the platform env allows static subdomains.
			platEnvKeys := make([]string, 0, len(platform.Environments))
			for k := range platform.Environments {
				platEnvKeys = append(platEnvKeys, k)
			}
			var allowStatic bool
			if matchedKey, keyMatched := matchEnvKey(env.Original, platEnvKeys); keyMatched {
				allowStatic = platform.Environments[matchedKey].AllowStaticSubdomain
			}
			if !allowStatic {
				w := fmt.Sprintf(
					"component %q: subdomain %q is static but environment %q is a wildcard match; "+
						"simultaneous deployments to this environment will share the same hostname "+
						"(suppress with allowStaticSubdomain: true in the platform file, error code %s)",
					name, subdomain, env.Original, ErrCodeStaticWildcardSubdomain,
				)
				result.warnings = append(result.warnings, w)
			}
		}
	}
	rc.FQDN = fqdn

	// Resolve TLS.
	if domain.TLS != nil {
		rc.TLSMode = domain.TLS.Mode
		result.fields = append(result.fields, ResolvedField{
			Component: name,
			Path:      "expose.tls.mode",
			Value:     string(domain.TLS.Mode),
			Source:    fmt.Sprintf("platform environments.%s.domains.%s.tls.mode", env.Original, domainKey),
		})
		switch domain.TLS.Mode {
		case TLSModeCertManager:
			rc.TLSIssuer = domain.TLS.Issuer
			result.fields = append(result.fields, ResolvedField{
				Component: name,
				Path:      "expose.tls.issuer",
				Value:     domain.TLS.Issuer,
				Source:    fmt.Sprintf("platform environments.%s.domains.%s.tls.issuer", env.Original, domainKey),
			})
		case TLSModeSecretName:
			rc.TLSSecretName = domain.TLS.SecretName
			result.fields = append(result.fields, ResolvedField{
				Component: name,
				Path:      "expose.tls.secretName",
				Value:     domain.TLS.SecretName,
				Source:    fmt.Sprintf("platform environments.%s.domains.%s.tls.secretName", env.Original, domainKey),
			})
		}
	}

	return rc, result, nil
}

// joinStrings returns a sorted, comma-separated, quoted list of strings.
func joinStrings(ss []string) string {
	if len(ss) == 0 {
		return "(none)"
	}
	sorted := make([]string, 0, len(ss))
	sorted = append(sorted, ss...)
	sort.Strings(sorted)
	quoted := make([]string, 0, len(sorted))
	for _, s := range sorted {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	return strings.Join(quoted, ", ")
}

// ResolveForDisplay is like [Resolve] but never returns a hard error for
// missing platform file. Instead it marks the report with PLATFORM_NOT_FOUND
// and returns partial results. Used by the resolve command in offline mode.
func ResolveForDisplay(
	appSpec *Spec,
	platform *PlatformConfig,
	env EnvIdentity,
	substReport SubstitutionReport,
) (*ResolvedSpec, *ResolutionReport, error) {
	if platform == nil {
		// Partial result: no platform, emit PLATFORM_NOT_FOUND.
		report := &ResolutionReport{
			Env:          env,
			ErrorCode:    ErrCodePlatformNotFound,
			ErrorMessage: fmt.Sprintf("platform file not found; run with --platform-file or create %s", DefaultPlatformPath),
		}
		resolved := &ResolvedSpec{
			Spec:       appSpec,
			Env:        env,
			Components: make(map[string]ResolvedComponent),
		}
		return resolved, report, nil
	}
	return Resolve(appSpec, platform, env, substReport)
}

// PlatformEnvContext returns the Kubernetes context for the given environment
// from the platform config, or empty string if not found.
func PlatformEnvContext(platform *PlatformConfig, envName string) string {
	if platform == nil {
		return ""
	}
	keys := make([]string, 0, len(platform.Environments))
	for k := range platform.Environments {
		keys = append(keys, k)
	}
	if matched, ok := matchEnvKey(envName, keys); ok {
		return platform.Environments[matched].Context
	}
	return ""
}
