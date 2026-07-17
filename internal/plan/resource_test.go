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

const splitResourcesManifest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
  namespace: default
data:
  key: value
`

// TestSplitResources_OneEntryPerResource verifies each "---"-separated
// document becomes its own [ResourceYAML], labeled and re-encoded correctly.
func TestSplitResources_OneEntryPerResource(t *testing.T) {
	t.Parallel()
	resources, err := SplitResources(splitResourcesManifest)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	assert.Equal(t, "Deployment/default/web", resources[0].Label)
	assert.Contains(t, resources[0].YAML, "kind: Deployment")
	assert.Contains(t, resources[0].YAML, "replicas: 2")
	assert.NotContains(t, resources[0].YAML, "---", "each entry must be a standalone document")

	assert.Equal(t, "ConfigMap/default/web-config", resources[1].Label)
	assert.Contains(t, resources[1].YAML, "kind: ConfigMap")
}

// TestSplitResources_EmptyManifest covers the named case.
func TestSplitResources_EmptyManifest(t *testing.T) {
	t.Parallel()
	resources, err := SplitResources("")
	require.NoError(t, err)
	assert.Empty(t, resources)
}

// TestSplitResources_InvalidManifest covers the named case.
func TestSplitResources_InvalidManifest(t *testing.T) {
	t.Parallel()
	_, err := SplitResources("kind: Pod\nmetadata: {}\n")
	require.Error(t, err)
}
