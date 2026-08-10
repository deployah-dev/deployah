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

package deploy

import (
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
)

// loadPreviousResolvedComponents reads deployah.resolved.components from the
// last successful Helm release. An empty map means no prior release (or no
// resolved block); callers treat that as a first deploy. Never returns nil on
// success.
func loadPreviousResolvedComponents(
	c *nabat.Context,
	helmClient session.HelmClient,
	project, environment string,
) (map[string]map[string]any, error) {
	rel, _, err := plan.LastSuccessfulRelease(c, helmClient, project, environment)
	if err != nil {
		return nil, fmt.Errorf("load previous release: %w", err)
	}
	if rel == nil || rel.Chart == nil {
		return map[string]map[string]any{}, nil
	}
	return previousResolvedComponents(rel.Chart.Values), nil
}

// checkWorkloadGuards enforces hard deploy-time rules that need the previous
// release's chart values: kind flips, StatefulSet persistence add/remove, and
// persistence size decreases. prevResolved must be non-nil (use an empty map
// when there is no prior release).
func checkWorkloadGuards(
	manifest *spec.Spec,
	environment string,
	prevResolved map[string]map[string]any,
) error {
	var errors []string
	for name, component := range manifest.Components {
		if !componentActiveInEnv(component, environment) {
			continue
		}
		prev, ok := prevResolved[name]
		if !ok {
			continue
		}

		wantKind := "Deployment"
		if component.Kind == spec.ComponentKindStateful {
			wantKind = "StatefulSet"
		}
		prevKind, hasKind := prev["workloadKind"].(string)
		if hasKind && prevKind != "" && prevKind != wantKind {
			errors = append(errors, fmt.Sprintf(
				"  %s: kind change %s -> %s is not supported; delete the release and redeploy",
				name, prevKind, wantKind,
			))
		}

		wantRole := string(component.Role)
		if wantRole == "" {
			wantRole = string(spec.ComponentRoleService)
		}
		// Missing or empty previous role means service (the only role before
		// workers existed). Always compare so service -> worker upgrades are
		// rejected even when the prior release omitted role from resolved values.
		prevRole, hasRole := prev["role"].(string)
		if !hasRole || prevRole == "" {
			prevRole = string(spec.ComponentRoleService)
		}
		if prevRole != wantRole {
			errors = append(errors, fmt.Sprintf(
				"  %s: role change %s -> %s is not supported; delete the release and redeploy",
				name, prevRole, wantRole,
			))
		}

		prevSize, hasPrevSize := prev["persistenceSize"].(string)
		prevHadPersistence := hasPrevSize && prevSize != ""
		nowHasPersistence := component.Persistence != nil
		// volumeClaimTemplates are immutable: adding or removing persistence
		// on a StatefulSet (past or present) requires delete + redeploy.
		wasOrWillBeStateful := wantKind == "StatefulSet" || prevKind == "StatefulSet"
		if wasOrWillBeStateful && prevHadPersistence != nowHasPersistence {
			switch {
			case !prevHadPersistence && nowHasPersistence:
				errors = append(errors, fmt.Sprintf(
					"  %s: adding persistence to an existing StatefulSet is not supported; delete the release and redeploy",
					name,
				))
			case prevHadPersistence && !nowHasPersistence:
				errors = append(errors, fmt.Sprintf(
					"  %s: removing persistence from an existing StatefulSet is not supported; delete the release and redeploy",
					name,
				))
			}
		}

		if component.Persistence == nil || prevSize == "" {
			continue
		}
		decreased, cmpErr := persistenceSizeDecreased(prevSize, component.Persistence.Size)
		if cmpErr != nil {
			errors = append(errors, fmt.Sprintf("  %s: %v", name, cmpErr))
			continue
		}
		if decreased {
			errors = append(errors, fmt.Sprintf(
				"  %s: persistence.size decrease %s -> %s is not supported",
				name, prevSize, component.Persistence.Size,
			))
		}
	}

	if len(errors) == 0 {
		return nil
	}
	slices.Sort(errors)
	return fmt.Errorf(
		"workload change rejected for %s/%s:\n%s",
		manifest.Project, environment, strings.Join(errors, "\n"),
	)
}

// emitWorkloadWarnings prints non-fatal plan warnings for stateful/HPA,
// multi-replica expose, and mountPath changes.
func emitWorkloadWarnings(
	c *nabat.Context,
	manifest *spec.Spec,
	environment string,
	prevResolved map[string]map[string]any,
) {
	var warnings []string
	for name, component := range manifest.Components {
		if !componentActiveInEnv(component, environment) {
			continue
		}

		replicas := 1
		if component.Replicas != nil {
			replicas = *component.Replicas
		}
		if component.Autoscaling != nil && component.Autoscaling.Enabled && component.Autoscaling.MaxReplicas > replicas {
			replicas = component.Autoscaling.MaxReplicas
		}

		if component.Kind == spec.ComponentKindStateful {
			if component.Persistence != nil &&
				component.Autoscaling != nil && component.Autoscaling.Enabled {
				warnings = append(warnings, fmt.Sprintf(
					"  %s: HPA on a stateful component retains PVCs on scale-down by default (whenScaled: Retain); scaled-down volume cost remains until deleted",
					name,
				))
			}
			if component.Expose != nil && replicas > 1 {
				warnings = append(warnings, fmt.Sprintf(
					"  %s: expose with replicas > 1 load-balances across pods; only use this when every replica can serve the same traffic",
					name,
				))
			}
		}

		if component.Persistence != nil {
			if prev, ok := prevResolved[name]; ok {
				if prevMount, hasMount := prev["persistenceMountPath"].(string); hasMount &&
					prevMount != "" && prevMount != component.Persistence.MountPath {
					warnings = append(warnings, fmt.Sprintf(
						"  %s: persistence.mountPath change %s -> %s leaves existing data at the old path",
						name, prevMount, component.Persistence.MountPath,
					))
				}
			}
		}
	}

	if len(warnings) == 0 {
		return
	}
	slices.Sort(warnings)
	c.Warn("workload warnings:\n" + strings.Join(warnings, "\n"))
}

// hasStatefulWithPersistence reports whether any active stateful component
// declares persistence (triggers the Kubernetes 1.32+ RWOP/retention floor).
func hasStatefulWithPersistence(manifest *spec.Spec, environment string) bool {
	for _, component := range manifest.Components {
		if !componentActiveInEnv(component, environment) {
			continue
		}
		if component.Kind == spec.ComponentKindStateful && component.Persistence != nil {
			return true
		}
	}
	return false
}

func componentActiveInEnv(component spec.Component, environment string) bool {
	if len(component.Environments) == 0 {
		return true
	}
	_, ok := spec.MatchEnvKey(environment, component.Environments)
	return ok
}

// previousResolvedComponents extracts deployah.resolved.components from chart
// values. Always returns a non-nil map (empty when the resolved block is absent).
func previousResolvedComponents(chartValues map[string]any) map[string]map[string]any {
	deployahBlock, ok := chartValues["deployah"].(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	resolvedBlock, ok := deployahBlock["resolved"].(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	componentsBlock, ok := resolvedBlock["components"].(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	out := make(map[string]map[string]any, len(componentsBlock))
	for name, raw := range componentsBlock {
		m, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		out[name] = m
	}
	return out
}

func persistenceSizeDecreased(prev, next string) (bool, error) {
	prevQ, err := resource.ParseQuantity(prev)
	if err != nil {
		return false, fmt.Errorf("parse previous persistence.size %q: %w", prev, err)
	}
	nextQ, err := resource.ParseQuantity(next)
	if err != nil {
		return false, fmt.Errorf("parse new persistence.size %q: %w", next, err)
	}
	return nextQ.Cmp(prevQ) < 0, nil
}
