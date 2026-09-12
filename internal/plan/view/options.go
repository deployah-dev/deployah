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

package view

// Options controls renderer behavior. ShowSecrets is renderer-only and
// is not a CLI flag in Stage C.
type Options struct {
	// ShowSecrets reveals Secret data and stringData values. The default
	// hides those values without hiding that a field changed.
	ShowSecrets bool
}
