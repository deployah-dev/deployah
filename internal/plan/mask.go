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

package plan

import "strings"

// secretMaskedTopLevelFields lists the Kubernetes Secret mapping keys that
// hold the secret's actual payload, as opposed to metadata like Type. Both
// are checked because a Secret can populate either or both, depending on
// how the value was declared in the chart.
var secretMaskedTopLevelFields = []string{"data", "stringData"}

// ApplyMasking flags every FieldDiff belonging to a Secret resource's data
// or stringData block as Masked. It must run on every [Plan] before
// display, including --output json, since the JSON schema always masks
// secrets: see [FieldDiff].
//
// Masking is based on resource kind and field location, not on where a
// value came from during templating, so it can't be bypassed by routing a
// secret value through the chart differently.
func ApplyMasking(p *Plan) {
	if p == nil {
		return
	}
	maskSecretFields(p.Changes)
	maskSecretFields(p.Drift)
}

// maskSecretFields flags every FieldDiff under a Secret's data or
// stringData block as Masked, across both the ordinary changes and drift.
func maskSecretFields(changes []Change) {
	for i := range changes {
		c := &changes[i]
		if c.Kind != "Secret" {
			continue
		}
		for j := range c.Fields {
			if isSecretDataPath(c.Fields[j].Path) {
				c.Fields[j].Masked = true
			}
		}
	}
}

// isSecretDataPath reports whether a dyff dot-style path (as produced by
// [ComputeDiff]) falls under a Secret's data or stringData block, e.g.
// "data.password" or "stringData.API_KEY".
func isSecretDataPath(path string) bool {
	for _, prefix := range secretMaskedTopLevelFields {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}
