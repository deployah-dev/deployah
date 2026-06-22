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
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolswatch "k8s.io/client-go/tools/watch"
)

// DeployEvent carries a single Kubernetes event relevant to a deploy.
// It is a thin translation of a Kubernetes Event object with only the
// fields the deploy watcher needs for rendering and deduplication.
type DeployEvent struct {
	// UID is the Kubernetes object UID of the source event.
	UID types.UID
	// Type is the event severity, either "Normal" or "Warning".
	Type string
	// Reason is the short CamelCase reason string
	// (Scheduled, Pulled, BackOff, etc.).
	Reason string
	// Message is the human-readable event description.
	Message string
	// Object is the formatted involvedObject reference,
	// for example "pod/api-7d9abc" or "replicaset/api-5f123".
	Object string
	// Count is the number of times this event has occurred.
	Count int32
	// Timestamp is the last time the event was observed.
	Timestamp time.Time
}

// releaseWatcherClient adapts the Kubernetes events client into a
// [cache.WatcherWithContext] for use with RetryWatcher.
type releaseWatcherClient struct {
	client    kubernetes.Interface
	namespace string
}

func (r releaseWatcherClient) WatchWithContext(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return r.client.CoreV1().Events(r.namespace).Watch(ctx, opts)
}

// WatchDeployEvents watches Kubernetes events in namespace and returns
// a channel of [DeployEvent] values filtered to resources whose name
// starts with releasePrefix. The channel is closed when ctx is
// canceled.
//
// It lists existing events to obtain the initial resourceVersion, then
// uses a RetryWatcher for automatic reconnection on transient API
// failures. The background goroutine stops when ctx is canceled; the
// returned channel is closed once the goroutine exits.
func WatchDeployEvents(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, releasePrefix string,
) (<-chan DeployEvent, error) {
	// List to get the current resourceVersion. We do not process
	// existing events, only new ones arriving after the deploy starts.
	list, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list events in %q: %w", namespace, err)
	}

	initialRV := list.ResourceVersion
	if initialRV == "" || initialRV == "0" {
		// Fall back to a safe value so RetryWatcher does not reject it.
		initialRV = "1"
	}

	wc := releaseWatcherClient{client: client, namespace: namespace}
	rw, err := toolswatch.NewRetryWatcherWithContext(ctx, initialRV, wc)
	if err != nil {
		return nil, fmt.Errorf("create retry watcher: %w", err)
	}

	ch := make(chan DeployEvent, 32)
	prefix := releasePrefix + "-"

	go func() {
		defer close(ch)
		defer rw.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-rw.ResultChan():
				if !ok {
					return
				}
				if ev.Type == watch.Error {
					continue
				}
				de, ok := toDeployEvent(ev, prefix)
				if !ok {
					continue
				}
				select {
				case ch <- de:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// toDeployEvent converts a watch event into a [DeployEvent].
// It returns false when the event does not match the release prefix
// or cannot be type-asserted to a core Event.
func toDeployEvent(ev watch.Event, prefix string) (DeployEvent, bool) {
	coreEv, ok := ev.Object.(*corev1.Event)
	if !ok {
		return DeployEvent{}, false
	}

	if !strings.HasPrefix(coreEv.InvolvedObject.Name, prefix) {
		return DeployEvent{}, false
	}

	ts := coreEv.LastTimestamp.Time
	if ts.IsZero() {
		ts = coreEv.EventTime.Time
	}
	if ts.IsZero() {
		ts = coreEv.CreationTimestamp.Time
	}

	kind := strings.ToLower(coreEv.InvolvedObject.Kind)
	name := coreEv.InvolvedObject.Name
	object := kind + "/" + name

	count := coreEv.Count
	if count == 0 {
		count = 1
	}

	return DeployEvent{
		UID:       coreEv.UID,
		Type:      coreEv.Type,
		Reason:    coreEv.Reason,
		Message:   coreEv.Message,
		Object:    object,
		Count:     count,
		Timestamp: ts,
	}, true
}
