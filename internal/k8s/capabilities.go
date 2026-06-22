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
	"context"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
)

// APIRequirement describes a cluster API group/version that a spec feature
// needs. At least one of the GroupVersions must be present on the cluster.
type APIRequirement struct {
	// GroupVersions holds the acceptable group/version strings (e.g.
	// ["autoscaling/v2", "autoscaling/v2beta2"]). Any single match satisfies
	// the requirement.
	GroupVersions []string
	// Reason is a human-readable explanation shown when the requirement is
	// not met, e.g. `required by component "web" (autoscaling enabled)`.
	Reason string
}

// CheckAPIRequirements probes the cluster's available API groups and returns
// an error listing every requirement that is not satisfied. Returns nil when
// all requirements are met.
//
// The check calls ServerGroups once and builds an in-memory set of available
// group/version strings, so it incurs a single network round-trip regardless
// of how many requirements are checked.
func CheckAPIRequirements(ctx context.Context, client kubernetes.Interface, reqs []APIRequirement) error {
	if len(reqs) == 0 {
		return nil
	}

	groups, err := client.Discovery().ServerGroups()
	if err != nil {
		return fmt.Errorf("discover cluster API groups: %w", err)
	}

	available := make(map[string]struct{})
	for _, g := range groups.Groups {
		for _, v := range g.Versions {
			available[v.GroupVersion] = struct{}{}
		}
	}

	var missing []string
	for _, req := range reqs {
		found := false
		for _, gv := range req.GroupVersions {
			if _, ok := available[gv]; ok {
				found = true
				break
			}
		}
		if !found {
			label := strings.Join(req.GroupVersions, " or ")
			missing = append(missing, fmt.Sprintf("  - %s (%s)", label, req.Reason))
		}
	}

	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("cluster does not support required APIs:\n%s", strings.Join(missing, "\n"))
}
