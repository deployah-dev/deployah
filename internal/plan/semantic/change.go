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

// ResourceChange is one declarative difference between Previous and
// Desired for a resource in the Helm release [Header] names.
type ResourceChange struct {
	Resource ResourceRef
	Action   Action
	// Before is the Previous declaration. It is unset for [Create].
	Before *ResourceSnapshot
	// After is the Desired declaration. It is unset for [Delete].
	After *ResourceSnapshot
	// Fields are the semantic differences the caller computed. [New]
	// keeps them; it does not recompute them from the snapshots.
	Fields []FieldChange
	// HelmOrder is where this change sits in Helm's deploy or upgrade
	// order. A lower number comes first. Create and Update follow
	// Helm's install kind order, and equal kinds keep manifest order.
	// Deletes come after those, in Previous release order. The planner
	// sets it. [New] sorts by HelmOrder, then by the resource
	// reference. It is not written to JSON. It does not say how Helm
	// writes or deletes.
	HelmOrder int
}
