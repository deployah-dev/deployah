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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/k8s"

	corev1 "k8s.io/api/core/v1"
)

// newWatcher builds a DeployWatcher for tests.
func newWatcher() *DeployWatcher {
	return NewDeployWatcher(nil, "default", "myapp-prod")
}

// makeDeployEvent is a shorthand for building test events.
func makeDeployEvent(uid types.UID, evType, reason, object, message string, count int32) k8s.DeployEvent {
	return k8s.DeployEvent{
		UID:       uid,
		Type:      evType,
		Reason:    reason,
		Object:    object,
		Message:   message,
		Count:     count,
		Timestamp: time.Now(),
	}
}

// runStatus runs fn inside a non-TTY c.Status call and returns the text
// written to stderr (the title + plain-text row table).
func runStatus(t *testing.T, fn func(*nabat.Status), title string) string {
	t.Helper()
	io, _, _, stderr := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	app.MustCommand("run", nabat.WithRun(func(c *nabat.Context) error {
		return c.Status(func(st *nabat.Status) error {
			fn(st)
			return nil
		}, nabat.WithTitle(title))
	}))
	require.NoError(t, nabattest.Run(t, app, []string{"run"}))
	return stderr.String()
}

// TestDeployWatcher_Warnings_CollectsOnlyWarningEvents verifies that
// only Warning-type events appear in the warnings list.
func TestDeployWatcher_Warnings_CollectsOnlyWarningEvents(t *testing.T) {
	w := newWatcher()

	w.trackWarning(makeDeployEvent("uid-norm", corev1.EventTypeNormal, "Scheduled", "pod/abc", "ok", 1))
	w.trackWarning(makeDeployEvent("uid-warn1", corev1.EventTypeWarning, "BackOff", "pod/abc", "back-off restarting", 2))
	w.trackWarning(makeDeployEvent("uid-warn2", corev1.EventTypeWarning, "Failed", "pod/abc", "failed to pull image", 1))

	warnings := w.Warnings()
	assert.Len(t, warnings, 2)
	for _, ww := range warnings {
		assert.Equal(t, corev1.EventTypeWarning, ww.Type)
	}
}

// TestDeployWatcher_Warnings_DeduplicatesOnUpdate verifies that
// repeated warnings with the same UID update rather than duplicate.
func TestDeployWatcher_Warnings_DeduplicatesOnUpdate(t *testing.T) {
	w := newWatcher()

	w.trackWarning(makeDeployEvent("uid-w", corev1.EventTypeWarning, "BackOff", "pod/api", "crash", 1))
	w.trackWarning(makeDeployEvent("uid-w", corev1.EventTypeWarning, "BackOff", "pod/api", "crash", 5))

	warnings := w.Warnings()
	assert.Len(t, warnings, 1, "same UID should not produce duplicate warnings")
	assert.Equal(t, int32(5), warnings[0].Count)
}

// TestDeployWatcher_Summary_ReturnsCopy verifies that Summary
// returns an independent copy that does not alias internal state.
func TestDeployWatcher_Summary_ReturnsCopy(t *testing.T) {
	w := newWatcher()
	w.mu.Lock()
	w.summary = []ComponentStatus{{Name: "api", ReadyPods: 1, TotalPods: 2}}
	w.mu.Unlock()

	s := w.Summary()
	assert.Len(t, s, 1)

	// Mutate the returned slice; the internal state must not change.
	s[0].ReadyPods = 99
	s2 := w.Summary()
	assert.Equal(t, 1, s2[0].ReadyPods, "Summary must return an independent copy")
}

// TestDeployWatcher_PushRow_NormalEvent verifies that a Normal event row
// appears in the rendered Status output with the expected fields.
func TestDeployWatcher_PushRow_NormalEvent(t *testing.T) {
	w := newWatcher()
	ev := makeDeployEvent("uid-1", corev1.EventTypeNormal, "Scheduled", "pod/api-abc", "successfully assigned", 1)

	out := runStatus(t, func(st *nabat.Status) {
		w.pushRow(st, ev)
	}, "test")

	assert.Contains(t, out, "Scheduled", "row must contain the reason")
	assert.Contains(t, out, "pod/api-abc", "row must contain the object ref")
	assert.Contains(t, out, "successfully assigned", "row must contain the message")
}

// TestDeployWatcher_PushRow_WarningEvent verifies that a Warning event
// renders with the warning icon.
func TestDeployWatcher_PushRow_WarningEvent(t *testing.T) {
	w := newWatcher()
	ev := makeDeployEvent("uid-w", corev1.EventTypeWarning, "Failed", "pod/api-abc", "ErrImagePull", 1)

	out := runStatus(t, func(st *nabat.Status) {
		w.pushRow(st, ev)
	}, "test")

	assert.Contains(t, out, "!", "warning row must show the warning icon")
	assert.Contains(t, out, "Failed", "row must contain the reason")
}

// TestDeployWatcher_PushRow_CountAppendedToMessage verifies that a count
// greater than one is appended to the message cell as "(xN)".
func TestDeployWatcher_PushRow_CountAppendedToMessage(t *testing.T) {
	w := newWatcher()
	ev := makeDeployEvent("uid-c", corev1.EventTypeWarning, "Failed", "pod/api-abc", "ErrImagePull", 3)

	out := runStatus(t, func(st *nabat.Status) {
		w.pushRow(st, ev)
	}, "test")

	assert.Contains(t, out, "(x3)", "message must include count when greater than one")
}

// TestDeployWatcher_PushRow_CountNotShownWhenOne verifies that a count
// of 1 is not appended to the message.
func TestDeployWatcher_PushRow_CountNotShownWhenOne(t *testing.T) {
	w := newWatcher()
	ev := makeDeployEvent("uid-one", corev1.EventTypeNormal, "Started", "pod/api", "container started", 1)

	out := runStatus(t, func(st *nabat.Status) {
		w.pushRow(st, ev)
	}, "test")

	assert.NotContains(t, out, "(x1)", "count of 1 must not be displayed")
}

// TestDeployWatcher_PushRow_UIDNotDisplayed verifies that the event UID used
// as the row key does not appear in the rendered output.
func TestDeployWatcher_PushRow_UIDNotDisplayed(t *testing.T) {
	w := newWatcher()
	uid := types.UID("uid-do-not-show-this-1234")
	ev := makeDeployEvent(uid, corev1.EventTypeNormal, "Scheduled", "pod/api", "assigned", 1)

	out := runStatus(t, func(st *nabat.Status) {
		w.pushRow(st, ev)
	}, "test")

	assert.NotContains(t, out, string(uid), "UID must not appear in the rendered output")
}

// spyTitleSetter records the last title set via SetTitle. It satisfies the
// headerUpdater interface and is used in updateTitle tests to avoid the
// non-TTY limitation where dynamic SetTitle calls are not re-printed.
type spyTitleSetter struct{ title string }

func (s *spyTitleSetter) SetTitle(title string) { s.title = title }

// TestDeployWatcher_UpdateTitle_NoPods verifies that the title reads
// "waiting for pods..." when no pods have been observed yet.
func TestDeployWatcher_UpdateTitle_NoPods(t *testing.T) {
	w := newWatcher()
	spy := &spyTitleSetter{}

	w.updateTitle(spy)

	assert.Equal(t, "waiting for pods...", spy.title)
}

// TestDeployWatcher_UpdateTitle_WithPods verifies that the title shows
// a ready/total count when pod summary data is available.
func TestDeployWatcher_UpdateTitle_WithPods(t *testing.T) {
	w := newWatcher()
	w.mu.Lock()
	w.summary = []ComponentStatus{{Name: "api", ReadyPods: 2, TotalPods: 3}}
	w.mu.Unlock()

	spy := &spyTitleSetter{}
	w.updateTitle(spy)

	assert.Equal(t, "pods 2/3 ready", spy.title)
}
