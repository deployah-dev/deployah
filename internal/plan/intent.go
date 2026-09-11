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

import "deployah.dev/deployah/internal/extras"

// DeploymentIntent is the mutation and executability flags a deploy would
// use. Presentation flags such as output format are not part of intent.
// The zero value is not a valid intent: CRDs is empty, which extras
// rejects. Callers should use [DefaultDeploymentIntent].
type DeploymentIntent struct {
	// CRDs is how missing or existing CRDs are applied.
	CRDs extras.Policy
	// ResizeVolumes enables persistent volume claim expansion.
	ResizeVolumes bool
	// Reapply forces Helm to run even when the rendered spec is unchanged.
	Reapply bool
	// ForceHostnameChange allows a hostname change that would otherwise be
	// blocked.
	ForceHostnameChange bool
}

// DefaultDeploymentIntent returns the deploy defaults: create-only CRDs
// and the boolean flags unset.
func DefaultDeploymentIntent() DeploymentIntent {
	return DeploymentIntent{CRDs: extras.PolicyCreate}
}
