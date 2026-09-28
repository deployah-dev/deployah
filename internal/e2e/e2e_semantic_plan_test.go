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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	corev1 "k8s.io/api/core/v1"
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

func (s *E2ESuite) TestSemanticPlanDoesNotMutateOrReadLive() {
	t := s.T()
	resolved := &spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "shop"},
		Env:  spec.EnvIdentity{Original: "dev"},
	}

	t.Run("fresh install missing namespace", func(t *testing.T) {
		ns := fixtureNamespace("semantic-plan-install")
		manifest := configMapManifest(ns, "app", "v")
		client := &semanticPlanClient{
			result: &render.RenderResult{
				ReleaseName: "web",
				Namespace:   ns,
				Manifest:    manifest,
				Revision:    1,
			},
			prep: helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		}
		p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, s.mapper, plan.SemanticBuildInput{
			ClusterContext: kindContext,
			Resolved:       resolved,
		})
		require.NotNil(t, cleanup)
		t.Cleanup(cleanup)
		require.NoError(t, err)

		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Create, p.Changes[0].Action)
		assert.Equal(t, "ConfigMap", p.Changes[0].Resource.Kind)
		assert.Equal(t, "app", p.Changes[0].Resource.Name)
		for _, c := range p.Changes {
			assert.NotEqual(t, "Namespace", c.Resource.Kind)
		}

		cs, _ := s.kubeClients(t)
		_, err = cs.CoreV1().Namespaces().Get(t.Context(), ns, metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err), "namespace %s should still be absent: %v", ns, err)
		_, err = cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err), "configmap app should still be absent: %v", err)
	})

	t.Run("upgrade ignores live drift", func(t *testing.T) {
		ns := fixtureNamespace("semantic-plan-upgrade")
		manifest := configMapManifest(ns, "app", "same") + "---\n" + configMapManifest(ns, "other", "same")
		cs, _ := s.kubeClients(t)
		_, err := cs.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{Name: ns}, metav1.CreateOptions{})
		require.NoError(t, err)
		t.Cleanup(func() { s.deleteNamespace(t, ns) })
		for _, name := range []string{"app", "other"} {
			_, err = cs.CoreV1().ConfigMaps(ns).Create(t.Context(), &corev1.ConfigMap{
				Name:      name,
				Namespace: ns,
				Data:      map[string]string{"key": "same"},
			}, metav1.CreateOptions{})
			require.NoError(t, err)
		}
		live, err := cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
		require.NoError(t, err)
		live.Data["key"] = "drifted"
		_, err = cs.CoreV1().ConfigMaps(ns).Update(t.Context(), live, metav1.UpdateOptions{})
		require.NoError(t, err)
		require.NoError(t, cs.CoreV1().ConfigMaps(ns).Delete(t.Context(), "other", metav1.DeleteOptions{}))

		release := &v1.Release{
			Name:      "web",
			Namespace: ns,
			Manifest:  manifest,
			Version:   1,
		}
		client := &semanticPlanClient{
			result: &render.RenderResult{
				ReleaseName: "web",
				Namespace:   ns,
				Manifest:    manifest,
				IsUpgrade:   true,
				Revision:    2,
			},
			prep: helm.ReleasePrep{
				Operation:    helm.OperationUpgrade,
				Current:      release,
				Newest:       release,
				NextRevision: 2,
			},
		}
		p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, s.mapper, plan.SemanticBuildInput{
			ClusterContext: kindContext,
			Resolved:       resolved,
		})
		require.NotNil(t, cleanup)
		t.Cleanup(cleanup)
		require.NoError(t, err)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		assert.Empty(t, p.Changes)

		got, err := cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "drifted", got.Data["key"])
		_, err = cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "other", metav1.GetOptions{})
		require.True(t, apierrors.IsNotFound(err), "deleted configmap should stay absent: %v", err)
	})
}

func configMapManifest(namespace, name, value string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name +
		"\n  namespace: " + namespace + "\ndata:\n  key: " + value + "\n"
}
