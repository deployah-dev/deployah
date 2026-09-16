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

// stampHelmApplyOrder sets [semantic.ResourceChange.ApplyOrder] from Helm's
// exported kind ordering on OriginHelm changes only. Create, update, and
// replace use InstallOrder. Deletes use UninstallOrder and sort after
// applies. Identity remains the [semantic.New] tie-break.
func stampHelmApplyOrder(changes []semantic.ResourceChange) error {
	if len(changes) == 0 {
		return nil
	}
	applyFiles := make(map[string]string, len(changes))
	deleteFiles := make(map[string]string, len(changes))
	applyIdx := make(map[string]int, len(changes))
	deleteIdx := make(map[string]int, len(changes))
	for i, c := range changes {
		if c.Origin.Kind != semantic.OriginHelm {
			continue
		}
		obj := snapshotMap(c.After)
		if c.Action == semantic.Delete {
			obj = snapshotMap(c.Before)
		}
		raw, err := yaml.Marshal(obj)
		if err != nil {
			return fmt.Errorf("encode %s for helm order: %w", c.Resource, err)
		}
		name := fmt.Sprintf("%010d.yaml", i)
		if c.Action == semantic.Delete {
			deleteFiles[name] = string(raw)
			deleteIdx[name] = i
			continue
		}
		applyFiles[name] = string(raw)
		applyIdx[name] = i
	}

	order := 1
	stamped := make(map[int]struct{}, len(changes))
	n, err := stampSorted(applyFiles, applyIdx, util.InstallOrder, changes, stamped, order)
	if err != nil {
		return err
	}
	order = n
	n, err = stampSorted(deleteFiles, deleteIdx, util.UninstallOrder, changes, stamped, order)
	if err != nil {
		return err
	}
	order = n
	for i := range changes {
		if _, ok := stamped[i]; ok {
			continue
		}
		if changes[i].Origin.Kind != semantic.OriginHelm {
			continue
		}
		changes[i].ApplyOrder = order
		order++
	}
	return nil
}

func stampSorted(files map[string]string, idx map[string]int, ordering util.KindSortOrder, changes []semantic.ResourceChange, stamped map[int]struct{}, start int) (int, error) {
	if len(files) == 0 {
		return start, nil
	}
	_, manifests, err := util.SortManifests(files, nil, ordering)
	if err != nil {
		return start, fmt.Errorf("sort helm manifests: %w", err)
	}
	order := start
	for _, m := range manifests {
		i, ok := idx[m.Name]
		if !ok {
			continue
		}
		changes[i].ApplyOrder = order
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
