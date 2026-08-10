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

package k8s

import (
	"fmt"
	"strings"

	"deployah.dev/deployah/internal/spec"
)

// RequiredAPIs derives the Kubernetes API group/version requirements for
// components deployed in the target environment, including cert-manager
// (TLS mode) via the resolved spec and prometheus-operator when metrics are
// enabled.
func RequiredAPIs(manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) []APIRequirement {
	type entry struct {
		groupVersions []string
		components    []string
	}

	entries := make(map[string]*entry) // keyed by canonical group/version string

	add := func(groupVersions []string, componentName string) {
		key := strings.Join(groupVersions, "|")
		e := entries[key]
		if e == nil {
			e = &entry{groupVersions: groupVersions}
			entries[key] = e
		}
		e.components = append(e.components, fmt.Sprintf("%q", componentName))
	}

	for name, component := range manifest.Components {
		// Same matcher as spec.Resolve and chart generation, so wildcard
		// deploys agree on the active component set.
		if len(component.Environments) > 0 {
			if _, ok := spec.MatchEnvKey(environment, component.Environments); !ok {
				continue
			}
		}
		if component.Autoscaling != nil && component.Autoscaling.Enabled {
			add([]string{"autoscaling/v2", "autoscaling/v2beta2"}, name)
		}
		if component.Expose != nil {
			add([]string{"networking.k8s.io/v1"}, name)
		}
		if component.Metrics.IsEnabled() {
			add([]string{"monitoring.coreos.com/v1"}, name)
		}
		if resolved != nil {
			if rc, ok := resolved.Components[name]; ok && rc.TLSMode == spec.TLSModeCertManager {
				add([]string{"cert-manager.io/v1"}, name)
			}
		}
	}

	reqs := make([]APIRequirement, 0, len(entries))
	for _, e := range entries {
		noun := "component"
		if len(e.components) > 1 {
			noun = "components"
		}
		reqs = append(reqs, APIRequirement{
			GroupVersions: e.groupVersions,
			Reason:        fmt.Sprintf("required by %s %s", noun, strings.Join(e.components, ", ")),
		})
	}
	return reqs
}
