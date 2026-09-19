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

//go:build e2e

package e2e_test

import (
	"context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type semanticPlanClient struct {
	result *render.RenderResult
	prep   helm.ReleasePrep
}

func (c *semanticPlanClient) RenderManifestsWithPrep(
	_ context.Context,
	_ *spec.ResolvedSpec,
	_ postrenderer.PostRenderer,
	_ []extras.RawFile,
) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	return c.result, c.prep, func() {}, nil
}

func (s *E2ESuite) TestSemanticPlanPrerequisites() {
	t := s.T()
	ns := fixtureNamespace("semantic-plan-prereq")
	cluster := s.predictCluster(t)
	client := &semanticPlanClient{
		result: &render.RenderResult{
			ReleaseName: "web",
			Namespace:   ns,
			Manifest:    "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: " + ns + "\ndata:\n  k: v\n",
			Revision:    1,
		},
		prep: helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, plan.SemanticBuildInput{
		ClusterContext: kindContext,
		Resolved:       &spec.ResolvedSpec{Spec: &spec.Spec{Project: "shop"}, Env: spec.EnvIdentity{Original: "dev"}},
		CRDs:           []extras.RawFile{{Path: "widgets.yaml", Raw: []byte("not a crd\n")}},
	})
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(p.Changes), 2)
	assert.Equal(t, semantic.OriginNamespace, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, semantic.OriginHelm, p.Changes[1].Origin.Kind)
	assert.Equal(t, "ConfigMap", p.Changes[1].Resource.Kind)
	assert.Equal(t, semantic.Create, p.Changes[1].Action)
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
	require.NotEmpty(t, p.Diagnostics)

	cs, _ := s.kubeClients(t)
	_, err = cs.CoreV1().Namespaces().Get(t.Context(), ns, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err), "namespace %s should still be absent: %v", ns, err)
}
