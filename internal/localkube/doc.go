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

// Package localkube manages local Kubernetes clusters for development use.
//
// It wraps the Kind backend (sigs.k8s.io/kind) and supports Docker, Podman
// (daemonless, rootless), and nerdctl as host container engines. The active
// backend is selected at construction time via [WithBackend]; the default is
// [BackendKind]. Future backends can be added as additional [Backend] constants
// without breaking callers.
//
// # Basic usage
//
//	m, err := localkube.New()
//	if err != nil { ... }
//	defer m.Close()
//
//	if err := m.Create(ctx, "dev"); err != nil { ... }
//	defer m.Delete(ctx, "dev", localkube.WithIgnoreMissing())
//
//	kc, err := m.KubeConfig(ctx, "dev")
//	_ = kc.Path() // stable path on disk
//
// # Image loading
//
// Two methods are available:
//
//   - [Manager.LoadImage] resolves an image reference (daemon image, remote
//     registry, or local .tar file) and pipes the result into
//     [Manager.LoadImageArchive].
//   - [Manager.LoadImageArchive] is the low-level primitive that accepts any
//     [io.Reader] of a Docker/OCI tar archive and loads it into every cluster
//     node.
//
// # Cloud provider (LoadBalancer, Ingress, Gateway API)
//
// The optional cloud provider runs the cloud-provider-kind image as a managed
// container via the [gopherly.dev/currus] engine abstraction. This avoids
// in-process global state mutation and works without elevated privileges on
// Linux.
//
//	m, _ := localkube.New()
//	defer m.Close()
//
//	// Non-blocking: start in background, stop when done.
//	if err := m.StartCloudProvider(ctx); err != nil { ... }
//	defer m.StopCloudProvider(ctx)
//	running := m.CloudProviderRunning(ctx)
//
//	// Foreground: blocks until ctx is canceled, then stops the container.
//	// Logs go to os.Stderr by default; pass WithAttachWriter to redirect them.
//	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
//	defer stop()
//	_ = m.AttachCloudProvider(ctx) // logs to os.Stderr
//	_ = m.AttachCloudProvider(ctx, localkube.WithAttachWriter(w))
//
// Docker and Podman are fully supported. Returns [ErrUnsupported] for
// nerdctl/finch (cluster create/delete/kubeconfig still work on all engines).
//
// # Concurrency
//
// [Manager] is safe for concurrent use. Its config is immutable after [New].
// [Manager.StartCloudProvider] and [Manager.StopCloudProvider] are idempotent:
// calling Start while the container is already running is a no-op, and Stop
// when no container exists is also a no-op.
//
// # Cancellation
//
// Kind's Go API does not accept a [context.Context]. When a context passed to
// [Manager.Create] is canceled:
//
//   - Create returns immediately with ctx.Err().
//   - Unless [WithRetainOnFailure] is set, a background goroutine waits for
//     Kind to finish and then deletes the partial cluster.
//   - Call [Manager.Close] to wait for the goroutine to finish before exit.
//
// # File-on-disk side effects
//
// Create writes to one deployah-managed location:
//
//  1. Kubeconfig copy under XDG StateHome at
//     deployah/localkube/kubeconfigs/<name>.yaml: written by
//     [Manager.KubeConfig], accessible via [KubeConfig.Path].
//
// Kind itself writes to two additional locations:
//
//  2. User's ~/.kube/config: Kind merges a "kind-<name>" context on Create and
//     removes it on Delete.
//
//  3. Container labels on Kind's node containers: visible to "kind get clusters"
//     and "docker ps". Kind is the single source of truth for cluster existence.
package localkube
