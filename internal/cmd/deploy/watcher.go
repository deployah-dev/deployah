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
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	pollInterval        = 3 * time.Second
	finalRefreshPoll    = 1 * time.Second
	finalRefreshTimeout = 10 * time.Second
)

// ComponentStatus summarizes pod readiness for one Deployah component
// at the end of a deploy.
type ComponentStatus struct {
	// Name is the component name from the Deployah spec.
	Name string
	// ReadyPods is the number of pods that passed readiness checks.
	ReadyPods int
	// TotalPods is the total number of pods for this component.
	TotalPods int
}

// DeployWatcher observes Kubernetes events and pod readiness during a
// Helm deploy. It feeds a live status view to a [nabat.Status] and
// collects warning events for post-deploy reporting.
//
// Create with [NewDeployWatcher], start with [DeployWatcher.Run], then
// read results with [DeployWatcher.Warnings] and [DeployWatcher.Summary]
// after Run returns.
//
// DeployWatcher is not safe for concurrent use. Call Run from exactly
// one goroutine; call Warnings and Summary only after Run returns.
type DeployWatcher struct {
	k8sClient   kubernetes.Interface
	namespace   string
	releaseName string

	mu       sync.Mutex
	warnings []k8s.DeployEvent
	summary  []ComponentStatus
}

// NewDeployWatcher creates a watcher for the given release.
func NewDeployWatcher(
	k8sClient kubernetes.Interface,
	namespace, releaseName string,
) *DeployWatcher {
	return &DeployWatcher{
		k8sClient:   k8sClient,
		namespace:   namespace,
		releaseName: releaseName,
	}
}

// headerUpdater is the subset of [nabat.Status] used by updateTitle.
// Keeping it narrow makes the method trivially testable with a spy struct.
type headerUpdater interface {
	SetTitle(string)
}

// Run watches events and polls pod status until ctx is canceled.
// It updates st with keyed status rows on each change. Run blocks
// until ctx is canceled and is intended to be called in a dedicated
// goroutine.
func (w *DeployWatcher) Run(ctx context.Context, st *nabat.Status) {
	eventCh, err := k8s.WatchDeployEvents(ctx, w.k8sClient, w.namespace, w.releaseName)
	if err != nil {
		st.SetTitle(fmt.Sprintf("event watch unavailable: %v", err))
		<-ctx.Done()
		return
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.finalRefresh(st) //nolint:contextcheck // ctx is canceled; finalRefresh creates its own timeout
			return

		case ev, ok := <-eventCh:
			if !ok {
				<-ctx.Done()
				w.finalRefresh(st) //nolint:contextcheck // ctx is canceled; finalRefresh creates its own timeout
				return
			}
			w.trackWarning(ev)
			w.pushRow(st, ev)
			w.refreshPodStatus(ctx)
			w.updateTitle(st)

		case <-ticker.C:
			w.refreshPodStatus(ctx)
			w.updateTitle(st)
		}
	}
}

// Warnings returns the Warning-type events collected during the deploy.
// Call only after [DeployWatcher.Run] returns.
func (w *DeployWatcher) Warnings() []k8s.DeployEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]k8s.DeployEvent(nil), w.warnings...)
}

// Summary returns per-component pod readiness from the last poll.
// Call only after [DeployWatcher.Run] returns.
func (w *DeployWatcher) Summary() []ComponentStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]ComponentStatus(nil), w.summary...)
}

// trackWarning records Warning-type events for post-deploy reporting,
// updating existing entries by UID rather than duplicating them.
func (w *DeployWatcher) trackWarning(ev k8s.DeployEvent) {
	if ev.Type != corev1.EventTypeWarning {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, ww := range w.warnings {
		if ww.UID == ev.UID {
			w.warnings[i] = ev
			return
		}
	}
	w.warnings = append(w.warnings, ev)
}

// pushRow adds or updates the Nabat status row for ev. The event UID is
// used as the row key for dedup only and is not displayed (Label is set to
// suppress the key column). Cells are REASON, OBJECT, MESSAGE in that order.
// Warning events use Warn(), normal events use Done().
func (w *DeployWatcher) pushRow(st *nabat.Status, ev k8s.DeployEvent) {
	msg := ev.Message
	if ev.Count > 1 {
		msg += fmt.Sprintf(" (x%d)", ev.Count)
	}
	// Label("") suppresses the UID key from the display; cells carry all
	// visible content in REASON, OBJECT, MESSAGE column order.
	row := st.Row(string(ev.UID)).Label("").Set(ev.Reason, ev.Object, msg)
	if ev.Type == corev1.EventTypeWarning {
		row.Warn()
	} else {
		row.Done()
	}
}

// updateTitle sets the status header to the current pod readiness summary.
func (w *DeployWatcher) updateTitle(st headerUpdater) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.summary) == 0 {
		st.SetTitle("waiting for pods...")
		return
	}
	total, ready := 0, 0
	for _, s := range w.summary {
		total += s.TotalPods
		ready += s.ReadyPods
	}
	st.SetTitle(fmt.Sprintf("pods %d/%d ready", ready, total))
}

// finalRefresh polls pod readiness in a loop until all pods are ready
// or finalRefreshTimeout elapses. It updates the status title after
// each poll and sets the completion to RowWarning if pods are still
// not fully ready when the timeout expires.
//
// Called after the parent ctx is already canceled, hence the
// independent timeout.
func (w *DeployWatcher) finalRefresh(st *nabat.Status) {
	deadline := time.After(finalRefreshTimeout)
	ticker := time.NewTicker(finalRefreshPoll)
	defer ticker.Stop()

	for {
		freshCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		w.refreshPodStatus(freshCtx)
		cancel()
		w.updateTitle(st)

		if w.allReady() {
			return
		}

		select {
		case <-deadline:
			st.SetCompletion(nabat.RowWarning)
			return
		case <-ticker.C:
		}
	}
}

// allReady reports whether every observed pod is ready.
func (w *DeployWatcher) allReady() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.summary) == 0 {
		return false
	}
	for _, s := range w.summary {
		if s.ReadyPods < s.TotalPods {
			return false
		}
	}
	return true
}

// refreshPodStatus lists pods for the release and updates the per-component
// summary. Errors are silently ignored so the watcher stays passive.
func (w *DeployWatcher) refreshPodStatus(ctx context.Context) {
	pods, err := w.k8sClient.CoreV1().Pods(w.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + w.releaseName,
	})
	if err != nil || pods == nil {
		return
	}

	type counts struct{ ready, total int }
	byComponent := make(map[string]*counts)

	for _, pod := range pods.Items {
		comp := pod.Labels[k8s.ComponentLabel]
		if comp == "" {
			comp = pod.Labels["app.kubernetes.io/component"]
		}
		if comp == "" {
			comp = w.releaseName
		}

		c := byComponent[comp]
		if c == nil {
			c = &counts{}
			byComponent[comp] = c
		}
		c.total++

		if isPodReady(&pod) {
			c.ready++
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.summary = make([]ComponentStatus, 0, len(byComponent))
	for name, c := range byComponent {
		w.summary = append(w.summary, ComponentStatus{
			Name:      name,
			ReadyPods: c.ready,
			TotalPods: c.total,
		})
	}
}

// isPodReady reports whether a pod is running and all containers are ready.
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
}
