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

package predict_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/predict"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestActionString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Create", predict.ActionCreate.String())
	assert.Equal(t, "Update", predict.ActionUpdate.String())
	assert.Equal(t, "Delete", predict.ActionDelete.String())
	assert.Equal(t, "NoOp", predict.ActionNoOp.String())
	assert.Equal(t, "Action(0)", predict.Action(0).String())
}

func TestInputFromPrep(t *testing.T) {
	t.Parallel()

	desired := configMapYAML("app", "prod", "next")
	current := &v1.Release{Name: "web", Manifest: configMapYAML("app", "prod", "prev")}

	t.Run("nil prep", func(t *testing.T) {
		t.Parallel()
		_, err := predict.InputFromPrep(nil, "web", "prod", desired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "release prep is required")
	})

	t.Run("zero operation", func(t *testing.T) {
		t.Parallel()
		_, err := predict.InputFromPrep(&helm.ReleasePrep{}, "web", "prod", desired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid helm operation")
	})

	t.Run("upgrade without current", func(t *testing.T) {
		t.Parallel()
		_, err := predict.InputFromPrep(&helm.ReleasePrep{Operation: helm.OperationUpgrade}, "web", "prod", desired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "upgrade prep requires a current release")
	})

	t.Run("install with current", func(t *testing.T) {
		t.Parallel()
		_, err := predict.InputFromPrep(&helm.ReleasePrep{
			Operation: helm.OperationInstall,
			Current:   current,
		}, "web", "prod", desired)
		require.Error(t, err)
		assert.ErrorContains(t, err, "install prep must not have a current release")
	})

	t.Run("install without current", func(t *testing.T) {
		t.Parallel()
		in, err := predict.InputFromPrep(&helm.ReleasePrep{Operation: helm.OperationInstall}, "web", "prod", desired)
		require.NoError(t, err)
		assert.Equal(t, helm.OperationInstall, in.Operation)
		assert.Equal(t, "web", in.ReleaseName)
		assert.Equal(t, "prod", in.Namespace)
		assert.Empty(t, in.Previous)
		assert.Equal(t, desired, in.Desired)
	})

	t.Run("upgrade uses current manifest", func(t *testing.T) {
		t.Parallel()
		in, err := predict.InputFromPrep(&helm.ReleasePrep{
			Operation: helm.OperationUpgrade,
			Current:   current,
		}, "web", "prod", desired)
		require.NoError(t, err)
		assert.Equal(t, helm.OperationUpgrade, in.Operation)
		assert.Equal(t, current.Manifest, in.Previous)
		assert.Equal(t, desired, in.Desired)
	})
}
