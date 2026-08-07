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
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	resizeWaitTimeout  = 5 * time.Minute
	resizePollInterval = 2 * time.Second
	defaultSCAnnotKey  = "storageclass.kubernetes.io/is-default-class"
)

// persistenceResize describes one component whose persistence.size must grow.
type persistenceResize struct {
	Component    string
	PreviousSize string
	NewSize      string
	StorageClass string // resolved Kubernetes class name; may be empty
	Stateful     bool   // true when workload is StatefulSet (needs orphan-delete)
}

// detectPersistenceResizes compares previous resolved sizes against the
// current manifest. Only increases are returned; decreases are rejected by
// checkWorkloadGuards. Works for both stateful and stateless persistence.
// prevResolved must be non-nil (use an empty map when there is no prior release).
func detectPersistenceResizes(
	manifest *spec.Spec,
	environment string,
	resolved *spec.ResolvedSpec,
	prevResolved map[string]map[string]any,
) []persistenceResize {
	var out []persistenceResize
	for name, component := range manifest.Components {
		if !componentActiveInEnv(component, environment) || component.Persistence == nil {
			continue
		}
		prev, hasPrev := prevResolved[name]
		if !hasPrev {
			continue
		}
		prevSize, hasPrevSize := prev["persistenceSize"].(string)
		if !hasPrevSize || prevSize == "" || prevSize == component.Persistence.Size {
			continue
		}
		decreased, cmpErr := persistenceSizeDecreased(prevSize, component.Persistence.Size)
		if cmpErr != nil || decreased {
			continue // size-decrease guard owns that error
		}
		sc := ""
		if resolved != nil {
			if rc, found := resolved.Components[name]; found {
				sc = rc.StorageClass
			}
		}
		out = append(out, persistenceResize{
			Component:    name,
			PreviousSize: prevSize,
			NewSize:      component.Persistence.Size,
			StorageClass: sc,
			Stateful:     component.Kind == spec.ComponentKindStateful,
		})
	}
	slices.SortFunc(out, func(a, b persistenceResize) int {
		return cmp.Compare(a.Component, b.Component)
	})
	return out
}

// requireResizeFlag errors when volume growth is needed and --resize-volumes
// was not passed.
func requireResizeFlag(resizes []persistenceResize, enabled bool) error {
	if len(resizes) == 0 || enabled {
		return nil
	}
	lines := make([]string, 0, len(resizes))
	for _, r := range resizes {
		lines = append(lines, fmt.Sprintf("  %s: %s -> %s", r.Component, r.PreviousSize, r.NewSize))
	}
	hint := "re-run deploy with --resize-volumes to expand PVCs"
	if hasStatefulResize(resizes) {
		hint += " (StatefulSet controllers are orphan-deleted so Helm can re-apply volumeClaimTemplates)"
	}
	return fmt.Errorf(
		"persistence.size increase requires --resize-volumes:\n%s\n%s",
		strings.Join(lines, "\n"), hint,
	)
}

// resizeFailureHint describes recovery after a failed resizeVolumes call.
// Mentions orphan-delete only when at least one resize targeted a StatefulSet.
func resizeFailureHint(resizes []persistenceResize) string {
	if hasStatefulResize(resizes) {
		return "resize volumes failed (PVCs may already be patched; StatefulSets may have been orphan-deleted; pods/PVCs should still be running; re-run deploy with --resize-volumes after fixing the cause)"
	}
	return "resize volumes failed (PVCs may already be patched; re-run deploy with --resize-volumes after fixing the cause)"
}

func hasStatefulResize(resizes []persistenceResize) bool {
	for _, r := range resizes {
		if r.Stateful {
			return true
		}
	}
	return false
}

// resizeVolumes patches PVC requests, waits for expansion, then orphan-deletes
// StatefulSets that need volumeClaimTemplates rewritten. Stateless components
// only patch the shared PVC; Helm upgrade updates the claim template in values.
func resizeVolumes(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace, releaseName string,
	resizes []persistenceResize,
) error {
	if k8sClient == nil {
		return fmt.Errorf("resize volumes: kubernetes client is required")
	}
	if len(resizes) == 0 {
		return nil
	}

	var patched []string
	var statefulComponents []string
	checkedClasses := map[string]struct{}{}

	for _, r := range resizes {
		qty, parseErr := resource.ParseQuantity(r.NewSize)
		if parseErr != nil {
			return fmt.Errorf("component %s: parse size %q: %w", r.Component, r.NewSize, parseErr)
		}

		pvcNames, listErr := componentPVCNames(ctx, k8sClient, namespace, releaseName, r)
		if listErr != nil {
			return listErr
		}
		if len(pvcNames) == 0 {
			return fmt.Errorf("resize volumes: no PVCs found for component %s", r.Component)
		}

		for _, pvcName := range pvcNames {
			pvc, getErr := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvcName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("get PVC %s: %w", pvcName, getErr)
			}
			className, classErr := resolveStorageClassForExpansion(ctx, k8sClient, pvc, r.StorageClass)
			if classErr != nil {
				return classErr
			}
			if _, seen := checkedClasses[className]; !seen {
				if expandErr := ensureVolumeExpansionAllowed(ctx, k8sClient, className); expandErr != nil {
					return expandErr
				}
				checkedClasses[className] = struct{}{}
			}
			if patchErr := patchPVCSize(ctx, k8sClient, namespace, pvc, qty); patchErr != nil {
				return patchErr
			}
			patched = append(patched, pvcName)
		}
		if r.Stateful {
			statefulComponents = append(statefulComponents, r.Component)
		}
	}

	for _, pvcName := range patched {
		if waitErr := waitForPVCExpansion(ctx, k8sClient, namespace, pvcName); waitErr != nil {
			return waitErr
		}
	}

	return orphanDeleteStatefulSets(ctx, k8sClient, namespace, releaseName, statefulComponents)
}

// orphanDeleteStatefulSets removes StatefulSet controllers (orphan propagation)
// for each listed component so Helm can recreate them with updated
// volumeClaimTemplates. Every component must match exactly one StatefulSet.
func orphanDeleteStatefulSets(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace, releaseName string,
	components []string,
) error {
	if len(components) == 0 {
		return nil
	}

	propagation := metav1.DeletePropagationOrphan
	for _, comp := range components {
		stsList, err := k8sClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf(
				"app.kubernetes.io/instance=%s,%s=%s",
				releaseName, spec.LabelComponent, comp,
			),
		})
		if err != nil {
			return fmt.Errorf("list StatefulSets for component %s: %w", comp, err)
		}
		if len(stsList.Items) == 0 {
			return fmt.Errorf("orphan-delete: StatefulSet for component %q not found", comp)
		}
		if len(stsList.Items) > 1 {
			return fmt.Errorf(
				"orphan-delete: expected 1 StatefulSet for component %q, found %d",
				comp, len(stsList.Items),
			)
		}
		sts := &stsList.Items[0]
		if delErr := k8sClient.AppsV1().StatefulSets(namespace).Delete(ctx, sts.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); delErr != nil {
			return fmt.Errorf("orphan-delete StatefulSet %s: %w", sts.Name, delErr)
		}
	}
	return nil
}

func componentPVCNames(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace, releaseName string,
	r persistenceResize,
) ([]string, error) {
	if r.Stateful {
		// StatefulSet PVCs inherit labels from volumeClaimTemplates.
		return listPVCsByComponentLabels(ctx, k8sClient, namespace, releaseName, r.Component)
	}
	return deploymentComponentPVCNames(ctx, k8sClient, namespace, releaseName, r.Component)
}

func deploymentComponentPVCNames(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace, releaseName, component string,
) ([]string, error) {
	// Chart PVC name is common.names.fullname = {release}-{component}.
	direct := releaseName + "-" + component
	_, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, direct, metav1.GetOptions{})
	if err == nil {
		return []string{direct}, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get PVC %s: %w", direct, err)
	}

	return listPVCsByComponentLabels(ctx, k8sClient, namespace, releaseName, component)
}

func resolveStorageClassForExpansion(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	pvc *corev1.PersistentVolumeClaim,
	hint string,
) (string, error) {
	// Prefer the live PVC class: that is what expansion actually targets.
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		return *pvc.Spec.StorageClassName, nil
	}
	if hint != "" {
		return hint, nil
	}
	defaultName, err := defaultStorageClassName(ctx, k8sClient)
	if err != nil {
		return "", err
	}
	if defaultName == "" {
		return "", fmt.Errorf(
			"pvc %s has no storage class and the cluster has no default storage class; cannot verify volume expansion",
			pvc.Name,
		)
	}
	return defaultName, nil
}

func defaultStorageClassName(ctx context.Context, k8sClient kubernetes.Interface) (string, error) {
	list, err := k8sClient.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list storage classes: %w", err)
	}
	for i := range list.Items {
		sc := &list.Items[i]
		if sc.Annotations[defaultSCAnnotKey] == "true" {
			return sc.Name, nil
		}
	}
	return "", nil
}

func ensureVolumeExpansionAllowed(ctx context.Context, k8sClient kubernetes.Interface, className string) error {
	sc, err := k8sClient.StorageV1().StorageClasses().Get(ctx, className, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get storage class %q: %w", className, err)
	}
	if sc.AllowVolumeExpansion == nil || !*sc.AllowVolumeExpansion {
		return fmt.Errorf(
			"storage class %q does not allow volume expansion (allowVolumeExpansion is not true); cannot resize volumes",
			className,
		)
	}
	return nil
}

func listPVCsByComponentLabels(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace, releaseName, component string,
) ([]string, error) {
	pvcList, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf(
			"app.kubernetes.io/instance=%s,%s=%s",
			releaseName, spec.LabelComponent, component,
		),
	})
	if err != nil {
		return nil, fmt.Errorf("list PVCs for component %s: %w", component, err)
	}
	names := make([]string, 0, len(pvcList.Items))
	for _, pvc := range pvcList.Items {
		names = append(names, pvc.Name)
	}
	return names, nil
}

func patchPVCSize(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	namespace string,
	pvc *corev1.PersistentVolumeClaim,
	size resource.Quantity,
) error {
	if pvc.Spec.Resources.Requests == nil {
		pvc.Spec.Resources.Requests = corev1.ResourceList{}
	}
	current := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if current.Cmp(size) >= 0 {
		return nil
	}
	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = size
	if _, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, pvc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("patch PVC %s storage to %s: %w", pvc.Name, size.String(), err)
	}
	return nil
}

func waitForPVCExpansion(ctx context.Context, k8sClient kubernetes.Interface, namespace, name string) error {
	deadline := time.Now().Add(resizeWaitTimeout)
	ticker := time.NewTicker(resizePollInterval)
	defer ticker.Stop()

	for {
		pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("wait for PVC %s expansion: %w", name, err)
		}
		req := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		capQty := pvc.Status.Capacity[corev1.ResourceStorage]
		if !capQty.IsZero() && capQty.Cmp(req) >= 0 {
			return nil
		}
		for _, cond := range pvc.Status.Conditions {
			if cond.Type == corev1.PersistentVolumeClaimFileSystemResizePending &&
				cond.Status == corev1.ConditionTrue {
				// Online/offline FS resize will finish after pod restart.
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for PVC %s expansion (requested %s, capacity %s)",
				name, req.String(), capQty.String())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
