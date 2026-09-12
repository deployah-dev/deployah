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

//go:build e2e

package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/predict"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *E2ESuite) predictCluster(t *testing.T) predict.Cluster {
	t.Helper()
	cluster, err := predict.NewCluster(s.client.RESTConfig())
	require.NoError(t, err)
	return cluster
}

func (s *E2ESuite) kubeClients(t *testing.T) (*kubernetes.Clientset, dynamic.Interface) {
	t.Helper()
	cs, err := kubernetes.NewForConfig(s.client.RESTConfig())
	require.NoError(t, err)
	dyn, err := dynamic.NewForConfig(s.client.RESTConfig())
	require.NoError(t, err)
	return cs, dyn
}

func predictConfigMapYAML(name, namespace, value string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name +
		"\n  namespace: " + namespace + "\ndata:\n  key: " + value + "\n"
}

func applyJSON(t *testing.T, obj *unstructured.Unstructured) []byte {
	t.Helper()
	data, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	require.NoError(t, err)
	return data
}

func ownedConfigMapUnstructured(name, namespace, release, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "Helm",
			},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      release,
				"meta.helm.sh/release-namespace": namespace,
			},
		},
		"data": map[string]any{"key": value},
	}}
}

func ssaApplyOwnedConfigMap(t *testing.T, dyn dynamic.Interface, obj *unstructured.Unstructured, fieldManager string) {
	t.Helper()
	gvr := corev1.SchemeGroupVersion.WithResource("configmaps")
	_, err := dyn.Resource(gvr).Namespace(obj.GetNamespace()).Patch(
		t.Context(), obj.GetName(), types.ApplyPatchType, applyJSON(t, obj), metav1.PatchOptions{
			FieldManager:    fieldManager,
			FieldValidation: metav1.FieldValidationStrict,
		})
	require.NoError(t, err)
}

func findPredictResult(t *testing.T, results []predict.Result, name string) predict.Result {
	t.Helper()
	for _, r := range results {
		if r.Identity.Name == name {
			return r
		}
	}
	t.Fatalf("Predict() missing result %q", name)
	return predict.Result{}
}

func (s *E2ESuite) TestPredictSSACreate() {
	t := s.T()
	ns := fixtureNamespace("predict-ssa-create")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	cluster := s.predictCluster(t)
	cs, _ := s.kubeClients(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "app")
	assert.Equal(t, predict.ActionCreate, got.Action)
	require.NotNil(t, got.Predicted)
	key, found, nestErr := unstructured.NestedString(got.Predicted.Object, "data", "key")
	require.NoError(t, nestErr)
	require.True(t, found)
	assert.Equal(t, "next", key)

	_, err = cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err), "live object must still be missing after Predict: %v", err)
}

func (s *E2ESuite) TestPredictSSAUpdate() {
	t := s.T()
	ns := fixtureNamespace("predict-ssa-update")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	cs, dyn := s.kubeClients(t)
	ssaApplyOwnedConfigMap(t, dyn, ownedConfigMapUnstructured("app", ns, "web", "live"), kube.ManagedFieldsManager)

	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "app")
	assert.Equal(t, predict.ActionUpdate, got.Action)
	require.NotNil(t, got.Predicted)
	key, found, nestErr := unstructured.NestedString(got.Predicted.Object, "data", "key")
	require.NoError(t, nestErr)
	require.True(t, found)
	assert.Equal(t, "next", key)

	live, err := cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "live", live.Data["key"])
}

func (s *E2ESuite) TestPredictSSAConflict() {
	t := s.T()
	ns := fixtureNamespace("predict-ssa-conflict")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	_, dyn := s.kubeClients(t)
	ssaApplyOwnedConfigMap(t, dyn, ownedConfigMapUnstructured("app", ns, "web", "other"), "other-manager")

	cluster := s.predictCluster(t)
	_, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.Error(t, err)
	assert.True(t, apierrors.IsConflict(err), "Predict() = %v, want field-manager conflict", err)
}

func (s *E2ESuite) TestPredictAPIServerDefaulting() {
	t := s.T()
	ns := fixtureNamespace("predict-api-default")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	desired := `apiVersion: v1
kind: Service
metadata:
  name: svc
  namespace: ` + ns + `
spec:
  selector:
    app: web
  ports:
  - port: 80
`
	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     desired,
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "svc")
	require.NotNil(t, got.Predicted)
	affinity, found, err := unstructured.NestedString(got.Predicted.Object, "spec", "sessionAffinity")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, string(corev1.ServiceAffinityNone), affinity)
}

func (s *E2ESuite) TestPredictExternalAnnotationPreserved() {
	t := s.T()
	ns := fixtureNamespace("predict-ext-anno")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	cs, dyn := s.kubeClients(t)
	ssaApplyOwnedConfigMap(t, dyn, ownedConfigMapUnstructured("app", ns, "web", "live"), kube.ManagedFieldsManager)
	live, err := cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
	require.NoError(t, err)
	if live.Annotations == nil {
		live.Annotations = map[string]string{}
	}
	live.Annotations["deployah.dev/external"] = "keep-me"
	_, err = cs.CoreV1().ConfigMaps(ns).Update(t.Context(), live, metav1.UpdateOptions{
		FieldManager: "external-annotator",
	})
	require.NoError(t, err)

	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "app")
	require.NotNil(t, got.Predicted)
	assert.Equal(t, "keep-me", got.Predicted.GetAnnotations()["deployah.dev/external"])
}

func (s *E2ESuite) TestPredictOwnedAdoption() {
	t := s.T()
	ns := fixtureNamespace("predict-adopt")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	_, dyn := s.kubeClients(t)
	ssaApplyOwnedConfigMap(t, dyn, ownedConfigMapUnstructured("app", ns, "web", "live"), kube.ManagedFieldsManager)

	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "app")
	assert.Equal(t, predict.ActionUpdate, got.Action)
	assert.NotNil(t, got.Live)
	assert.Empty(t, got.Limitation)
}

func (s *E2ESuite) TestPredictDryRunDelete() {
	t := s.T()
	ns := fixtureNamespace("predict-dry-delete")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	cs, _ := s.kubeClients(t)
	_, err := cs.CoreV1().ConfigMaps(ns).Create(t.Context(), &corev1.ConfigMap{
		Name:      "old",
		Namespace: ns,
		Data:      map[string]string{"key": "live"},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationUpgrade,
		ReleaseName: "web",
		Namespace:   ns,
		Previous:    predictConfigMapYAML("old", ns, "prev"),
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "old")
	assert.Equal(t, predict.ActionDelete, got.Action)

	live, err := cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "old", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "live", live.Data["key"])
}

func (s *E2ESuite) TestPredictManagedFieldsMigration() {
	t := s.T()
	ns := fixtureNamespace("predict-mf-migrate")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	cs, _ := s.kubeClients(t)
	created, err := cs.CoreV1().ConfigMaps(ns).Create(t.Context(), &corev1.ConfigMap{
		Name:      "app",
		Namespace: ns,
		Labels:    map[string]string{"app.kubernetes.io/managed-by": "Helm"},
		Annotations: map[string]string{
			"meta.helm.sh/release-name":      "web",
			"meta.helm.sh/release-namespace": ns,
		},
		Data: map[string]string{"key": "v1"},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	created.Data["key"] = "v2"
	updated, err := cs.CoreV1().ConfigMaps(ns).Update(t.Context(), created, metav1.UpdateOptions{
		FieldManager: kube.ManagedFieldsManager,
	})
	require.NoError(t, err)
	before, err := json.Marshal(updated.ManagedFields)
	require.NoError(t, err)

	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "v2"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "app")
	assert.Equal(t, predict.LimitationManagedFieldsMigration, got.Limitation)
	assert.Equal(t, predict.ActionUpdate, got.Action)

	afterLive, err := cs.CoreV1().ConfigMaps(ns).Get(t.Context(), "app", metav1.GetOptions{})
	require.NoError(t, err)
	after, err := json.Marshal(afterLive.ManagedFields)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

func (s *E2ESuite) TestPredictManagedFieldsSSAAdoption() {
	t := s.T()
	ns := fixtureNamespace("predict-mf-ssa")
	s.createNamespace(t, ns)
	t.Cleanup(func() { s.deleteNamespace(t, ns) })

	_, dyn := s.kubeClients(t)
	ssaApplyOwnedConfigMap(t, dyn, ownedConfigMapUnstructured("app", ns, "web", "live"), kube.ManagedFieldsManager)

	cluster := s.predictCluster(t)
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   ns,
		Desired:     predictConfigMapYAML("app", ns, "next"),
	})
	require.NoError(t, err)
	got := findPredictResult(t, results, "app")
	assert.Empty(t, got.Limitation)
	assert.Equal(t, predict.ActionUpdate, got.Action)
}
