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

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"deployah.dev/deployah/internal/extras"
)

func TestDefaultDeploymentIntent(t *testing.T) {
	t.Parallel()

	got := DefaultDeploymentIntent()
	assert.Equal(t, extras.PolicyCreate, got.CRDs)
	assert.False(t, got.ResizeVolumes)
	assert.False(t, got.Reapply)
	assert.False(t, got.ForceHostnameChange)
}

func TestDeploymentIntent_ZeroValueCRDsEmpty(t *testing.T) {
	t.Parallel()

	var zero DeploymentIntent
	assert.Equal(t, extras.Policy(""), zero.CRDs)
	assert.NotEqual(t, extras.PolicyCreate, zero.CRDs)
}

// deploymentIntentFields is the allowed field set of [DeploymentIntent].
// Converting DeploymentIntent to this type fails to compile if a
// presentation field is added or CRDs is not extras.Policy.
type deploymentIntentFields struct {
	CRDs                extras.Policy
	ResizeVolumes       bool
	Reapply             bool
	ForceHostnameChange bool
}

var _ = deploymentIntentFields(DeploymentIntent{})

func TestDeploymentIntent_UsesExtrasPolicy(t *testing.T) {
	t.Parallel()

	got := deploymentIntentFields(DefaultDeploymentIntent())
	assert.Equal(t, extras.PolicyCreate, got.CRDs)
	assert.False(t, got.ResizeVolumes)
	assert.False(t, got.Reapply)
	assert.False(t, got.ForceHostnameChange)
}
