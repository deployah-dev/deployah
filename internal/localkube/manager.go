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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"gopherly.dev/currus"

	"deployah.dev/deployah/internal/localkube/cloudprovider"
	"deployah.dev/deployah/internal/localkube/imageref"
)

var _ io.Closer = (*Manager)(nil)

// Manager manages local Kubernetes clusters. It is safe for concurrent use.
type Manager struct {
	cfg  *config
	prov provider
	kcs  *kubeconfigStore
	eng  currus.Engine // nil when no Docker/Podman engine was found at init
	wg   sync.WaitGroup
}

// New creates a Manager with the given options.
//
// Example:
//
//	m, err := localkube.New(localkube.WithRuntime(localkube.RuntimePodman))
func New(opts ...Option) (*Manager, error) {
	cfg := &config{
		logger:    slog.Default(),
		timeout:   defaultManagerTimeout,
		runtime:   RuntimeAuto,
		backend:   BackendKind,
		eventFunc: func(Event) {},
	}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	if cfg.logger == nil {
		cfg.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	kcs, err := newKubeconfigStore(cfg.kubeconfigDir)
	if err != nil {
		return nil, fmt.Errorf("localkube: init kubeconfig store: %w", err)
	}

	prov, err := newKindProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("localkube: init kind provider: %w", err)
	}

	// currus is optional: it is only needed for the cloud provider container
	// and container inspect. A nil engine means ErrUnsupported on those methods.
	eng, engErr := currus.New(context.Background(), currus.WithLogger(cfg.logger))
	if engErr != nil {
		cfg.logger.Debug("localkube: container engine unavailable; cloud provider features disabled", "err", engErr)
	}

	return &Manager{cfg: cfg, prov: prov, kcs: kcs, eng: eng}, nil
}

// Close waits for all background goroutines to finish and releases the currus
// engine connection. It is safe to call multiple times.
//
// Example:
//
//	m, _ := localkube.New()
//	defer m.Close()
func (m *Manager) Close() error {
	m.wg.Wait()
	if m.eng != nil {
		return m.eng.Close()
	}
	return nil
}

// Create provisions a new cluster with the given name.
//
// Kind merges a "kind-<name>" context into ~/.kube/config on creation.
// A progress callback can be installed with [WithCreateEventHandler].
//
// If the context is canceled, Create returns ctx.Err() immediately. Unless
// [WithRetainOnFailure] is set, a background goroutine waits for the Kind
// operation to finish and then deletes the partial cluster. The goroutine
// is tracked in Manager.wg; call [Manager.Close] to wait for it to finish.
func (m *Manager) Create(ctx context.Context, name string, opts ...CreateOption) error {
	if err := safeClusterName(name); err != nil {
		return err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	cc := &createConfig{
		base:           m.cfg,
		waitTimeout:    defaultCreateWaitReady,
		kubeconfigPath: m.kcs.kubeconfigPath(name),
	}
	for _, o := range opts {
		if o != nil {
			o(cc)
		}
	}

	cc.emit(Event{Step: StepCreating, Status: StepStarted})

	wait, err := m.prov.create(ctx, name, cc)
	if wait == nil {
		wait = func() {}
	}
	if err != nil {
		if errors.Is(err, ErrAlreadyExists) && cc.createIfMissing {
			wait()
			cc.emit(Event{Step: StepCreating, Status: StepCompleted})
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			// Context was canceled; emit failure and schedule best-effort
			// cleanup. We must wait for the provider goroutine before deleting,
			// otherwise the delete races the in-flight create.
			cc.emit(Event{Step: StepCreating, Status: StepFailed, Detail: ctxErr.Error(), Err: ctxErr})
			if cc.retainOnFail {
				wait()
				return ctxErr
			}
			cleanupCtx := context.WithoutCancel(ctx)
			m.wg.Go(func() {
				wait() // drain Kind goroutine first
				dcfg := &deleteConfig{base: m.cfg, ignoreMissing: true, kubeconfigPath: m.kcs.kubeconfigPath(name)}
				if deleteErr := m.prov.delete(cleanupCtx, name, dcfg); deleteErr != nil {
					m.cfg.logger.Warn("localkube: cleanup after canceled create failed",
						slog.String("cluster", name), slog.Any("err", deleteErr))
				}
			})
			return ctxErr
		}
		wait()
		cc.emit(Event{Step: StepCreating, Status: StepFailed, Detail: err.Error(), Err: err})
		return fmt.Errorf("localkube: create %q: %w", name, err)
	}
	wait()

	cc.emit(Event{Step: StepCreating, Status: StepCompleted})
	return nil
}

// Delete removes the named cluster.
//
// Returns [ErrNotFound] when the cluster does not exist, unless
// [WithIgnoreMissing] is passed.
func (m *Manager) Delete(ctx context.Context, name string, opts ...DeleteOption) error {
	if err := safeClusterName(name); err != nil {
		return err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	dc := &deleteConfig{base: m.cfg, kubeconfigPath: m.kcs.kubeconfigPath(name)}
	for _, o := range opts {
		if o != nil {
			o(dc)
		}
	}

	dc.emit(Event{Step: StepDeleting, Status: StepStarted})

	err := m.prov.delete(ctx, name, dc)
	if err != nil {
		if errors.Is(err, ErrNotFound) && dc.ignoreMissing {
			dc.emit(Event{Step: StepDeleting, Status: StepCompleted})
			return nil
		}
		dc.emit(Event{Step: StepDeleting, Status: StepFailed, Detail: err.Error(), Err: err})
		return fmt.Errorf("localkube: delete %q: %w", name, err)
	}

	if removeErr := m.kcs.remove(name); removeErr != nil {
		m.cfg.logger.Warn("localkube: remove kubeconfig after delete failed",
			slog.String("cluster", name), slog.Any("err", removeErr))
	}

	dc.emit(Event{Step: StepDeleting, Status: StepCompleted})
	return nil
}

// Recreate deletes an existing cluster (if any) and creates a fresh one.
func (m *Manager) Recreate(ctx context.Context, name string, opts ...CreateOption) error {
	if err := m.Delete(ctx, name, WithIgnoreMissing()); err != nil {
		return err
	}
	return m.Create(ctx, name, opts...)
}

// Get returns the metadata for a cluster. Kind is the source of truth;
// the cluster does not need to have been created via localkube.
// Returns [ErrNotFound] if Kind does not know about this cluster.
func (m *Manager) Get(ctx context.Context, name string) (*Cluster, error) {
	if err := safeClusterName(name); err != nil {
		return nil, err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	info, err := m.prov.inspect(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("localkube: get %q: %w", name, err)
	}
	return backendInfoToCluster(name, m.prov.backendName(), info), nil
}

// List returns all clusters known to the backend, sorted by name.
//
// Kind is the source of truth; any cluster created outside localkube is also
// returned. Results are sorted by cluster name.
func (m *Manager) List(ctx context.Context) ([]*Cluster, error) {
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	names, err := m.prov.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("localkube: list: %w", err)
	}

	out := make([]*Cluster, 0, len(names))
	for _, name := range names {
		info, inspectErr := m.prov.inspect(ctx, name)
		if inspectErr != nil {
			m.cfg.logger.Warn("localkube: skip cluster during list",
				slog.String("cluster", name), slog.Any("err", inspectErr))
			continue
		}
		out = append(out, backendInfoToCluster(name, m.prov.backendName(), info))
	}
	slices.SortFunc(out, func(a, b *Cluster) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// Status returns the runtime health of a named cluster.
// A non-nil error indicates the status could not be determined; the Status
// value is still set to a best-effort estimate (e.g. [StatusStopped]).
func (m *Manager) Status(ctx context.Context, name string) (Status, error) {
	if err := safeClusterName(name); err != nil {
		return StatusUnknown, err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	st, err := m.prov.status(ctx, name)
	if err != nil {
		m.cfg.logger.Warn("localkube: status check failed",
			slog.String("cluster", name), slog.Any("err", err))
		return st, err
	}
	return st, nil
}

// KubeConfig fetches the kubeconfig for a cluster, writes it atomically to the
// deployah kubeconfig directory, and returns an immutable [KubeConfig] value.
//
// The returned KubeConfig.Path() is guaranteed to exist on disk.
func (m *Manager) KubeConfig(ctx context.Context, name string) (*KubeConfig, error) {
	if err := safeClusterName(name); err != nil {
		return nil, err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	m.cfg.eventFunc(Event{Step: StepWritingKubeconfig, Status: StepStarted})

	raw, err := m.prov.kubeConfigBytes(ctx, name)
	if err != nil {
		m.cfg.eventFunc(Event{Step: StepWritingKubeconfig, Status: StepFailed, Detail: err.Error(), Err: err})
		return nil, fmt.Errorf("localkube: kubeconfig %q: %w", name, err)
	}

	path, err := m.kcs.writeKubeconfig(name, raw)
	if err != nil {
		m.cfg.eventFunc(Event{Step: StepWritingKubeconfig, Status: StepFailed, Detail: err.Error(), Err: err})
		return nil, fmt.Errorf("localkube: write kubeconfig %q: %w", name, err)
	}

	m.cfg.eventFunc(Event{Step: StepWritingKubeconfig, Status: StepCompleted})
	return &KubeConfig{path: path, bytes: raw}, nil
}

// ContextName returns the Kubernetes context name for a cluster.
// Pure helper — performs no I/O.
func (m *Manager) ContextName(name string) string {
	return "kind-" + name
}

// StartCloudProvider starts the cloud-provider-kind container, enabling
// LoadBalancer, Ingress, and Gateway API in local Kind clusters.
//
// It is idempotent: calling it when the container is already running returns nil.
// Returns [ErrUnsupported] when no Docker or Podman engine is available (e.g.
// when the cluster is running via nerdctl or finch).
//
// Example:
//
//	if err := m.StartCloudProvider(ctx); err != nil { ... }
//	defer m.StopCloudProvider(ctx)
func (m *Manager) StartCloudProvider(ctx context.Context, opts ...CloudProviderOption) error {
	ctrl, err := m.cloudProviderController(opts...)
	if err != nil {
		return err
	}
	m.cfg.eventFunc(Event{Step: StepStartingCloudProvider, Status: StepStarted})
	if startErr := ctrl.Start(ctx); startErr != nil {
		m.cfg.eventFunc(Event{Step: StepStartingCloudProvider, Status: StepFailed, Detail: startErr.Error(), Err: startErr})
		return fmt.Errorf("localkube: start cloud provider: %w", startErr)
	}
	m.cfg.eventFunc(Event{Step: StepStartingCloudProvider, Status: StepCompleted})
	return nil
}

// StopCloudProvider removes the cloud-provider-kind container.
//
// It is idempotent: calling it when no container is running returns nil.
func (m *Manager) StopCloudProvider(ctx context.Context) error {
	ctrl, err := m.cloudProviderController()
	if err != nil {
		return err
	}
	m.cfg.eventFunc(Event{Step: StepStoppingCloudProvider, Status: StepStarted})
	if stopErr := ctrl.Stop(ctx); stopErr != nil {
		m.cfg.eventFunc(Event{Step: StepStoppingCloudProvider, Status: StepFailed, Detail: stopErr.Error(), Err: stopErr})
		return fmt.Errorf("localkube: stop cloud provider: %w", stopErr)
	}
	m.cfg.eventFunc(Event{Step: StepStoppingCloudProvider, Status: StepCompleted})
	return nil
}

// CloudProviderRunning reports whether the cloud-provider-kind container is
// currently running. Returns false when no container is found or the engine
// is unavailable.
func (m *Manager) CloudProviderRunning(ctx context.Context) bool {
	ctrl, err := m.cloudProviderController()
	if err != nil {
		return false
	}
	return ctrl.Running(ctx)
}

// AttachCloudProvider starts the cloud-provider-kind container (if not already
// running) and streams its logs until ctx is canceled, then stops the container.
//
// By default logs are written to [os.Stderr]. Use [WithAttachWriter] to redirect
// them to any [io.Writer].
//
// Use this for the foreground "deployah cluster up --attach" workflow.
//
// Example:
//
//	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
//	defer stop()
//	_ = m.AttachCloudProvider(ctx)
//
// Redirect logs to a file:
//
//	_ = m.AttachCloudProvider(ctx, localkube.WithAttachWriter(logFile))
func (m *Manager) AttachCloudProvider(ctx context.Context, opts ...CloudProviderOption) error {
	if m.eng == nil {
		return fmt.Errorf("%w: cloud provider requires Docker or Podman", ErrUnsupported)
	}
	cpCfg := defaultCloudProviderConfig()
	for _, o := range opts {
		if o != nil {
			o(cpCfg)
		}
	}
	ctrl := cloudprovider.New(m.eng, cpCfg.toControllerConfig())

	if startErr := ctrl.Start(ctx); startErr != nil {
		return fmt.Errorf("localkube: start cloud provider: %w", startErr)
	}

	out := cpCfg.attachWriter
	if out == nil {
		out = os.Stderr
	}
	// Stream logs until ctx is canceled; a nil error or context-cancel is normal.
	if attachErr := ctrl.Attach(ctx, out); attachErr != nil {
		m.cfg.logger.Debug("localkube: cloud provider log stream ended with error", "err", attachErr)
	}
	// Use a fresh context because the original is canceled.
	stopCtx := context.WithoutCancel(ctx)
	if stopErr := ctrl.Stop(stopCtx); stopErr != nil {
		m.cfg.logger.Warn("localkube: stop cloud provider after attach failed", slog.Any("err", stopErr))
	}
	return nil
}

// LoadImage resolves an image reference, fetches it (from local daemon,
// registry, or file), and loads it into the named cluster.
//
// Resolution order: file path → local daemon → remote registry.
// For low-level loading from an existing reader, use [Manager.LoadImageArchive].
func (m *Manager) LoadImage(ctx context.Context, clusterName, imageRef string) error {
	if err := safeClusterName(clusterName); err != nil {
		return err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()

	m.cfg.eventFunc(Event{Step: StepResolvingImage, Status: StepStarted, Detail: imageRef})

	rc, err := imageref.Open(ctx, imageRef)
	if err != nil {
		m.cfg.eventFunc(Event{Step: StepResolvingImage, Status: StepFailed, Detail: err.Error(), Err: err})
		return fmt.Errorf("localkube: resolve image %q: %w", imageRef, err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			m.cfg.logger.Warn("localkube: close image reader failed",
				slog.String("image", imageRef), slog.Any("err", closeErr))
		}
	}()

	m.cfg.eventFunc(Event{Step: StepResolvingImage, Status: StepCompleted, Detail: imageRef})
	return m.loadImageArchiveInternal(ctx, clusterName, rc)
}

// LoadImageArchive loads a Docker/OCI tar archive into the named cluster.
// The archive is read until EOF; the caller is responsible for closing it.
func (m *Manager) LoadImageArchive(ctx context.Context, clusterName string, archive io.Reader) error {
	if err := safeClusterName(clusterName); err != nil {
		return err
	}
	ctx, cancel := contextWithBudget(ctx, m.cfg.timeout)
	defer cancel()
	return m.loadImageArchiveInternal(ctx, clusterName, archive)
}

func (m *Manager) loadImageArchiveInternal(ctx context.Context, clusterName string, archive io.Reader) error {
	m.cfg.eventFunc(Event{Step: StepLoadingImage, Status: StepStarted})

	if err := m.prov.loadImageArchive(ctx, clusterName, archive); err != nil {
		m.cfg.eventFunc(Event{Step: StepLoadingImage, Status: StepFailed, Detail: err.Error(), Err: err})
		return fmt.Errorf("localkube: load image archive into %q: %w", clusterName, err)
	}

	m.cfg.eventFunc(Event{Step: StepLoadingImage, Status: StepCompleted})
	return nil
}

// cloudProviderController creates a controller for the cloud-provider-kind
// container, applying any provided options. Returns ErrUnsupported when no
// Docker/Podman engine is available.
func (m *Manager) cloudProviderController(opts ...CloudProviderOption) (*cloudprovider.Controller, error) {
	if m.eng == nil {
		return nil, fmt.Errorf("%w: cloud provider requires Docker or Podman", ErrUnsupported)
	}
	cfg := defaultCloudProviderConfig()
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	return cloudprovider.New(m.eng, cfg.toControllerConfig()), nil
}

// contextWithBudget wraps ctx with a timeout only when the caller has not
// already set a deadline, protecting against runaway operations.
func contextWithBudget(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, budget)
}

// backendInfoToCluster converts a provider name + backendInfo to a public Cluster.
func backendInfoToCluster(name, backendName string, info *backendInfo) *Cluster {
	c := &Cluster{
		Name:    name,
		Backend: backendName,
	}
	if info != nil {
		c.Nodes = info.Nodes
		c.Roles = info.Roles
		c.Runtime = info.Runtime
		c.CreatedAt = info.CreatedAt
	}
	return c
}
