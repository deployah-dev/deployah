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

package testing

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	k8sjson "k8s.io/apimachinery/pkg/util/json"
)

func TestDiffSubset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    string
		got     string
		wantErr bool
		contain string
	}{
		{
			name: "absent keys in want are ignored",
			want: `replicas: 1`,
			got: `replicas: 1
image: nginx`,
		},
		{
			name:    "typo key is reported missing",
			want:    `replica: 1`,
			got:     `replicas: 1`,
			wantErr: true,
			contain: "replica: missing",
		},
		{
			name: "null means absent",
			want: `startingDeadlineSeconds: null`,
			got:  `schedule: "@hourly"`,
		},
		{
			name:    "null fails when field is set",
			want:    `startingDeadlineSeconds: null`,
			got:     `startingDeadlineSeconds: 30`,
			wantErr: true,
			contain: "want absent/null",
		},
		{
			name: "scalar list exact match",
			want: `command: ["echo", "ok"]`,
			got:  `command: ["echo", "ok"]`,
		},
		{
			name:    "scalar list order mismatch",
			want:    `command: ["echo", "ok"]`,
			got:     `command: ["ok", "echo"]`,
			wantErr: true,
			contain: "want string(echo), got string(ok)",
		},
		{
			name:    "scalar list length mismatch",
			want:    `command: ["echo"]`,
			got:     `command: ["echo", "ok"]`,
			wantErr: true,
			contain: "want list len 1, got 2",
		},
		{
			name: "empty want slice matches any",
			want: `command: []`,
			got:  `command: ["echo", "ok"]`,
		},
		{
			name: "object list matched by name, extra got items ignored",
			want: `containers:
  - name: web
    image: nginx:latest`,
			got: `containers:
  - name: sidecar
    image: busybox
  - name: web
    image: nginx:latest
    ports:
      - containerPort: 80`,
		},
		{
			name: "object list missing name",
			want: `containers:
  - name: web
    image: nginx:latest`,
			got: `containers:
  - name: sidecar
    image: nginx:latest`,
			wantErr: true,
			contain: "no item with name=",
		},
		{
			name: "nested maps",
			want: `spec:
  replicas: 1
  template:
    spec:
      restartPolicy: Always`,
			got: `spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    spec:
      restartPolicy: Always
      containers:
        - name: web`,
		},
		{
			name: "int64 vs float64",
			want: `replicas: 1`,
			got:  `replicas: 1`,
		},
		{
			name: "nested null in spec",
			want: `spec:
  activeDeadlineSeconds: null`,
			got: `spec:
  completions: 1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := decodeYAMLMap(t, tt.want)
			got := decodeYAMLMap(t, tt.got)
			diffs := DiffSubset("$", want, got)
			if !tt.wantErr {
				assert.Empty(t, diffs)
				return
			}
			require.NotEmpty(t, diffs)
			if tt.contain != "" {
				joined := ""
				for _, d := range diffs {
					joined += d
				}
				assert.Contains(t, joined, tt.contain)
			}
		})
	}
}

func TestDiffSubset_jsonNumberEqualsInt(t *testing.T) {
	t.Parallel()

	want := map[string]any{"replicas": int64(1)}
	got := map[string]any{"replicas": json.Number("1")}
	assert.Empty(t, DiffSubset("$", want, got))
}

func TestDiffContainsByKey_typeFallback(t *testing.T) {
	t.Parallel()

	want := []any{
		map[string]any{"type": "http", "port": int64(80)},
	}
	got := []any{
		map[string]any{"type": "tcp", "port": int64(22)},
		map[string]any{"type": "http", "port": int64(80), "name": "web"},
	}
	assert.Empty(t, DiffContainsByKey("ports", want, got))
}

func decodeYAMLMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	jsonBytes, err := yaml.YAMLToJSON([]byte(raw))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, k8sjson.Unmarshal(jsonBytes, &m))
	return m
}
