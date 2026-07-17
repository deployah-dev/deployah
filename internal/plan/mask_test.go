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
	"github.com/stretchr/testify/require"
)

const secretV1 = `
apiVersion: v1
kind: Secret
metadata:
  name: web-secret
  namespace: default
type: Opaque
stringData:
  password: old-password
data:
  token: b2xkLXRva2Vu
`

const secretV2 = `
apiVersion: v1
kind: Secret
metadata:
  name: web-secret
  namespace: default
type: Opaque
stringData:
  password: new-password
data:
  token: bmV3LXRva2Vu
`

// TestApplyMasking_MasksSecretDataAndStringData covers the named case.
func TestApplyMasking_MasksSecretDataAndStringData(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(secretV1, secretV2)
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	require.Len(t, p.Changes[0].Fields, 2)

	// Before masking, values are visible (a renderer honoring --show-secrets
	// still needs them).
	for _, f := range p.Changes[0].Fields {
		assert.False(t, f.Masked)
		assert.NotEmpty(t, f.Old)
		assert.NotEmpty(t, f.New)
	}

	ApplyMasking(p)

	for _, f := range p.Changes[0].Fields {
		assert.True(t, f.Masked, "path %s should be masked", f.Path)
		// Masking flags the field; it does not destroy the values, so a
		// text renderer can still honor --show-secrets.
		assert.NotEmpty(t, f.Old)
		assert.NotEmpty(t, f.New)
	}
}

// TestApplyMasking_NonSecretResourceUntouched covers the named case.
func TestApplyMasking_NonSecretResourceUntouched(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV2)
	require.NoError(t, err)

	ApplyMasking(p)

	require.Len(t, p.Changes, 1)
	for _, f := range p.Changes[0].Fields {
		assert.False(t, f.Masked)
	}
}

// TestIsSecretDataPath covers Secret data/stringData path matching.
func TestIsSecretDataPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{path: "data", want: true},
		{path: "data.token", want: true},
		{path: "stringData", want: true},
		{path: "stringData.API_KEY", want: true},
		{path: "type", want: false},
		{path: "metadata.name", want: false},
		// must not match a "data"-prefixed key that isn't actually data.*
		{path: "databases", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isSecretDataPath(tt.path))
		})
	}
}
