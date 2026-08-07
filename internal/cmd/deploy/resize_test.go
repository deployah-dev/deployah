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

package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/spec"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRequireResizeFlag(t *testing.T) {
	t.Parallel()
	stateful := []persistenceResize{{
		Component: "db", PreviousSize: "10Gi", NewSize: "20Gi", Stateful: true,
	}}
	require.NoError(t, requireResizeFlag(stateful, true))
	err := requireResizeFlag(stateful, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--resize-volumes")
	assert.Contains(t, err.Error(), "db: 10Gi -> 20Gi")
	assert.Contains(t, err.Error(), "orphan-deleted")

	stateless := []persistenceResize{{
		Component: "web", PreviousSize: "1Gi", NewSize: "2Gi", Stateful: false,
	}}
	err = requireResizeFlag(stateless, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--resize-volumes")
	assert.NotContains(t, err.Error(), "orphan-deleted")
}

func TestResizeFailureHint(t *testing.T) {
	t.Parallel()
	stateful := resizeFailureHint([]persistenceResize{{Component: "db", Stateful: true}})
	assert.Contains(t, stateful, "orphan-deleted")
	assert.Contains(t, stateful, "PVCs may already be patched")

	stateless := resizeFailureHint([]persistenceResize{{Component: "web", Stateful: false}})
	assert.NotContains(t, stateless, "orphan-deleted")
	assert.Contains(t, stateless, "PVCs may already be patched")
}

func TestDetectPersistenceResizes(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("db", map[string]any{
		"workloadKind":    "StatefulSet",
		"persistenceSize": "10Gi",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {
				Kind: spec.ComponentKindStateful,
				Persistence: &spec.Persistence{
					Size:      "20Gi",
					MountPath: "/data",
				},
			},
		},
	}
	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"db": {StorageClass: "fast-ssd"},
		},
	}
	resizes := detectPersistenceResizes(manifest, "production", resolved, prev)
	require.Len(t, resizes, 1)
	assert.Equal(t, "db", resizes[0].Component)
	assert.Equal(t, "10Gi", resizes[0].PreviousSize)
	assert.Equal(t, "20Gi", resizes[0].NewSize)
	assert.Equal(t, "fast-ssd", resizes[0].StorageClass)
	assert.True(t, resizes[0].Stateful)
}

func TestDetectPersistenceResizes_Stateless(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("web", map[string]any{
		"workloadKind":    "Deployment",
		"persistenceSize": "1Gi",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"web": {
				Kind: spec.ComponentKindStateless,
				Persistence: &spec.Persistence{
					Size:      "2Gi",
					MountPath: "/data",
				},
			},
		},
	}
	resizes := detectPersistenceResizes(manifest, "production", nil, prev)
	require.Len(t, resizes, 1)
	assert.Equal(t, "web", resizes[0].Component)
	assert.False(t, resizes[0].Stateful)
}

func TestResizeVolumes_HappyPath(t *testing.T) {
	t.Parallel()

	allow := true
	sc := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		AllowVolumeExpansion: &allow,
	}
	sts := &appsv1.StatefulSet{
		Name:      "shop-production-db",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{Name: "data"},
			},
		},
	}
	qty10 := resource.MustParse("10Gi")
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "data-shop-production-db-0",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty10},
			},
			StorageClassName: new("fast-ssd"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: qty10},
			Conditions: []corev1.PersistentVolumeClaimCondition{{
				Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
				Status: corev1.ConditionTrue,
			}},
		},
	}

	client := fake.NewSimpleClientset(sc, sts, pvc)
	resizes := []persistenceResize{{
		Component: "db", PreviousSize: "10Gi", NewSize: "20Gi",
		StorageClass: "fast-ssd", Stateful: true,
	}}

	err := resizeVolumes(t.Context(), client, "default", "shop-production", resizes)
	require.NoError(t, err)

	updated, getErr := client.CoreV1().PersistentVolumeClaims("default").Get(t.Context(), "data-shop-production-db-0", metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.Equal(t, "20Gi", updated.Spec.Resources.Requests.Storage().String())

	_, stsErr := client.AppsV1().StatefulSets("default").Get(t.Context(), "shop-production-db", metav1.GetOptions{})
	require.Error(t, stsErr, "StatefulSet should be orphan-deleted")
}

func TestResizeVolumes_StatelessSharedPVC(t *testing.T) {
	t.Parallel()

	allow := true
	sc := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		AllowVolumeExpansion: &allow,
	}
	qty1 := resource.MustParse("1Gi")
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "shop-production-web",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "web",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty1},
			},
			StorageClassName: new("fast-ssd"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")},
		},
	}
	client := fake.NewSimpleClientset(sc, pvc)
	err := resizeVolumes(t.Context(), client, "default", "shop-production", []persistenceResize{{
		Component: "web", NewSize: "2Gi", StorageClass: "fast-ssd", Stateful: false,
	}})
	require.NoError(t, err)

	updated, getErr := client.CoreV1().PersistentVolumeClaims("default").Get(t.Context(), "shop-production-web", metav1.GetOptions{})
	require.NoError(t, getErr)
	assert.Equal(t, "2Gi", updated.Spec.Resources.Requests.Storage().String())
}

func TestResizeVolumes_ExpansionNotAllowed(t *testing.T) {
	t.Parallel()
	allow := false
	sc := &storagev1.StorageClass{
		Name:                 "slow",
		AllowVolumeExpansion: &allow,
	}
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "shop-production-web",
		Namespace: "default",
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
			StorageClassName: new("slow"),
		},
	}
	client := fake.NewSimpleClientset(sc, pvc)
	err := resizeVolumes(t.Context(), client, "default", "shop-production", []persistenceResize{{
		Component: "web", NewSize: "20Gi", StorageClass: "slow", Stateful: false,
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not allow volume expansion")
}

func TestResizeVolumes_DefaultStorageClass(t *testing.T) {
	t.Parallel()
	allow := true
	sc := &storagev1.StorageClass{
		Name: "standard",
		Annotations: map[string]string{
			defaultSCAnnotKey: "true",
		},
		AllowVolumeExpansion: &allow,
	}
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "shop-production-web",
		Namespace: "default",
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("2Gi")},
		},
	}
	client := fake.NewSimpleClientset(sc, pvc)
	err := resizeVolumes(t.Context(), client, "default", "shop-production", []persistenceResize{{
		Component: "web", NewSize: "2Gi", Stateful: false,
	}})
	require.NoError(t, err)
}

func TestResizeVolumes_PrefersPVCStorageClassOverHint(t *testing.T) {
	t.Parallel()
	allowFast := true
	allowSlow := false
	fast := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		AllowVolumeExpansion: &allowFast,
	}
	slow := &storagev1.StorageClass{
		Name:                 "slow",
		AllowVolumeExpansion: &allowSlow,
	}
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "shop-production-web",
		Namespace: "default",
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
			StorageClassName: new("slow"),
		},
	}
	client := fake.NewSimpleClientset(fast, slow, pvc)
	// Hint says fast (expandable), but live PVC is on slow (not expandable).
	err := resizeVolumes(t.Context(), client, "default", "shop-production", []persistenceResize{{
		Component: "web", NewSize: "2Gi", StorageClass: "fast-ssd", Stateful: false,
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slow")
	assert.Contains(t, err.Error(), "does not allow volume expansion")
}

func TestResolveStorageClassForExpansion_Order(t *testing.T) {
	t.Parallel()
	allow := true
	defaultSC := &storagev1.StorageClass{
		Name: "standard",
		Annotations: map[string]string{
			defaultSCAnnotKey: "true",
		},
		AllowVolumeExpansion: &allow,
	}
	client := fake.NewSimpleClientset(defaultSC)

	pvcWithClass := &corev1.PersistentVolumeClaim{
		Name: "with-class",
		Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: new("pvc-class")},
	}
	got, err := resolveStorageClassForExpansion(t.Context(), client, pvcWithClass, "hint-class")
	require.NoError(t, err)
	assert.Equal(t, "pvc-class", got)

	pvcNoClass := &corev1.PersistentVolumeClaim{Name: "no-class"}
	got, err = resolveStorageClassForExpansion(t.Context(), client, pvcNoClass, "hint-class")
	require.NoError(t, err)
	assert.Equal(t, "hint-class", got)

	got, err = resolveStorageClassForExpansion(t.Context(), client, pvcNoClass, "")
	require.NoError(t, err)
	assert.Equal(t, "standard", got)
}

func TestResizeVolumes_OrphanDeleteMissingStatefulSet(t *testing.T) {
	t.Parallel()

	allow := true
	sc := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		AllowVolumeExpansion: &allow,
	}
	qty := resource.MustParse("10Gi")
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "data-shop-production-db-0",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty},
			},
			StorageClassName: new("fast-ssd"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")},
			Conditions: []corev1.PersistentVolumeClaimCondition{{
				Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	client := fake.NewSimpleClientset(sc, pvc)
	err := resizeVolumes(t.Context(), client, "default", "shop-production", []persistenceResize{{
		Component: "db", NewSize: "20Gi", StorageClass: "fast-ssd", Stateful: true,
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `orphan-delete: StatefulSet for component "db" not found`)
}

func TestResizeVolumes_OrphanDeleteLeavesPods(t *testing.T) {
	t.Parallel()

	allow := true
	sc := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		AllowVolumeExpansion: &allow,
	}
	sts := &appsv1.StatefulSet{
		Name:      "shop-production-db",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{Name: "data"},
			},
		},
	}
	qty := resource.MustParse("10Gi")
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "data-shop-production-db-0",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty},
			},
			StorageClassName: new("fast-ssd"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")},
			Conditions: []corev1.PersistentVolumeClaimCondition{{
				Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	pod := &corev1.Pod{
		Name:      "shop-production-db-0",
		Namespace: "default",
		Labels:    map[string]string{"app.kubernetes.io/instance": "shop-production"},
		Status:    corev1.PodStatus{Phase: corev1.PodRunning},
	}

	client := fake.NewSimpleClientset(sc, sts, pvc, pod)
	require.NoError(t, resizeVolumes(t.Context(), client, "default", "shop-production", []persistenceResize{{
		Component: "db", NewSize: "20Gi", StorageClass: "fast-ssd", Stateful: true,
	}}))

	livePod, err := client.CoreV1().Pods("default").Get(t.Context(), "shop-production-db-0", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, corev1.PodRunning, livePod.Status.Phase)

	livePVC, err := client.CoreV1().PersistentVolumeClaims("default").Get(t.Context(), "data-shop-production-db-0", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "20Gi", livePVC.Spec.Resources.Requests.Storage().String())
}
