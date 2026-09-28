// Copyright 2026 The Deployah Authors
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

package plan

import (
	"fmt"

	"helm.sh/helm/v4/pkg/release/v1/util"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/plan/semantic"
)

// stampHelmOrder sets HelmOrder from Helm's deploy and upgrade sequence.
// Create and Update follow install kind order, and the same kind keeps
// input order. helm.sh/hook objects are not in that order. They take
// the next ranks, in slice order. Deletes come after, in Previous
// order, matching originals.Difference(targets), not uninstall order.
// The number does not say how Helm writes or deletes.
func stampHelmOrder(changes []semantic.ResourceChange) error {
	if len(changes) == 0 {
		return nil
	}
	applyFiles := make(map[string]string, len(changes))
	applyIdx := make(map[string]int, len(changes))
	deleteIdx := make([]int, 0)
	for i, c := range changes {
		if c.Action == semantic.Delete {
			deleteIdx = append(deleteIdx, i)
			continue
		}
		raw, err := yaml.Marshal(snapshotMap(c.After))
		if err != nil {
			return fmt.Errorf("encode %s for helm order: %w", c.Resource, err)
		}
		name := fmt.Sprintf("%010d.yaml", i)
		applyFiles[name] = string(raw)
		applyIdx[name] = i
	}

	order := 1
	stamped := make(map[int]struct{}, len(changes))
	n, err := stampSorted(applyFiles, applyIdx, changes, stamped, order)
	if err != nil {
		return err
	}
	order = n
	for i := range changes {
		if changes[i].Action == semantic.Delete {
			continue
		}
		if _, ok := stamped[i]; ok {
			continue
		}
		changes[i].HelmOrder = order
		order++
	}
	for _, i := range deleteIdx {
		changes[i].HelmOrder = order
		order++
	}
	return nil
}

func stampSorted(files map[string]string, idx map[string]int, changes []semantic.ResourceChange, stamped map[int]struct{}, start int) (int, error) {
	if len(files) == 0 {
		return start, nil
	}
	_, manifests, err := util.SortManifests(files, nil, util.InstallOrder)
	if err != nil {
		return start, fmt.Errorf("sort helm manifests: %w", err)
	}
	order := start
	for _, m := range manifests {
		i, ok := idx[m.Name]
		if !ok {
			continue
		}
		changes[i].HelmOrder = order
		stamped[i] = struct{}{}
		order++
	}
	return order, nil
}

func snapshotMap(s *semantic.ResourceSnapshot) map[string]any {
	if s == nil || s.Object == nil {
		return map[string]any{}
	}
	return s.Object
}
