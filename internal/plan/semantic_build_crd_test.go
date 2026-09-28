// Copyright 2026 The Deployah Authors
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

package plan_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
)

func TestBuildSemanticPlan_CRDFilesDoNotProduceResourceChanges(t *testing.T) {
	t.Parallel()
	crd := extras.RawFile{Path: "widgets.yaml", Raw: []byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\n")}
	rawCopy := append([]byte(nil), crd.Raw...)
	client := installClient(configMapYAML("app", "prod", "v1"))
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{crd}
	in.CRDDocs = []extras.CRDDoc{{
		Path: "/abs/.deployah/crds/widgets.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
	}}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, rawCopy, in.CRDs[0].Raw)
	assert.Equal(t, crd.Raw, client.gotCRDs[0].Raw)
	for _, c := range p.Changes {
		assert.NotEqual(t, "CustomResourceDefinition", c.Resource.Kind)
	}
	require.Len(t, p.ChartCRDs, 1)
	assert.Equal(t, semantic.ChartCRDProcess, p.ChartCRDs[0].Lifecycle)
	assert.True(t, p.ChartCRDs[0].WillProcess)
	assert.Equal(t, "widgets.example.com", p.ChartCRDs[0].Name)
}

func TestBuildSemanticPlan_ChartCRDLifecycle(t *testing.T) {
	t.Parallel()
	docs := []extras.CRDDoc{{
		Path: "/abs/.deployah/crds/widget.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
	}}
	tests := []struct {
		name        string
		op          helm.Operation
		skip        bool
		wantLife    semantic.ChartCRDLifecycle
		wantProcess bool
		wantAction  semantic.HelmAction
		wantChanges int
		wantEffects bool
		wantNoOp    bool
	}{
		{name: "fresh install", op: helm.OperationInstall, wantLife: semantic.ChartCRDProcess, wantProcess: true, wantAction: semantic.HelmInstall, wantChanges: 1, wantEffects: true},
		{name: "fresh skip", op: helm.OperationInstall, skip: true, wantLife: semantic.ChartCRDSkip, wantAction: semantic.HelmInstall, wantChanges: 1, wantEffects: true},
		{name: "upgrade", op: helm.OperationUpgrade, wantLife: semantic.ChartCRDUpgrade, wantAction: semantic.HelmNone, wantNoOp: true},
		{name: "upgrade ignores skip", op: helm.OperationUpgrade, skip: true, wantLife: semantic.ChartCRDUpgrade, wantAction: semantic.HelmNone, wantNoOp: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			manifest := configMapYAML("app", "prod", "same")
			var client *fakeBuildClient
			switch tc.op {
			case helm.OperationInstall:
				client = installClient(configMapYAML("app", "prod", "v1"))
			case helm.OperationUpgrade:
				client = upgradeClient(manifest, manifest, 4)
			}
			in := buildInput("ctx", resolvedSpec(), nil)
			in.CRDDocs = docs
			in.SkipCRDs = tc.skip
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), in)
			t.Cleanup(cleanup)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAction, p.HelmAction)
			require.Len(t, p.ChartCRDs, 1)
			assert.Equal(t, ".deployah/crds/widget.yaml", p.ChartCRDs[0].Source)
			assert.Equal(t, "CustomResourceDefinition", p.ChartCRDs[0].Kind)
			assert.Equal(t, "widgets.example.com", p.ChartCRDs[0].Name)
			assert.Equal(t, tc.wantLife, p.ChartCRDs[0].Lifecycle)
			assert.Equal(t, tc.wantProcess, p.ChartCRDs[0].WillProcess)
			assert.Len(t, p.Changes, tc.wantChanges)
			assert.Equal(t, tc.wantEffects, p.HasEffects())
			assert.Equal(t, tc.wantNoOp, p.IsNoOp())
			for _, c := range p.Changes {
				assert.NotEqual(t, "CustomResourceDefinition", c.Resource.Kind)
			}
		})
	}
}

func TestBuildSemanticPlan_OmittedLiveCRDIsNotDeleted(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	p := mustBuild(t, upgradeClient(manifest, manifest, 4), newMapper())
	assert.Empty(t, p.Changes)
	for _, c := range p.Changes {
		assert.NotEqual(t, "CustomResourceDefinition", c.Resource.Kind)
	}
}
