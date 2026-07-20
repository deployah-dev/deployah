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
	"fmt"
	"maps"
	"reflect"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
)

// ResolveProfileNames decides which profile names apply to a component.
//
// Rules:
//   - nil componentProfiles (field omitted): apply ["default"] when the
//     platform defines that profile; otherwise no profiles.
//   - empty componentProfiles (profiles: []): opt-out. Error when a default
//     profile exists; otherwise return an empty list.
//   - non-empty list: require a platform profiles map; prepend default when
//     defined (and not already listed).
func ResolveProfileNames(componentProfiles []string, platformProfiles map[string]PlatformProfile) ([]string, error) {
	_, hasDefault := platformProfiles[DefaultProfileName]

	if componentProfiles == nil {
		if hasDefault {
			return []string{DefaultProfileName}, nil
		}
		return nil, nil
	}

	if len(componentProfiles) == 0 {
		if hasDefault {
			return nil, &ResolutionError{
				Code: ErrCodeProfileOptOutBlocked,
				Message: fmt.Sprintf(
					"profiles: [] is not allowed when a %q profile is defined; the default profile cannot be opted out of",
					DefaultProfileName,
				),
			}
		}
		return []string{}, nil
	}

	if platformProfiles == nil {
		return nil, &ResolutionError{
			Code: ErrCodeProfileNotFound,
			Message: "component sets profiles but the platform file has no profiles section; " +
				"add a root-level profiles map to deployah.platform.yaml",
		}
	}

	names := make([]string, 0, len(componentProfiles)+1)
	if hasDefault {
		names = append(names, DefaultProfileName)
	}
	for _, n := range componentProfiles {
		if n == DefaultProfileName {
			continue
		}
		names = append(names, n)
	}
	return names, nil
}

// MergeProfiles looks up names in profiles and merges them left to right.
//
// Merge rules:
//   - maps (nodeSelector, podLabels, podAnnotations): deep merge, last wins
//   - security contexts: field overlay; non-nil overlay pointers win, including
//     false *bool values that mergo would skip
//   - arrays (tolerations): concatenate and deduplicate identical entries
//   - scalars (storageClass): last non-empty wins
//   - allowedDomains: intersection of explicit lists; omitted means no constraint
//   - maxResources: minimum (strictest) ceiling per resource
func MergeProfiles(names []string, profiles map[string]PlatformProfile) (PlatformProfile, error) {
	if len(names) == 0 {
		return PlatformProfile{}, nil
	}

	available := slices.Sorted(maps.Keys(profiles))
	merged := PlatformProfile{}
	var allowedDomainsSet bool

	for _, name := range names {
		p, ok := profiles[name]
		if !ok {
			return PlatformProfile{}, &ResolutionError{
				Code: ErrCodeProfileNotFound,
				Message: fmt.Sprintf(
					"profile %q is not defined in the platform file (available: %s)",
					name, joinStrings(available),
				),
			}
		}
		merged.NodeSelector = mergeStringMap(merged.NodeSelector, p.NodeSelector)
		merged.PodLabels = mergeStringMap(merged.PodLabels, p.PodLabels)
		merged.PodAnnotations = mergeStringMap(merged.PodAnnotations, p.PodAnnotations)
		merged.SecurityContext = mergePodSecurityContext(merged.SecurityContext, p.SecurityContext)
		merged.ContainerSecurityContext = mergeContainerSecurityContext(merged.ContainerSecurityContext, p.ContainerSecurityContext)
		merged.Tolerations = mergeTolerations(merged.Tolerations, p.Tolerations)
		if p.StorageClass != "" {
			merged.StorageClass = p.StorageClass
		}
		if p.AllowedDomains != nil {
			if !allowedDomainsSet {
				merged.AllowedDomains = slices.Clone(p.AllowedDomains)
				allowedDomainsSet = true
			} else {
				merged.AllowedDomains = intersectStrings(merged.AllowedDomains, p.AllowedDomains)
			}
		}
		merged.MaxResources = mergeMaxResources(merged.MaxResources, p.MaxResources)
	}

	return merged, nil
}

// ValidateProfileAgainstComponent checks domain, storage class, and resource
// ceiling constraints from the merged profile against the component and
// target environment.
func ValidateProfileAgainstComponent(
	compName string,
	comp Component,
	merged PlatformProfile,
	platformEnv *PlatformEnvironment,
	domainKey string,
) error {
	// Non-nil AllowedDomains (including empty) means the profile constrains
	// domains; nil means unconstrained.
	if merged.AllowedDomains != nil && domainKey != "" {
		if !slices.Contains(merged.AllowedDomains, domainKey) {
			return &ResolutionError{
				Code: ErrCodeProfileDomainNotAllowed,
				Message: fmt.Sprintf(
					"component %q expose.domain %q is not allowed by its profiles (allowed: %s)",
					compName, domainKey, joinStrings(merged.AllowedDomains),
				),
			}
		}
	}

	if merged.StorageClass != "" {
		if platformEnv == nil || platformEnv.StorageClasses == nil {
			return &ResolutionError{
				Code: ErrCodeProfileStorageClassNotFound,
				Message: fmt.Sprintf(
					"component %q profile references storageClass %q but the environment has no storageClasses",
					compName, merged.StorageClass,
				),
			}
		}
		if _, ok := platformEnv.StorageClasses[merged.StorageClass]; !ok {
			available := slices.Sorted(maps.Keys(platformEnv.StorageClasses))
			return &ResolutionError{
				Code: ErrCodeProfileStorageClassNotFound,
				Message: fmt.Sprintf(
					"component %q profile references storageClass %q but environment does not define it (available: %s)",
					compName, merged.StorageClass, joinStrings(available),
				),
			}
		}
	}

	if merged.MaxResources != nil {
		if err := checkResourceCeiling(compName, comp, merged.MaxResources); err != nil {
			return err
		}
	}

	return nil
}

func checkResourceCeiling(compName string, comp Component, max *ProfileMaxResources) error {
	// Compare against effective requests (explicit resources, named preset, or
	// the default small preset) so validate/resolve match deploy after Load.
	req := effectiveResourceRequests(comp)
	if quantitySet(max.CPU) && quantitySet(req.CPU) {
		if req.CPU.Cmp(*max.CPU) > 0 {
			return &ResolutionError{
				Code: ErrCodeProfileResourceExceeded,
				Message: fmt.Sprintf(
					"component %q CPU request %s exceeds profile maxResources.cpu %s",
					compName, req.CPU.String(), max.CPU.String(),
				),
			}
		}
	}
	if quantitySet(max.Memory) && quantitySet(req.Memory) {
		if req.Memory.Cmp(*max.Memory) > 0 {
			return &ResolutionError{
				Code: ErrCodeProfileResourceExceeded,
				Message: fmt.Sprintf(
					"component %q memory request %s exceeds profile maxResources.memory %s",
					compName, req.Memory.String(), max.Memory.String(),
				),
			}
		}
	}
	return nil
}

// effectiveResourceRequests returns the resource requests deploy would apply:
// explicit resources when set, otherwise the named or default small preset.
func effectiveResourceRequests(comp Component) Resources {
	if comp.Resources.ResourcesSet() {
		return comp.Resources
	}
	preset := comp.ResourcePreset
	if preset == "" {
		preset = ResourcePresetSmall
	}
	mapping, ok := ResourcePresetMappings[preset]
	if !ok {
		return Resources{}
	}
	return mapping["requests"]
}

func mergeStringMap(base, overlay map[string]string) map[string]string {
	if len(overlay) == 0 {
		return base
	}
	out := maps.Clone(base)
	if out == nil {
		out = make(map[string]string, len(overlay))
	}
	maps.Copy(out, overlay)
	return out
}

func mergePodSecurityContext(base, overlay *corev1.PodSecurityContext) *corev1.PodSecurityContext {
	if overlay == nil {
		return base
	}
	if base == nil {
		return overlay.DeepCopy()
	}
	out := base.DeepCopy()
	overlayExplicitFields(out, overlay)
	return out
}

func mergeContainerSecurityContext(base, overlay *corev1.SecurityContext) *corev1.SecurityContext {
	if overlay == nil {
		return base
	}
	if base == nil {
		return overlay.DeepCopy()
	}
	out := base.DeepCopy()
	overlayExplicitFields(out, overlay)
	return out
}

// overlayExplicitFields copies non-nil pointers and non-empty slices from src
// onto dst. Unlike mergo.WithOverride, a pointer to false wins over true.
func overlayExplicitFields(dst, src any) {
	dv := reflect.ValueOf(dst)
	sv := reflect.ValueOf(src)
	if dv.Kind() != reflect.Pointer || sv.Kind() != reflect.Pointer || dv.IsNil() || sv.IsNil() {
		return
	}
	dv = dv.Elem()
	sv = sv.Elem()
	if dv.Kind() != reflect.Struct || sv.Type() != dv.Type() {
		return
	}
	for i := range dv.NumField() {
		df := dv.Field(i)
		sf := sv.Field(i)
		if !df.CanSet() {
			continue
		}
		switch sf.Kind() {
		case reflect.Pointer:
			if !sf.IsNil() {
				df.Set(sf)
			}
		case reflect.Slice:
			if sf.Len() > 0 {
				df.Set(sf)
			}
		}
	}
}

func mergeTolerations(base, overlay []corev1.Toleration) []corev1.Toleration {
	if len(overlay) == 0 {
		return base
	}
	out := slices.Clone(base)
	for _, t := range overlay {
		if !containsToleration(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func containsToleration(list []corev1.Toleration, t corev1.Toleration) bool {
	for _, existing := range list {
		if equality.Semantic.DeepEqual(existing, t) {
			return true
		}
	}
	return false
}

func intersectStrings(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	// Always return a non-nil slice so an empty intersection stays distinct
	// from "omitted / unconstrained" (nil).
	out := make([]string, 0, min(len(a), len(b)))
	for _, s := range a {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}

func mergeMaxResources(base, overlay *ProfileMaxResources) *ProfileMaxResources {
	if overlay == nil {
		return base
	}
	if base == nil {
		return &ProfileMaxResources{
			CPU:    cloneQuantity(overlay.CPU),
			Memory: cloneQuantity(overlay.Memory),
		}
	}
	out := &ProfileMaxResources{
		CPU:    minQuantity(base.CPU, overlay.CPU),
		Memory: minQuantity(base.Memory, overlay.Memory),
	}
	if !quantitySet(out.CPU) && !quantitySet(out.Memory) {
		return nil
	}
	return out
}

func cloneQuantity(q *resource.Quantity) *resource.Quantity {
	if q == nil {
		return nil
	}
	return new(q.DeepCopy())
}

func minQuantity(a, b *resource.Quantity) *resource.Quantity {
	switch {
	case !quantitySet(a):
		return cloneQuantity(b)
	case !quantitySet(b):
		return cloneQuantity(a)
	case a.Cmp(*b) <= 0:
		return cloneQuantity(a)
	default:
		return cloneQuantity(b)
	}
}
