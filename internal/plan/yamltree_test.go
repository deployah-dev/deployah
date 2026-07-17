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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildYAMLTree_SiblingsShareParent verifies fields that share a
// common parent path (e.g. several changed resources.requests keys) nest
// under one shared node instead of each repeating the full parent chain.
func TestBuildYAMLTree_SiblingsShareParent(t *testing.T) {
	t.Parallel()
	fields := []FieldDiff{
		{
			Segments:   []PathSegment{{Name: "resources", Idx: -1}, {Name: "requests", Idx: -1}, {Name: "cpu", Idx: -1}},
			ChangeKind: FieldChanged, Old: "500m", New: "200m",
		},
		{
			Segments:   []PathSegment{{Name: "resources", Idx: -1}, {Name: "requests", Idx: -1}, {Name: "memory", Idx: -1}},
			ChangeKind: FieldChanged, Old: "1Gi", New: "512Mi",
		},
	}

	root := buildYAMLTree(fields)
	require.Len(t, root.order, 1, "both fields share the same top-level key")
	resources := root.children["resources"]
	require.NotNil(t, resources)
	require.Len(t, resources.order, 1)
	requests := resources.children["requests"]
	require.NotNil(t, requests)
	assert.ElementsMatch(t, []string{"cpu", "memory"}, requests.order)
	assert.Equal(t, "500m", requests.children["cpu"].field.Old)
	assert.Equal(t, "1Gi", requests.children["memory"].field.Old)
}

// TestBuildYAMLTree_NamedListEntryMarksListKey verifies a segment with
// ListKey set (a named-entry list item, e.g. a container matched by
// "name") marks the resulting node so the renderer knows to print it as
// "- name: <value>" rather than "<value>:".
func TestBuildYAMLTree_NamedListEntryMarksListKey(t *testing.T) {
	t.Parallel()
	fields := []FieldDiff{
		{
			Segments: []PathSegment{
				{Name: "containers", Idx: -1},
				{Name: "web", ListKey: "name", Idx: -1},
				{Name: "image", Idx: -1},
			},
			ChangeKind: FieldChanged, Old: "a:v1", New: "a:v2",
		},
	}

	root := buildYAMLTree(fields)
	containers := root.children["containers"]
	require.NotNil(t, containers)
	web := containers.children["web"]
	require.NotNil(t, web)
	assert.Equal(t, "name", web.listKey)
	assert.NotNil(t, web.children["image"].field)
}

// TestFieldSegments_FallsBackToDotPathWithoutCollision verifies two fields
// with no Segments (e.g. a hand-built Change, bypassing ComputeDiff) still
// both appear in the tree instead of colliding into a single node -- the
// bug the dot-path fallback in fieldSegments exists to prevent.
func TestFieldSegments_FallsBackToDotPathWithoutCollision(t *testing.T) {
	t.Parallel()
	fields := []FieldDiff{
		{Path: "spec.replicas", ChangeKind: FieldChanged, Old: "2", New: "3"},
		{Path: "spec.paused", ChangeKind: FieldChanged, Old: "true", New: "false"},
	}

	var buf strings.Builder
	require.NoError(t, writeYAMLTree(&buf, fields, TextOptions{}, yamlLeafStyleChange))

	got := buf.String()
	assert.Contains(t, got, "replicas: 2 -> 3")
	assert.Contains(t, got, "paused: true -> false")
}

// TestFieldSegments_PopulatedSegmentsTakePriority verifies fieldSegments
// prefers a field's real Segments over re-deriving from Path when both
// are present (Segments always wins; Path is only a fallback source).
func TestFieldSegments_PopulatedSegmentsTakePriority(t *testing.T) {
	t.Parallel()
	f := &FieldDiff{
		Path:     "a.b.c",
		Segments: []PathSegment{{Name: "x", Idx: -1}},
	}
	segments := fieldSegments(f)
	require.Len(t, segments, 1)
	assert.Equal(t, "x", segments[0].Name)
}
