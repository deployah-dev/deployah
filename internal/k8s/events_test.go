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

package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clienttesting "k8s.io/client-go/testing"
)

const (
	testNamespace     = "default"
	testRelease       = "myapp-prod"
	testReleasePrefix = testRelease
)

// makeEvent builds a minimal corev1.Event for testing.
// ResourceVersion is required by the RetryWatcher; callers must
// ensure each event in a test sequence has a distinct, non-empty
// value (the RetryWatcher aborts if ResourceVersion is empty).
func makeEvent(uid types.UID, involvedName, reason, message, evType, rv string) *corev1.Event {
	now := metav1.NewTime(time.Now())
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:            involvedName + "." + string(uid),
			Namespace:       testNamespace,
			UID:             uid,
			ResourceVersion: rv,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: involvedName,
		},
		Reason:        reason,
		Message:       message,
		Type:          evType,
		Count:         1,
		LastTimestamp: now,
	}
}

// sendAndDrain pushes ev into the fake watcher and drains one result
// from ch within 2 seconds.
func sendAndDrain(t *testing.T, fw *watch.FakeWatcher, ch <-chan DeployEvent, evType watch.EventType, ev *corev1.Event) (DeployEvent, bool) {
	t.Helper()
	switch evType {
	case watch.Added:
		fw.Add(ev)
	case watch.Modified:
		fw.Modify(ev)
	}
	select {
	case got, ok := <-ch:
		return got, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on channel")
		return DeployEvent{}, false
	}
}

// TestWatchDeployEvents_MatchingPrefixForwarded verifies that events
// whose involved object name starts with the release prefix are
// forwarded to the channel.
func TestWatchDeployEvents_MatchingPrefixForwarded(t *testing.T) {
	fw := watch.NewFake()
	cs := fake.NewClientset()
	// Replace the watch reactor to return our controlled fake watcher.
	cs.PrependWatchReactor("events", func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, fw, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := WatchDeployEvents(ctx, cs, testNamespace, testReleasePrefix)
	require.NoError(t, err)

	ev := makeEvent("uid-1", testRelease+"-api-abc123", "Scheduled", "pod scheduled", corev1.EventTypeNormal, "101")
	got, ok := sendAndDrain(t, fw, ch, watch.Added, ev)
	require.True(t, ok)

	assert.Equal(t, types.UID("uid-1"), got.UID)
	assert.Equal(t, "Scheduled", got.Reason)
	assert.Equal(t, "pod scheduled", got.Message)
	assert.Equal(t, corev1.EventTypeNormal, got.Type)
	assert.Equal(t, "pod/"+testRelease+"-api-abc123", got.Object)
	assert.Equal(t, int32(1), got.Count)
	assert.False(t, got.Timestamp.IsZero())
}

// TestWatchDeployEvents_NonMatchingPrefixFiltered verifies that
// events for a different release are not forwarded.
func TestWatchDeployEvents_NonMatchingPrefixFiltered(t *testing.T) {
	fw := watch.NewFake()
	cs := fake.NewClientset()
	cs.PrependWatchReactor("events", func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, fw, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := WatchDeployEvents(ctx, cs, testNamespace, testReleasePrefix)
	require.NoError(t, err)

	// Send an event for a different release.
	otherEv := makeEvent("uid-other", "otherpkg-prod-api-xyz", "Pulled", "image pulled", corev1.EventTypeNormal, "101")
	fw.Add(otherEv)

	// Then send a matching event to confirm the channel is live.
	matchEv := makeEvent("uid-match", testRelease+"-api-def456", "Started", "container started", corev1.EventTypeNormal, "102")
	got, ok := sendAndDrain(t, fw, ch, watch.Added, matchEv)
	require.True(t, ok)
	assert.Equal(t, types.UID("uid-match"), got.UID, "only the matching event should arrive")
}

// TestWatchDeployEvents_ModifiedEventForwarded verifies that
// MODIFIED watch events are forwarded with updated fields.
func TestWatchDeployEvents_ModifiedEventForwarded(t *testing.T) {
	fw := watch.NewFake()
	cs := fake.NewClientset()
	cs.PrependWatchReactor("events", func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, fw, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := WatchDeployEvents(ctx, cs, testNamespace, testReleasePrefix)
	require.NoError(t, err)

	ev := makeEvent("uid-mod", testRelease+"-worker-abc", "BackOff", "back-off restarting", corev1.EventTypeWarning, "101")
	ev.Count = 3

	got, _ := sendAndDrain(t, fw, ch, watch.Modified, ev)
	assert.Equal(t, types.UID("uid-mod"), got.UID)
	assert.Equal(t, "Warning", got.Type)
	assert.Equal(t, int32(3), got.Count)
}

// TestWatchDeployEvents_ChannelClosedOnContextCancel verifies that
// the event channel closes promptly after context cancellation.
func TestWatchDeployEvents_ChannelClosedOnContextCancel(t *testing.T) {
	fw := watch.NewFake()
	cs := fake.NewClientset()
	cs.PrependWatchReactor("events", func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, fw, nil
	})

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := WatchDeployEvents(ctx, cs, testNamespace, testReleasePrefix)
	require.NoError(t, err)

	cancel()

	// Channel must close shortly after cancel.
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed after context cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after context cancel")
	}
}

// TestWatchDeployEvents_FieldTranslation verifies that core event
// fields are correctly translated into DeployEvent fields.
func TestWatchDeployEvents_FieldTranslation(t *testing.T) {
	fw := watch.NewFake()
	cs := fake.NewClientset()
	cs.PrependWatchReactor("events", func(action clienttesting.Action) (bool, watch.Interface, error) {
		return true, fw, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := WatchDeployEvents(ctx, cs, testNamespace, testReleasePrefix)
	require.NoError(t, err)

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	ev := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testRelease + "-api-abc.uid-field",
			Namespace:       testNamespace,
			UID:             "uid-field",
			ResourceVersion: "101",
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "ReplicaSet",
			Name: testRelease + "-api-abc",
		},
		Reason:        "SuccessfulCreate",
		Message:       "Created pod: " + testRelease + "-api-abc-xyz",
		Type:          corev1.EventTypeNormal,
		Count:         2,
		LastTimestamp: metav1.NewTime(ts),
	}
	fw.Add(ev)

	select {
	case got := <-ch:
		assert.Equal(t, types.UID("uid-field"), got.UID)
		assert.Equal(t, "replicaset/"+testRelease+"-api-abc", got.Object)
		assert.Equal(t, int32(2), got.Count)
		assert.Equal(t, ts.UTC(), got.Timestamp.UTC())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestToDeployEvent_ZeroCount verifies that a zero Count is normalized to 1.
func TestToDeployEvent_ZeroCount(t *testing.T) {
	ev := watch.Event{
		Type: watch.Added,
		Object: &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{UID: "uid-zero"},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
				Name: "myapp-prod-api-xyz",
			},
			Type:   corev1.EventTypeNormal,
			Reason: "Started",
			Count:  0,
		},
	}
	got, ok := toDeployEvent(ev, "myapp-prod-")
	require.True(t, ok)
	assert.Equal(t, int32(1), got.Count, "zero count should be normalized to 1")
}

// TestToDeployEvent_NonEvent verifies that non-Event objects return false.
func TestToDeployEvent_NonEvent(t *testing.T) {
	ev := watch.Event{
		Type:   watch.Added,
		Object: &corev1.Pod{},
	}
	_, ok := toDeployEvent(ev, "myapp-prod-")
	assert.False(t, ok)
}

// TestToDeployEvent_TimestampFallbacks verifies EventTime and
// CreationTimestamp fallbacks when LastTimestamp is zero.
func TestToDeployEvent_TimestampFallbacks(t *testing.T) {
	eventTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ev := watch.Event{
		Type: watch.Added,
		Object: &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				UID:             "uid-ts",
				ResourceVersion: "101",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
				Name: "myapp-prod-api-abc",
			},
			Type:      corev1.EventTypeNormal,
			Reason:    "Pulled",
			Count:     1,
			EventTime: metav1.NewMicroTime(eventTime),
			// LastTimestamp intentionally zero.
		},
	}
	got, ok := toDeployEvent(ev, "myapp-prod-")
	require.True(t, ok)
	assert.Equal(t, eventTime.UTC(), got.Timestamp.UTC())
}

// Ensure fake.Clientset satisfies runtime.Object so the test compiles.
var _ runtime.Object = (*corev1.Event)(nil)
