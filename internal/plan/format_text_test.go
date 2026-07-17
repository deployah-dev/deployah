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

// TestRenderText_HeaderAndMixedChanges covers the named case.
func TestRenderText_HeaderAndMixedChanges(t *testing.T) {
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
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	got := buf.String()
	assert.Contains(t, got, "Project:     web\n")
	assert.Contains(t, got, "Environment: production\n")
	assert.Contains(t, got, "Release:     web-production (revision 7)\n")
	assert.Contains(t, got, "Namespace:   default\n")
	assert.Contains(t, got, "Context:     prod-eks-us-east-1\n")
	assert.Contains(t, got, "~ Deployment/web\n")
	assert.Contains(t, got, "    image: myapp:v1.2 -> myapp:v1.3\n")
	assert.Contains(t, got, "+ ConfigMap/web-config\n")
	assert.Contains(t, got, "- Service/legacy-sidecar\n")
	assert.Contains(t, got, "Plan: 1 to add, 1 to change, 1 to destroy.\n")
}

// TestRenderText_FreshInstall covers the named case.
func TestRenderText_FreshInstall(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff("", deploymentV1)
	require.NoError(t, err)
	p.Header = Header{Release: "web-production", FreshInstall: true}

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	assert.Contains(t, buf.String(), "Release:     web-production (fresh install)\n")
}

// TestRenderText_NoChanges covers the named case.
func TestRenderText_NoChanges(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	assert.Contains(t, buf.String(), "No changes.")
}

// TestRenderText_Warning covers the named case.
func TestRenderText_Warning(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.Header.Warning = "latest revision 8 is failed; comparing against revision 7 instead"

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	assert.Contains(t, buf.String(), "Warning: latest revision 8 is failed")
}

// TestRenderText_RawModeShowsUnmappedPath covers the named case.
func TestRenderText_RawModeShowsUnmappedPath(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV2)
	require.NoError(t, err)

	var compact, raw strings.Builder
	require.NoError(t, RenderText(&compact, p, TextOptions{Mode: ModeCompact}))
	require.NoError(t, RenderText(&raw, p, TextOptions{Mode: ModeRaw}))

	assert.Contains(t, compact.String(), "    image: myapp:v1.2 -> myapp:v1.3\n")
	assert.Contains(t, raw.String(), "    spec.template.spec.containers.web.image: myapp:v1.2 -> myapp:v1.3\n")
	assert.NotContains(t, raw.String(), "    image:")
}

// TestRenderText_YAMLModeReconstructsManifestShape verifies ModeYAML
// rebuilds the real nested manifest structure (map keys nest, a named list
// entry like a container renders as "- name: <name>") instead of a single
// flattened dot-path line, even for a plain scalar leaf value.
func TestRenderText_YAMLModeReconstructsManifestShape(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV2)
	require.NoError(t, err)

	var compact, yaml strings.Builder
	require.NoError(t, RenderText(&compact, p, TextOptions{Mode: ModeCompact}))
	require.NoError(t, RenderText(&yaml, p, TextOptions{Mode: ModeYAML}))

	assert.Contains(t, compact.String(), "    image: myapp:v1.2 -> myapp:v1.3\n",
		"compact mode keeps the single-line form for a scalar field")

	got := yaml.String()
	assert.Contains(t, got, "    spec:\n")
	assert.Contains(t, got, "      template:\n")
	assert.Contains(t, got, "        spec:\n")
	assert.Contains(t, got, "          containers:\n")
	assert.Contains(t, got, "            - name: web\n",
		"a name-keyed list entry (the web container) must render as a real YAML list item")
	assert.Contains(t, got, "              image: myapp:v1.2 -> myapp:v1.3\n",
		"the leaf value nests under the reconstructed path instead of repeating the dot path")
	assert.NotContains(t, got, "\n    image: myapp:v1.2 -> myapp:v1.3\n",
		"ModeYAML must not fall back to the single-line compact form (anchored at start of line, unlike the deeper-indented nested leaf)")
	assert.NotContains(t, got, "spec.template.spec.containers.web.image",
		"ModeYAML must not print the flattened dot path at all")
}

// deploymentWithSidecarV1/V2 add a whole new named container (a sidecar),
// so dyff reports it as one ADDED diff at the list-item level (New holding
// the entire container as a YAML mapping), rather than a per-field diff --
// the case ModeYAML's writeYAMLValueBlock exists for.
const deploymentWithSidecarV1 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.2
`

const deploymentWithSidecarV2 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.2
        - name: sidecar
          image: sidecar:v1.0
`

// TestRenderText_YAMLModeWholeListItemAdded verifies a whole new named
// list entry (an entire container added, not a per-field change) renders
// as its own "- name: <name>" list item with its full content dumped
// underneath, instead of one flattened "spec.template...sidecar: (added)
// <yaml blob>" line.
func TestRenderText_YAMLModeWholeListItemAdded(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentWithSidecarV1, deploymentWithSidecarV2)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{Mode: ModeYAML}))

	got := buf.String()
	assert.Contains(t, got, "            - name: sidecar\n",
		"the whole new container must render as its own named list item")
	assert.Contains(t, got, "name: sidecar",
		"the added container's own content (dumped as a YAML block) must appear nested under the list item")
	assert.Contains(t, got, "image: sidecar:v1.0")
}

// TestRenderText_YAMLModeDriftUsesDriftWording verifies the drift section
// under ModeYAML reconstructs the same nested manifest shape as the
// ordinary diff, but with drift's "expected X, live Y" leaf wording
// instead of "X -> Y".
func TestRenderText_YAMLModeDriftUsesDriftWording(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.DriftChecked = true
	drift, err := ComputeDiff(deploymentV1, deploymentV2)
	require.NoError(t, err)
	p.Drift = drift.Changes

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{Mode: ModeYAML}))

	got := buf.String()
	assert.Contains(t, got, "Drift (cluster changed outside deployah):")
	assert.Contains(t, got, "            - name: web\n")
	assert.Contains(t, got, "              image: expected myapp:v1.2, live myapp:v1.3\n",
		"drift leaves must use 'expected X, live Y' wording even when nested")
}

// TestRenderText_MaskedSecretHidesValueByDefault covers the named case.
func TestRenderText_MaskedSecretHidesValueByDefault(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(secretV1, secretV2)
	require.NoError(t, err)
	ApplyMasking(p)

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	got := buf.String()
	assert.Contains(t, got, "(masked) changed")
	assert.NotContains(t, got, "old-password")
	assert.NotContains(t, got, "new-password")
}

// TestRenderText_ShowSecretsRevealsValue covers the named case.
func TestRenderText_ShowSecretsRevealsValue(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(secretV1, secretV2)
	require.NoError(t, err)
	ApplyMasking(p)

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{ShowSecrets: true}))

	assert.Contains(t, buf.String(), "old-password -> new-password")
}

// TestRenderText_NoDriftSectionWhenNotChecked verifies a plan rendered
// without --drift never grows the drift section, even if Drift happened to
// be empty (the common no-drift-requested case).
func TestRenderText_NoDriftSectionWhenNotChecked(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	assert.NotContains(t, buf.String(), "Drift")
}

// TestRenderText_DriftSection_ShowsExpectedVsLive verifies drift fields use
// "expected X, live Y" wording, distinct from the "X -> Y" wording ordinary
// changes use.
func TestRenderText_DriftSection_ShowsExpectedVsLive(t *testing.T) {
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
	}

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	got := buf.String()
	assert.Contains(t, got, "Drift (cluster changed outside deployah):")
	assert.Contains(t, got, "~ Deployment/web")
	assert.Contains(t, got, "replicas: expected 2, live 5")
	assert.Contains(t, got, "Note: deploy does not revert drift.")
}

// TestRenderText_DriftSection_NoDriftFound verifies the checked-but-clean
// case is distinguishable from "not requested": it still prints the
// section heading with an explicit "no drift" line.
func TestRenderText_DriftSection_NoDriftFound(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.DriftChecked = true

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	got := buf.String()
	assert.Contains(t, got, "Drift (cluster changed outside deployah):")
	assert.Contains(t, got, "No drift detected.")
}

// TestRenderText_DriftSection_Incomplete verifies resources drift could not
// be checked for are surfaced as a warning rather than silently dropped.
func TestRenderText_DriftSection_Incomplete(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff(deploymentV1, deploymentV1)
	require.NoError(t, err)
	p.DriftChecked = true
	p.DriftIncomplete = []string{"Secret/default/web-secret"}

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	got := buf.String()
	assert.Contains(t, got, "drift is incomplete")
	assert.Contains(t, got, "Secret/default/web-secret")
}

// TestRenderText_DriftSection_FreshInstallIsNoOp verifies --drift on a fresh
// install adds no "Drift (...)" section to stdout, even if DriftChecked ends
// up true alongside FreshInstall (the explanation lives in stderr instead).
func TestRenderText_DriftSection_FreshInstallIsNoOp(t *testing.T) {
	t.Parallel()
	p, err := ComputeDiff("", deploymentV1)
	require.NoError(t, err)
	p.Header.FreshInstall = true
	p.DriftChecked = true

	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))

	got := buf.String()
	assert.NotContains(t, got, "Drift (cluster changed outside deployah):")
	assert.NotContains(t, got, "no-op on a fresh install")
	assert.NotContains(t, got, "No drift detected.")
}

// TestMapCompactPath covers the named case.
func TestMapCompactPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		display string
		ok      bool
	}{
		{"spec.replicas", "replicas", true},
		{"spec.template.spec.containers.web.image", "image", true},
		{"spec.template.spec.containers.web.ports.http.containerPort", "port", true},
		{"spec.template.spec.containers.web.env.NODE_ENV.value", "env.NODE_ENV", true},
		{"metadata.labels.foo", "", false},
	}
	for _, tt := range tests {
		display, ok := mapCompactPath(tt.path)
		assert.Equal(t, tt.ok, ok, "path %s", tt.path)
		assert.Equal(t, tt.display, display, "path %s", tt.path)
	}
}
