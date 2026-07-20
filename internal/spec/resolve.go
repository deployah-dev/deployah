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
	"maps"
	"slices"
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

		for _, w := range unknownEnvironmentNameWarnings(appSpec, keys) {
			report.Warnings = append(report.Warnings, w)
			resolved.Warnings = append(resolved.Warnings, w)
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

		rc, compFields, err := resolveComponent(compName, comp, platformEnv, env, substReport, platform)
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
) (ResolvedComponent, componentResolveResult, error) {
	rc := ResolvedComponent{}
	result := componentResolveResult{}

	var platformProfiles map[string]PlatformProfile
	if platform != nil {
		platformProfiles = platform.Profiles
	}

	// Profiles require a platform file when the component sets them.
	if comp.Profiles != nil && platform == nil {
		return rc, result, &ResolutionError{
			Code: ErrCodePlatformNotFound,
			Message: fmt.Sprintf(
				"component %q sets profiles but no platform file was found; "+
					"pass --platform-file or create %s",
				name, DefaultPlatformPath,
			),
		}
	}

	profileNames, err := ResolveProfileNames(comp.Profiles, platformProfiles)
	if err != nil {
		return rc, result, err
	}
	if len(profileNames) > 0 {
		merged, mergeErr := MergeProfiles(profileNames, platformProfiles)
		if mergeErr != nil {
			return rc, result, mergeErr
		}
		rc.Profiles = profileNames
		rc.MergedProfile = &merged
		result.fields = append(result.fields, ResolvedField{
			Component: name,
			Path:      "profiles",
			Value:     strings.Join(profileNames, ", "),
			Source:    "platform profiles (merged left to right)",
		})
	}

	if comp.Expose == nil {
		if rc.MergedProfile != nil {
			if profileErr := ValidateProfileAgainstComponent(name, comp, *rc.MergedProfile, platformEnv, ""); profileErr != nil {
				return rc, result, profileErr
			}
			applyResolvedStorageClass(&rc, &result, name, env, platformEnv)
		}
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

	domainKeys := slices.Sorted(maps.Keys(platformEnv.Domains))
	domainKey := comp.Expose.Domain
	domainDefaulted := false
	if domainKey == "" {
		key, domainErr := defaultDomainKey(platformEnv)
		if domainErr != nil {
			return rc, result, &ResolutionError{
				Code: ErrCodeDomainGap,
				Message: fmt.Sprintf(
					"component %q names no expose.domain and environment %q has %s",
					name, env.Original, domainErr,
				),
			}
		}
		domainKey = key
		domainDefaulted = true
	}
	domain, ok := platformEnv.Domains[domainKey]
	if !ok {
		return rc, result, &ResolutionError{
			Code: ErrCodeDomainGap,
			Message: fmt.Sprintf(
				"component %q references domain %q but environment %q does not define it (available: %s)",
				name, domainKey, env.Original, joinStrings(domainKeys),
			),
		}
	}
	rc.DomainKey = domainKey
	domainSource := fmt.Sprintf("platform environments.%s.domains.%s", env.Original, domainKey)
	if domainDefaulted {
		domainSource += " (default domain)"
	}

	// Build FQDN.
	var fqdn string
	if comp.Expose.Apex {
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
			Source:    domainSource + ".baseDomain (apex)",
		})
	} else {
		subdomain := name
		subdomainSource := "component name (default)"
		if comp.Expose.Subdomain != nil {
			subdomain = *comp.Expose.Subdomain
			subdomainSource = fmt.Sprintf("expose.subdomain=%q", subdomain)
		}
		isDynamic := substReport.DynamicSubdomains[name]

		// Skip DNS validation when the subdomain was produced by envsubst
		// (e.g. ${PR_NUMBER}): it may still contain template syntax in
		// offline mode, and at deploy time it will be substituted to a
		// valid label.
		if !isDynamic {
			if errs := k8svalidation.IsDNS1123Label(subdomain); len(errs) > 0 {
				field := fmt.Sprintf("expose.subdomain %q", subdomain)
				if comp.Expose.Subdomain == nil {
					field = fmt.Sprintf("the component name %q (used as the default subdomain)", subdomain)
				}
				return rc, result, &ResolutionError{
					Code: ErrCodeInvalidDNS,
					Message: fmt.Sprintf(
						"component %q: %s is not a valid DNS label: %s",
						name, field, strings.Join(errs, "; "),
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
			Source:    subdomainSource + " + " + domainSource + ".baseDomain",
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

	if rc.MergedProfile != nil {
		if profileErr := ValidateProfileAgainstComponent(name, comp, *rc.MergedProfile, platformEnv, domainKey); profileErr != nil {
			return rc, result, profileErr
		}
		applyResolvedStorageClass(&rc, &result, name, env, platformEnv)
	}

	return rc, result, nil
}

// applyResolvedStorageClass copies the merged profile's logical storage class
// key to the Kubernetes className after validation has succeeded.
func applyResolvedStorageClass(
	rc *ResolvedComponent,
	result *componentResolveResult,
	compName string,
	env EnvIdentity,
	platformEnv *PlatformEnvironment,
) {
	if rc.MergedProfile == nil || rc.MergedProfile.StorageClass == "" || platformEnv == nil {
		return
	}
	sc, ok := platformEnv.StorageClasses[rc.MergedProfile.StorageClass]
	if !ok {
		return
	}
	rc.StorageClass = sc.ClassName
	result.fields = append(result.fields, ResolvedField{
		Component: compName,
		Path:      "storageClass",
		Value:     sc.ClassName,
		Source: fmt.Sprintf(
			"platform profiles -> storageClass %q -> environments.%s.storageClasses.%s.className",
			rc.MergedProfile.StorageClass, env.Original, rc.MergedProfile.StorageClass,
		),
	})
}

// defaultDomainKey picks the domain used when a component names none: the
// environment's only domain, else the one marked default. The error text
// describes what the environment has instead; callers wrap it.
func defaultDomainKey(pe *PlatformEnvironment) (string, error) {
	keys := slices.Sorted(maps.Keys(pe.Domains))
	if len(keys) == 1 {
		return keys[0], nil
	}
	if len(keys) == 0 {
		return "", errors.New("no domains defined")
	}
	// Platform validation guarantees at most one default per environment.
	for k, d := range pe.Domains {
		if d.Default {
			return k, nil
		}
	}
	return "", fmt.Errorf("several domains and none marked default: %s", joinStrings(keys))
}

// unknownEnvironmentNameWarnings reports spec environment keys and component
// environments filter entries that match nothing in the platform registry.
// Warnings, not errors, so not-yet-registered environments stay legal.
func unknownEnvironmentNameWarnings(appSpec *Spec, registry []string) []string {
	var warnings []string
	for _, name := range appSpec.EnvironmentNames() {
		if _, ok := matchEnvKey(name, registry); !ok {
			warnings = append(warnings, fmt.Sprintf(
				"environment %q has overrides in the spec but is not defined in the platform file (available: %s)",
				name, joinStrings(registry)))
		}
	}

	for _, compName := range slices.Sorted(maps.Keys(appSpec.Components)) {
		for _, entry := range appSpec.Components[compName].Environments {
			if _, ok := matchEnvKey(entry, registry); !ok {
				warnings = append(warnings, fmt.Sprintf(
					"component %q environments filter entry %q matches no environment in the platform file (available: %s)",
					compName, entry, joinStrings(registry)))
			}
		}
	}
	return warnings
}

// CrossCheckPlatformReferences checks spec references against the platform
// file without picking an environment. It returns problems (expose.domain
// keys defined in no platform environment, unknown profile names) and
// warnings (environment names unknown to the registry). Domains containing
// ${VAR} tokens are skipped.
func CrossCheckPlatformReferences(appSpec *Spec, platform *PlatformConfig) (problems, warnings []string) {
	if appSpec == nil || platform == nil {
		return nil, nil
	}
	registry := slices.Sorted(maps.Keys(platform.Environments))
	warnings = unknownEnvironmentNameWarnings(appSpec, registry)

	domainKeys := make(map[string]bool)
	for _, pe := range platform.Environments {
		for k := range pe.Domains {
			domainKeys[k] = true
		}
	}
	available := slices.Sorted(maps.Keys(domainKeys))
	profileKeys := slices.Sorted(maps.Keys(platform.Profiles))

	for _, name := range slices.Sorted(maps.Keys(appSpec.Components)) {
		comp := appSpec.Components[name]

		if len(comp.Profiles) > 0 {
			if platform.Profiles == nil {
				problems = append(problems, fmt.Sprintf(
					"component %q sets profiles but the platform file has no profiles section",
					name))
			} else {
				for _, profileName := range comp.Profiles {
					if _, ok := platform.Profiles[profileName]; !ok {
						problems = append(problems, fmt.Sprintf(
							"component %q references profile %q which is not defined in the platform file (available: %s)",
							name, profileName, joinStrings(profileKeys)))
					}
				}
			}
		}

		if comp.Expose == nil || strings.Contains(comp.Expose.Domain, "${") {
			continue
		}
		domain := comp.Expose.Domain
		if domain != "" && !domainKeys[domain] {
			problems = append(problems, fmt.Sprintf(
				"component %q expose.domain %q is not defined in any platform environment (available: %s)",
				name, domain, joinStrings(available)))
			continue
		}
		// Per-environment gaps for the environments the component is
		// active in: a named domain missing there, or an ambiguous default.
		for _, envKey := range registry {
			if len(comp.Environments) > 0 {
				if _, ok := matchEnvKey(envKey, comp.Environments); !ok {
					continue
				}
			}
			pe := platform.Environments[envKey]
			if domain != "" {
				if _, ok := pe.Domains[domain]; !ok {
					warnings = append(warnings, fmt.Sprintf(
						"component %q references domain %q but environment %q does not define it",
						name, domain, envKey))
				}
				continue
			}
			if _, err := defaultDomainKey(&pe); err != nil {
				warnings = append(warnings, fmt.Sprintf(
					"component %q relies on the default domain but environment %q has %s",
					name, envKey, err))
			}
		}
	}
	return problems, warnings
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
