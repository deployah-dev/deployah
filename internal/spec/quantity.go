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
	"k8s.io/apimachinery/pkg/api/resource"
)

// MustQuantity parses s as a Kubernetes quantity pointer. Panics on invalid
// input; use only for compile-time constants such as resource presets.
func MustQuantity(s string) *resource.Quantity {
	return new(resource.MustParse(s))
}

// quantitySet reports whether q is a non-nil, non-zero quantity.
func quantitySet(q *resource.Quantity) bool {
	return q != nil && !q.IsZero()
}

// ResourcesSet reports whether r has any non-zero resource request.
func (r Resources) ResourcesSet() bool {
	return quantitySet(r.CPU) || quantitySet(r.Memory) || quantitySet(r.EphemeralStorage)
}

// ResourcesPresent reports whether any resource field pointer is non-nil
// (including an explicit zero quantity).
func (r Resources) ResourcesPresent() bool {
	return r.CPU != nil || r.Memory != nil || r.EphemeralStorage != nil
}
