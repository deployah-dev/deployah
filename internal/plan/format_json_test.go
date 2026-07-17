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
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderJSON_HeaderAndMixedChanges covers the named case.
func TestRenderJSON_HeaderAndMixedChanges(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1+"---\n"+legacySidecar, deploymentV2+"---\n"+configMap)
	require.NoError(t, err)
	p.Header = Header{
		Project:     "web",
		Environment: "production",
		Release:     "web-production",
		Namespace:   "default",
		Context:     "prod-eks-us-east-1",
		Revision:    7,
	}

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))

	assert.Equal(t, "1.0", doc["format_version"])
	assert.Equal(t, "web", doc["project"])
	assert.Equal(t, "production", doc["environment"])
	assert.Equal(t, "web-production", doc["release"])
	assert.Equal(t, "default", doc["namespace"])
	assert.Equal(t, "prod-eks-us-east-1", doc["context"])
	assert.InEpsilon(t, float64(7), doc["revision"], 0)
	assert.Equal(t, false, doc["fresh_install"])

	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok)
	assert.InEpsilon(t, float64(1), summary["add"], 0)
	assert.InEpsilon(t, float64(1), summary["change"], 0)
	assert.InEpsilon(t, float64(1), summary["destroy"], 0)

	changes, ok := doc["changes"].([]any)
	require.True(t, ok)
	require.Len(t, changes, 3)

	byKind := map[string]map[string]any{}
	for _, raw := range changes {
		c, changeOK := raw.(map[string]any)
		require.True(t, changeOK)
		kind, kindOK := c["kind"].(string)
		require.True(t, kindOK)
		byKind[kind] = c
	}

	deployment := byKind["Deployment"]
	require.NotNil(t, deployment)
	assert.Equal(t, "change", deployment["action"])
	assert.Equal(t, "apps/v1", deployment["api_version"])
	assert.Equal(t, "web", deployment["name"])
	assert.Equal(t, "default", deployment["namespace"])
	fields, ok := deployment["fields"].([]any)
	require.True(t, ok)
	require.Len(t, fields, 1)
	field, fieldOK := fields[0].(map[string]any)
	require.True(t, fieldOK)
	assert.Equal(t, "spec.template.spec.containers.web.image", field["path"])
	assert.Equal(t, "myapp:v1.2", field["old"])
	assert.Equal(t, "myapp:v1.3", field["new"])
	assert.NotContains(t, field, "masked")
	assert.NotContains(t, field, "change")

	configMapChange := byKind["ConfigMap"]
	require.NotNil(t, configMapChange)
	assert.Equal(t, "add", configMapChange["action"])
	assert.Empty(t, configMapChange["fields"])

	serviceChange := byKind["Service"]
	require.NotNil(t, serviceChange)
	assert.Equal(t, "destroy", serviceChange["action"])
	assert.Empty(t, serviceChange["fields"])
}

// TestRenderJSON_HooksChangedField verifies a hook-only change (no
// resource-level Changes at all) is still visible in the JSON document, so
// a CI consumer parsing `--output json --detailed-exitcode` output can
// explain a non-zero exit code against an otherwise-empty changes/summary.
func TestRenderJSON_HooksChangedField(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.HooksChanged = true
	p.Header = Header{Release: "web-production"}
	require.True(t, p.HasChanges(), "a hook-only change must still count as a change")

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))

	assert.Empty(t, doc["changes"], "manifest is unchanged: no resource-level changes")
	assert.Equal(t, true, doc["hooks_changed"], "hooks_changed must surface even when changes is empty")
}

// TestRenderJSON_HooksChangedOmittedWhenFalse verifies the common case (no
// hook change) keeps the field out of the document entirely (omitempty),
// matching the Warning field's convention.
func TestRenderJSON_HooksChangedOmittedWhenFalse(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.Header = Header{Release: "web-production"}

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))

	assert.NotContains(t, doc, "hooks_changed")
}

// TestRenderJSON_FreshInstallRevisionIsNull covers the named case.
func TestRenderJSON_FreshInstallRevisionIsNull(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff("", deploymentV1)
	require.NoError(t, err)
	p.Header = Header{FreshInstall: true}

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))

	assert.Nil(t, doc["revision"])
	assert.Contains(t, string(buf.String()), `"revision": null`)
	assert.Equal(t, true, doc["fresh_install"])
}

// TestRenderJSON_MaskedFieldOmitsOldAndNew covers the named case.
func TestRenderJSON_MaskedFieldOmitsOldAndNew(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(secretV1, secretV2)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	raw := buf.String()
	assert.NotContains(t, raw, "old-password")
	assert.NotContains(t, raw, "new-password")
	assert.Contains(t, raw, `"masked": true`)
	assert.Contains(t, raw, `"change": "changed"`)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	changes, changesOK := doc["changes"].([]any)
	require.True(t, changesOK)
	require.Len(t, changes, 1)
	change, changeOK := changes[0].(map[string]any)
	require.True(t, changeOK)
	fields, fieldsOK := change["fields"].([]any)
	require.True(t, fieldsOK)
	for _, fieldRaw := range fields {
		f, fieldOK := fieldRaw.(map[string]any)
		require.True(t, fieldOK)
		assert.NotContains(t, f, "old")
		assert.NotContains(t, f, "new")
	}
}

// TestRenderJSON_WarningOmittedWhenEmpty covers the named case.
func TestRenderJSON_WarningOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	assert.NotContains(t, buf.String(), "warning")
}

// TestRenderJSON_DriftOmittedWhenNotChecked verifies a plan rendered
// without --drift carries no "drift" key at all.
func TestRenderJSON_DriftOmittedWhenNotChecked(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))
	assert.NotContains(t, doc, "drift")
	assert.NotContains(t, doc, "drift_incomplete")
}

// TestRenderJSON_DriftChangesAndIncomplete verifies drift changes and
// incomplete resources both serialize, and that a masked drift field omits
// old/new the same way an ordinary masked change does.
func TestRenderJSON_DriftChangesAndIncomplete(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.DriftChecked = true
	p.Drift = []Change{
		{
			Action: ActionChange,
			Kind:   "Deployment",
			Name:   "web",
			Fields: []FieldDiff{
				{Path: "spec.replicas", ChangeKind: FieldChanged, Old: "2", New: "5"},
			},
		},
		{
			Action: ActionChange,
			Kind:   "Secret",
			Name:   "web-secret",
			Fields: []FieldDiff{
				{Path: "data.password", ChangeKind: FieldChanged, Old: "old", New: "new"},
			},
		},
	}
	p.DriftIncomplete = []string{"ConfigMap/default/web-config"}

	var buf strings.Builder
	require.NoError(t, RenderJSON(&buf, p))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))

	drift, ok := doc["drift"].([]any)
	require.True(t, ok)
	require.Len(t, drift, 2)

	byKind := map[string]map[string]any{}
	for _, raw := range drift {
		c, isMap := raw.(map[string]any)
		require.True(t, isMap)
		kind, isString := c["kind"].(string)
		require.True(t, isString)
		byKind[kind] = c
	}

	deploymentDrift := byKind["Deployment"]
	require.NotNil(t, deploymentDrift)
	deploymentFieldList, ok := deploymentDrift["fields"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, deploymentFieldList)
	fields, ok := deploymentFieldList[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2", fields["old"])
	assert.Equal(t, "5", fields["new"])

	secretDrift := byKind["Secret"]
	require.NotNil(t, secretDrift)
	secretFieldList, ok := secretDrift["fields"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, secretFieldList)
	secretFields, ok := secretFieldList[0].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, secretFields, "old", "drift under a Secret's data block must still be masked")
	assert.NotContains(t, secretFields, "new")

	incomplete, ok := doc["drift_incomplete"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"ConfigMap/default/web-config"}, incomplete)
}
