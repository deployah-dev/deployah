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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/readiness"

	corev1 "k8s.io/api/core/v1"
)

const (
	pollInterval        = 3 * time.Second
	finalRefreshPoll    = 1 * time.Second
	finalRefreshTimeout = 10 * time.Second
	finalRefreshBudget  = 2 * time.Second
)

// ComponentStatus summarizes pod readiness for one Deployah component at
// the end of a deploy. Alias for [readiness.ComponentStatus].
type ComponentStatus = readiness.ComponentStatus

// DeployWatcher observes Kubernetes events and pod readiness during a Helm
// deploy, feeding a live status view to a [nabat.Status]. Not safe for
// concurrent use; call Run once, and read Warnings/Summary after Run returns.
type DeployWatcher struct {
	k8sClient   kubernetes.Interface
	namespace   string
	releaseName string

	mu        sync.Mutex
	warnings  []k8s.DeployEvent
	summary   []ComponentStatus
	pollStale bool // true when the last readiness poll failed
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

// Run watches events and polls pod status until ctx is canceled, updating
// st with keyed status rows. Intended to run in a dedicated goroutine.
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
			w.finalRefresh(ctx, st)
			return

		case ev, ok := <-eventCh:
			if !ok {
				<-ctx.Done()
				w.finalRefresh(ctx, st)
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

// pushRow adds or updates the Nabat status row for ev.
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
// When the last poll failed, the title marks status as stale so the operator
// can tell updates have stopped without the watcher exiting.
func (w *DeployWatcher) updateTitle(st headerUpdater) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pollStale {
		if len(w.summary) == 0 {
			st.SetTitle("pod status unavailable (retrying...)")
			return
		}
		ready, total := readyTotal(w.summary)
		st.SetTitle(fmt.Sprintf("pods %d/%d ready (status stale)", ready, total))
		return
	}
	if len(w.summary) == 0 {
		st.SetTitle("waiting for pods...")
		return
	}
	ready, total := readyTotal(w.summary)
	st.SetTitle(fmt.Sprintf("pods %d/%d ready", ready, total))
}

// readyTotal sums ReadyPods and TotalPods across statuses.
func readyTotal(statuses []ComponentStatus) (ready, total int) {
	for _, s := range statuses {
		ready += s.ReadyPods
		total += s.TotalPods
	}
	return ready, total
}

// finalRefresh polls pod readiness until all pods are ready or
// finalRefreshTimeout elapses, marking RowWarning on timeout. Called after
// the parent ctx is already canceled; [context.WithoutCancel] preserves
// request-scoped values while detaching from that cancellation.
func (w *DeployWatcher) finalRefresh(ctx context.Context, st *nabat.Status) {
	deadline := time.After(finalRefreshTimeout)
	ticker := time.NewTicker(finalRefreshPoll)
	defer ticker.Stop()

	base := context.WithoutCancel(ctx)
	for {
		freshCtx, cancel := context.WithTimeout(base, finalRefreshBudget)
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
	return readiness.AllReady(w.summary)
}

// refreshPodStatus polls pods for the release via [readiness.Poll] and
// updates the per-component summary. Poll failures leave the previous
// summary in place, mark status stale for the title, and log at debug so
// the watcher stays passive without going silent.
func (w *DeployWatcher) refreshPodStatus(ctx context.Context) {
	statuses, err := readiness.Poll(ctx, w.k8sClient, w.namespace, w.releaseName)
	if err != nil {
		// Shutdown cancel is expected; other failures (including deadline)
		// mark the title stale so the operator sees updates have stopped.
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.DebugContext(ctx, "deploy watcher: pod status poll failed",
			"err", err,
			"namespace", w.namespace,
			"release", w.releaseName,
		)
		w.mu.Lock()
		w.pollStale = true
		w.mu.Unlock()
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.summary = statuses
	w.pollStale = false
}
