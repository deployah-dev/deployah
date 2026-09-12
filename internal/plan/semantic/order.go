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

package semantic

import (
	"cmp"
	"slices"
)

func sortChanges(changes []ResourceChange) {
	slices.SortFunc(changes, compareChange)
	for i := range changes {
		slices.SortFunc(changes[i].Fields, func(a, b FieldChange) int {
			return cmp.Compare(a.Path, b.Path)
		})
	}
}

func compareChange(a, b ResourceChange) int {
	return cmp.Or(
		cmp.Compare(a.Resource.APIVersion, b.Resource.APIVersion),
		cmp.Compare(a.Resource.Kind, b.Resource.Kind),
		cmp.Compare(a.Resource.Namespace, b.Resource.Namespace),
		cmp.Compare(a.Resource.Name, b.Resource.Name),
		cmp.Compare(a.Resource.GenerateName, b.Resource.GenerateName),
		cmp.Compare(actionRank(a.Action), actionRank(b.Action)),
	)
}

func sortDiagnostics(diags []Diagnostic) {
	slices.SortFunc(diags, func(a, b Diagnostic) int {
		return cmp.Or(
			cmp.Compare(refKey(a.Resource), refKey(b.Resource)),
			cmp.Compare(int(a.Category), int(b.Category)),
			cmp.Compare(a.Message, b.Message),
		)
	})
}

func refKey(r *ResourceRef) string {
	if r == nil {
		return ""
	}
	return r.identityKey()
}
