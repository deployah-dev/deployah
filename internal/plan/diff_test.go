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

const deploymentV1 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.2
`

const deploymentV2 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.3
`

const configMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
  namespace: default
data:
  key: value
`

const legacySidecar = `
apiVersion: v1
kind: Service
metadata:
  name: legacy-sidecar
  namespace: default
spec:
  ports:
    - port: 80
`

const deploymentWithNoise = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
  resourceVersion: "12345"
  uid: abc-123
  generation: 3
  creationTimestamp: "2024-01-01T00:00:00Z"
  managedFields:
    - manager: kubectl
status:
  replicas: 2
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.2
`

// TestComputeDiff covers add/change/destroy, noise stripping, and sort order.
func TestComputeDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		previous       string
		current        string
		wantSummary    Summary
		wantLen        int
		wantHasChanges *bool
		check          func(t *testing.T, p *Plan)
	}{
		{
			name:        "fresh install",
			previous:    "",
			current:     deploymentV1 + "---\n" + configMap,
			wantSummary: Summary{Add: 2},
			wantLen:     2,
			check: func(t *testing.T, p *Plan) {
				t.Helper()
				for _, c := range p.Changes {
					assert.Equal(t, ActionAdd, c.Action)
					assert.Empty(t, c.Fields)
				}
			},
		},
		{
			name:        "image bump",
			previous:    deploymentV1,
			current:     deploymentV2,
			wantSummary: Summary{Change: 1},
			wantLen:     1,
			check: func(t *testing.T, p *Plan) {
				t.Helper()
				change := p.Changes[0]
				assert.Equal(t, ActionChange, change.Action)
				assert.Equal(t, "Deployment", change.Kind)
				assert.Equal(t, "web", change.Name)
				require.Len(t, change.Fields, 1)
				assert.Equal(t, "spec.template.spec.containers.web.image", change.Fields[0].Path)
				assert.Equal(t, FieldChanged, change.Fields[0].ChangeKind)
				assert.Equal(t, "myapp:v1.2", change.Fields[0].Old)
				assert.Equal(t, "myapp:v1.3", change.Fields[0].New)
			},
		},
		{
			name:        "resource added",
			previous:    deploymentV1,
			current:     deploymentV1 + "---\n" + configMap,
			wantSummary: Summary{Add: 1},
			wantLen:     1,
			check: func(t *testing.T, p *Plan) {
				t.Helper()
				assert.Equal(t, ActionAdd, p.Changes[0].Action)
				assert.Equal(t, "ConfigMap", p.Changes[0].Kind)
				assert.Equal(t, "web-config", p.Changes[0].Name)
			},
		},
		{
			name:        "resource removed",
			previous:    deploymentV1 + "---\n" + legacySidecar,
			current:     deploymentV1,
			wantSummary: Summary{Destroy: 1},
			wantLen:     1,
			check: func(t *testing.T, p *Plan) {
				t.Helper()
				assert.Equal(t, ActionDestroy, p.Changes[0].Action)
				assert.Equal(t, "Service", p.Changes[0].Kind)
				assert.Equal(t, "legacy-sidecar", p.Changes[0].Name)
			},
		},
		{
			name:           "no changes",
			previous:       deploymentV1,
			current:        deploymentV1,
			wantSummary:    Summary{},
			wantLen:        0,
			wantHasChanges: new(false),
		},
		{
			name:        "noise fields ignored",
			previous:    deploymentWithNoise,
			current:     deploymentV1,
			wantSummary: Summary{},
			wantLen:     0,
		},
		{
			name:        "mixed changes sorted by kind then name",
			previous:    deploymentV1 + "---\n" + legacySidecar,
			current:     deploymentV2 + "---\n" + configMap,
			wantSummary: Summary{Add: 1, Change: 1, Destroy: 1},
			wantLen:     3,
			check: func(t *testing.T, p *Plan) {
				t.Helper()
				assert.Equal(t, "ConfigMap", p.Changes[0].Kind)
				assert.Equal(t, ActionAdd, p.Changes[0].Action)
				assert.Equal(t, "Deployment", p.Changes[1].Kind)
				assert.Equal(t, ActionChange, p.Changes[1].Action)
				assert.Equal(t, "Service", p.Changes[2].Kind)
				assert.Equal(t, ActionDestroy, p.Changes[2].Action)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := ComputeDiff(tt.previous, tt.current)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSummary, p.Summary)
			require.Len(t, p.Changes, tt.wantLen)
			if tt.wantHasChanges != nil {
				assert.Equal(t, *tt.wantHasChanges, p.HasChanges())
			}
			if tt.check != nil {
				tt.check(t, p)
			}
		})
	}
}
