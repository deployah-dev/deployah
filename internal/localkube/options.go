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
	"io"
	"log/slog"
	"time"
)

// Option configures a [Manager] at construction time via [New].
type Option func(*config)

// WithLogger sets the logger used by the Manager and its underlying provider.
// Passing nil silently discards all log output.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithKubernetesVersion pins the Kubernetes version for new clusters
// (e.g. "1.31" or "v1.31.2").
// The Kind provider maps this to the corresponding kindest/node image tag.
func WithKubernetesVersion(v string) Option {
	return func(c *config) { c.k8sVersion = v }
}

// WithRuntime forces a specific host container engine.
// Defaults to [RuntimeAuto] which lets Kind detect the available engine.
func WithRuntime(r Runtime) Option {
	return func(c *config) { c.runtime = r }
}

// WithBackend selects the cluster provisioning backend.
// Currently only [BackendKind] is supported. This option exists so callers can
// express intent explicitly and the API can accommodate future backends without
// a breaking change.
func WithBackend(b Backend) Option {
	return func(c *config) { c.backend = b }
}

// WithTimeout sets the per-method operation budget.
// Methods called with a context that already has a deadline respect that
// deadline instead.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// WithEventHandler registers a manager-level event callback. Individual Create
// calls may override this with their own handler via [WithCreateEventHandler].
func WithEventHandler(fn func(Event)) Option {
	return func(c *config) { c.eventFunc = fn }
}

// WithKubeconfigDir overrides the default XDG-based directory used for
// kubeconfig copies written by [Manager.KubeConfig].
// Intended for testing; production code should rely on the default.
func WithKubeconfigDir(dir string) Option {
	return func(c *config) { c.kubeconfigDir = dir }
}

// WithSpoolDir overrides the directory used for temporary image-archive files
// during [Manager.LoadImage] and [Manager.LoadImageArchive]. When empty
// (the default), [os.TempDir]() is used, which on many Linux systems (including
// NixOS) is backed by tmpfs/RAM. For large images, set TMPDIR or use this
// option to point at a persistent, disk-backed directory.
func WithSpoolDir(dir string) Option {
	return func(c *config) { c.spoolDir = dir }
}

// CreateOption configures a single [Manager.Create] call.
type CreateOption func(*createConfig)

// WithCreateEventHandler overrides the manager-level event handler for this
// Create call only.
func WithCreateEventHandler(fn func(Event)) CreateOption {
	return func(c *createConfig) { c.onEvent = fn }
}

// WithWaitTimeout sets how long Create waits for cluster nodes to become Ready.
// Defaults to [defaultCreateWaitReady].
func WithWaitTimeout(d time.Duration) CreateOption {
	return func(c *createConfig) { c.waitTimeout = d }
}

// WithRetainOnFailure keeps the partially-created cluster when Create fails
// or is canceled. Useful for post-mortem debugging.
func WithRetainOnFailure(retain bool) CreateOption {
	return func(c *createConfig) { c.retainOnFail = retain }
}

// WithPortMappings adds host-to-container port mappings to the cluster nodes.
// Ignored if [WithKindConfig] is also set (raw config takes priority).
func WithPortMappings(pms ...PortMapping) CreateOption {
	return func(c *createConfig) { c.portMappings = append(c.portMappings, pms...) }
}

// WithKindConfig supplies a raw Kind cluster config YAML.
// When set, it takes priority over [WithPortMappings]. A warning is logged
// if both are provided.
func WithKindConfig(raw []byte) CreateOption {
	return func(c *createConfig) { c.rawKindConfig = raw }
}

// WithCreateIfMissing makes Create a no-op (returning nil) when a cluster with
// the same name already exists, instead of returning [ErrAlreadyExists].
func WithCreateIfMissing() CreateOption {
	return func(c *createConfig) { c.createIfMissing = true }
}

// CloudProviderOption configures the cloud-provider-kind container started by
// [Manager.StartCloudProvider] or [Manager.AttachCloudProvider].
type CloudProviderOption func(*cloudProviderConfig)

// WithGatewayAPI selects the Gateway API release channel for cloud-provider-kind.
// Default: [GatewayStandard].
func WithGatewayAPI(ch GatewayChannel) CloudProviderOption {
	return func(c *cloudProviderConfig) { c.gatewayChannel = ch }
}

// WithIngressDefault toggles cloud-provider-kind's default ingress class.
// Default: true.
func WithIngressDefault(enabled bool) CloudProviderOption {
	return func(c *cloudProviderConfig) { c.ingressDefault = enabled }
}

// WithCloudProviderImage overrides the cloud-provider-kind container image.
// Use this to pin a specific version or test a local build.
func WithCloudProviderImage(image string) CloudProviderOption {
	return func(c *cloudProviderConfig) { c.image = image }
}

// WithCloudProviderSocket overrides the host engine socket bind-mounted into
// the cloud-provider container. When empty, the socket is derived from the
// currus engine endpoint (unix:// path). Set this when the engine listens on
// a non-standard socket path.
func WithCloudProviderSocket(socketPath string) CloudProviderOption {
	return func(c *cloudProviderConfig) { c.socketPath = socketPath }
}

// WithAttachWriter sets the [io.Writer] that [Manager.AttachCloudProvider]
// streams container logs to. Defaults to [os.Stderr] when not set.
func WithAttachWriter(w io.Writer) CloudProviderOption {
	return func(c *cloudProviderConfig) { c.attachWriter = w }
}

// WithClusterName sets the Kind cluster name so that [Manager.StopCloudProvider]
// also removes gateway sidecar containers spawned by cloud-provider-kind.
func WithClusterName(name string) CloudProviderOption {
	return func(c *cloudProviderConfig) { c.clusterName = name }
}

// DeleteOption configures a single [Manager.Delete] call.
type DeleteOption func(*deleteConfig)

// WithIgnoreMissing makes Delete return nil when the named cluster does not
// exist, instead of returning [ErrNotFound].
func WithIgnoreMissing() DeleteOption {
	return func(c *deleteConfig) { c.ignoreMissing = true }
}

// WithDeleteEventHandler overrides the manager-level event handler for this
// Delete call only, analogous to [WithCreateEventHandler].
func WithDeleteEventHandler(fn func(Event)) DeleteOption {
	return func(c *deleteConfig) { c.onEvent = fn }
}
