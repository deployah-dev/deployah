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

package localkube

import (
	"context"
	"io"
	"time"
)

// provider is the seam between Manager and backend implementations.
// All method names are unexported; Manager is the only caller.
//
// To select a backend, pass [WithBackend] to [New]. The active backend is
// resolved at construction time; it is not pluggable at runtime by external
// packages. All backends known to this module are listed in the [Backend]
// constants; the provider interface is an unexported implementation detail.
type provider interface {
	// create provisions a new cluster. Returns ErrAlreadyExists if the name
	// is already taken by this backend.
	//
	// The returned wait function blocks until any background goroutine spawned
	// by the provider has finished. It is always non-nil. If the context is
	// canceled before the cluster is ready, create returns ctx.Err() and the
	// caller must invoke wait() before attempting a delete, so the two
	// operations don't race.
	create(ctx context.Context, name string, cfg *createConfig) (wait func(), err error)

	// backendName returns the identifier for this backend (e.g. "kind").
	backendName() string

	// delete removes a cluster. Returns ErrNotFound if the name is unknown.
	delete(ctx context.Context, name string, cfg *deleteConfig) error

	// list returns all cluster names known to this backend.
	list(ctx context.Context) ([]string, error)

	// inspect returns metadata for a named cluster.
	// Returns ErrNotFound if the cluster does not exist in the backend.
	inspect(ctx context.Context, name string) (*backendInfo, error)

	// status probes a cluster's runtime health.
	// Returns ErrNotFound if the cluster does not exist.
	status(ctx context.Context, name string) (Status, error)

	// kubeConfigBytes returns the raw kubeconfig YAML for a cluster.
	// Returns ErrNotFound if the cluster does not exist.
	kubeConfigBytes(ctx context.Context, name string) ([]byte, error)

	// loadImageArchive streams an OCI/Docker tar archive into every node of
	// the named cluster. Ref-resolution is handled upstream by Manager.
	// Returns ErrNotFound when no nodes exist for the named cluster.
	loadImageArchive(ctx context.Context, name string, archive io.Reader) error
}

// backendInfo carries metadata that a provider can supply for a cluster.
type backendInfo struct {
	// Nodes is the total node count (control-plane + workers).
	Nodes int
	// Roles counts nodes by role (e.g. "control-plane": 1, "worker": 2).
	// May be nil or incomplete if the backend cannot determine roles.
	Roles map[string]int
	// Runtime is the resolved host container engine (never RuntimeAuto here).
	Runtime Runtime
	// CreatedAt is when the first node container was started; may be zero if
	// the backend cannot determine it without an additional inspect call.
	CreatedAt time.Time
}
