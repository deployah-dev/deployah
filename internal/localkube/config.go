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

	"deployah.dev/deployah/internal/localkube/cloudprovider"
)

// config holds manager-level settings, all read-only after New() returns.
type config struct {
	logger        *slog.Logger
	k8sVersion    string        // resolved per-backend; empty → use provider default
	timeout       time.Duration // budget per public method
	runtime       Runtime
	backend       Backend
	eventFunc     func(Event) // default: no-op, so providers need not nil-check
	kubeconfigDir string      // overridable via WithKubeconfigDir for tests
	spoolDir      string      // directory for image-archive temp files; empty → os.TempDir()
}

// createConfig collects the options for a single Create call.
type createConfig struct {
	base            *config
	onEvent         func(Event)   // per-call override for the manager-level event handler
	waitTimeout     time.Duration // how long to wait for nodes to become Ready
	retainOnFail    bool          // keep the partial cluster on failure
	portMappings    []PortMapping
	rawKindConfig   []byte // WithKindConfig escape hatch; takes priority over portMappings
	createIfMissing bool   // swallow ErrAlreadyExists when cluster already exists
	kubeconfigPath  string // path for Kind to write its kubeconfig (never touches ~/.kube/config)
}

// emit fires ev on both the per-call handler (if set) and the manager-level
// default, so debug logging and per-operation callbacks both receive every event.
func (c *createConfig) emit(ev Event) {
	if c.onEvent != nil {
		c.onEvent(ev)
	}
	c.base.eventFunc(ev)
}

// cloudProviderConfig holds settings for the cloud-provider-kind container.
// The container is started and stopped by Manager.StartCloudProvider /
// Manager.StopCloudProvider via the cloudprovider subpackage.
type cloudProviderConfig struct {
	image          string         // default: cloudprovider.DefaultImage
	network        string         // default: "kind"
	gatewayChannel GatewayChannel // default: GatewayStandard
	ingressDefault bool           // default: true
	socketPath     string         // host socket override; empty → auto-detect
	attachWriter   io.Writer      // destination for AttachCloudProvider log stream; nil → os.Stderr
	clusterName    string         // Kind cluster name; enables gateway container cleanup on Stop
}

// defaultCloudProviderConfig returns a ready-to-use cloud provider config.
func defaultCloudProviderConfig() *cloudProviderConfig {
	return &cloudProviderConfig{
		image:          cloudprovider.DefaultImage,
		network:        "kind",
		gatewayChannel: GatewayStandard,
		ingressDefault: true,
	}
}

// toControllerConfig converts an internal cloudProviderConfig to the
// cloudprovider subpackage Config used by the container controller.
func (c *cloudProviderConfig) toControllerConfig() cloudprovider.Config {
	return cloudprovider.Config{
		Image:          c.image,
		Network:        c.network,
		GatewayChannel: string(c.gatewayChannel),
		IngressDefault: c.ingressDefault,
		SocketPath:     c.socketPath,
		ClusterName:    c.clusterName,
	}
}

// deleteConfig collects the options for a single Delete call.
type deleteConfig struct {
	base           *config
	onEvent        func(Event) // per-call override for the manager-level event handler
	ignoreMissing  bool        // swallow ErrNotFound
	kubeconfigPath string      // path passed to Kind's Delete to keep context cleanup isolated
}

// emit fires ev on both the per-call handler (if set) and the manager-level
// default, so debug logging and per-operation callbacks both receive every event.
func (c *deleteConfig) emit(ev Event) {
	if c.onEvent != nil {
		c.onEvent(ev)
	}
	c.base.eventFunc(ev)
}
