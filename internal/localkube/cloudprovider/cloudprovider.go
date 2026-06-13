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

package cloudprovider

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gopherly.dev/currus"
)

const (
	// DefaultImage is the pinned cloud-provider-kind image used when no image
	// override is provided.
	DefaultImage = "registry.k8s.io/cloud-provider-kind/cloud-controller-manager:v0.10.0"

	// containerName is the well-known name for the managed container.
	containerName = "deployah-cloud-provider"

	// ownerLabel and ownerValue mark containers managed by this package.
	ownerLabel = "deployah.dev/cloud-provider"
	ownerValue = "true"

	// gatewayClusterLabel is set by cloud-provider-kind on gateway sidecar
	// containers (envoy proxies). The value is the Kind cluster name.
	gatewayClusterLabel = "io.x-k8s.cloud-provider-kind.cluster"
)

// ErrUnsupported is returned by Start/Stop/Running when the underlying engine
// does not support the Docker Engine API (e.g. plain containerd).
var ErrUnsupported = errors.New("cloud provider requires Docker or Podman")

// ErrRootlessUnsupported is returned by Start when the container engine is
// running in rootless mode. cloud-provider-kind bind-mounts the Docker daemon
// socket into a sidecar container, which does not work with rootless Docker or
// Podman due to user namespace isolation.
var ErrRootlessUnsupported = errors.New("cloud provider: rootless container engine detected; " +
	"LoadBalancer, Ingress, and Gateway API require a rootful daemon")

// errContainerNotFound is returned by findContainer when no managed
// container exists.
var errContainerNotFound = errors.New("container not found")

// Config holds the runtime configuration for the cloud-provider-kind container.
type Config struct {
	// Image is the container image to run. Defaults to DefaultImage.
	Image string

	// Network is the container network to join. Defaults to "kind".
	Network string

	// GatewayChannel selects the Gateway API release channel
	// ("standard", "experimental", or "disabled").
	GatewayChannel string

	// IngressDefault enables the default IngressClass. Defaults to true.
	IngressDefault bool

	// SocketPath is the host-side docker socket to bind-mount into the
	// container. If empty, it is derived from eng.Endpoint().Host.
	SocketPath string

	// ClusterName is the Kind cluster name. When set, Stop also removes
	// gateway sidecar containers (envoy proxies) spawned by
	// cloud-provider-kind for this cluster.
	ClusterName string
}

func (c *Config) image() string {
	if c.Image != "" {
		return c.Image
	}
	return DefaultImage
}

func (c *Config) network() string {
	if c.Network != "" {
		return c.Network
	}
	return "kind"
}

// buildArgs converts the config fields into container command-line arguments.
func (c *Config) buildArgs() []string {
	// --enable-lb-port-mapping publishes service ports from the envoy gateway
	// container to the host, making services reachable via localhost:<port> on
	// both Linux and macOS (including Docker-in-VM setups like Lima).
	args := []string{"--enable-lb-port-mapping"}
	if c.GatewayChannel != "" && c.GatewayChannel != "standard" {
		args = append(args, "--gateway-api-channel="+c.GatewayChannel)
	}
	if !c.IngressDefault {
		args = append(args, "--enable-ingress-default=false")
	}
	return args
}

// Controller manages the lifecycle of the cloud-provider-kind container.
type Controller struct {
	eng currus.Engine
	cfg Config
}

// New returns a Controller that uses eng to manage the cloud-provider-kind
// container with the given configuration.
func New(eng currus.Engine, cfg Config) *Controller {
	return &Controller{eng: eng, cfg: cfg}
}

// Start ensures the cloud-provider-kind container is running. It is idempotent:
// if the container is already running, Start returns nil.
func (c *Controller) Start(ctx context.Context) error {
	if c.eng.Capabilities().Rootless {
		return ErrRootlessUnsupported
	}

	existing, err := c.findContainer(ctx)
	if err != nil && !errors.Is(err, errContainerNotFound) {
		return fmt.Errorf("cloud provider: find container: %w", err)
	}
	if existing != nil {
		if existing.State == "running" {
			return nil // already running
		}
		// Stale stopped container — remove and recreate.
		if removeErr := c.eng.RemoveContainer(ctx, existing.ID, currus.RemoveContainerOpts{Force: true}); removeErr != nil {
			return fmt.Errorf("cloud provider: remove stale container: %w", removeErr)
		}
	}

	if pullErr := c.eng.PullImage(ctx, c.cfg.image(), currus.PullImageOpts{}); pullErr != nil {
		return fmt.Errorf("cloud provider: pull image: %w", pullErr)
	}

	socketPath, err := c.socketPath()
	if err != nil {
		return err
	}

	spec := currus.ContainerSpec{
		Image:   c.cfg.image(),
		Name:    containerName,
		Restart: currus.RestartPolicy{Mode: currus.RestartUnlessStopped},
		Labels: map[string]string{
			ownerLabel: ownerValue,
		},
		Mounts: []currus.Mount{
			{
				Type:   currus.MountTypeBind,
				Source: socketPath,
				Target: "/var/run/docker.sock",
			},
		},
		Networks: []currus.NetworkAttachment{{Name: c.cfg.network()}},
		Args:     c.cfg.buildArgs(),
		// Run as root so the container can access the Docker socket regardless
		// of how the host socket is mounted (e.g. via Lima's file sharing layer,
		// which loses the original socket ownership and makes it nobody:nobody).
		Security: currus.Security{User: "0"},
	}

	id, err := c.eng.CreateContainer(ctx, spec)
	if err != nil {
		return fmt.Errorf("cloud provider: create container: %w", err)
	}
	if startErr := c.eng.StartContainer(ctx, id); startErr != nil {
		// Best-effort cleanup of the created but unstarted container.
		_ = c.eng.RemoveContainer(context.WithoutCancel(ctx), id, currus.RemoveContainerOpts{Force: true}) //nolint:errcheck
		return fmt.Errorf("cloud provider: start container: %w", startErr)
	}
	return nil
}

// Stop removes the cloud-provider-kind container and any gateway sidecar
// containers it spawned. It is idempotent: if no containers are found, Stop
// returns nil.
func (c *Controller) Stop(ctx context.Context) error {
	existing, err := c.findContainer(ctx)
	if err != nil && !errors.Is(err, errContainerNotFound) {
		return fmt.Errorf("cloud provider: find container: %w", err)
	}
	if existing != nil {
		if removeErr := c.eng.RemoveContainer(ctx, existing.ID, currus.RemoveContainerOpts{Force: true}); removeErr != nil {
			return fmt.Errorf("cloud provider: remove container: %w", removeErr)
		}
	}
	if c.cfg.ClusterName != "" {
		if gwErr := c.removeGatewayContainers(ctx); gwErr != nil {
			return fmt.Errorf("cloud provider: remove gateway containers: %w", gwErr)
		}
	}
	return nil
}

// Running reports whether the cloud-provider-kind container is currently
// running. It returns false if no container is found or if detection fails.
func (c *Controller) Running(ctx context.Context) bool {
	existing, err := c.findContainer(ctx)
	if err != nil || existing == nil {
		return false
	}
	return existing.State == "running"
}

// Attach streams the cloud-provider-kind container logs to out until ctx is
// canceled. The container must already be running (call Start beforehand).
// If the engine does not support log streaming, Attach blocks until ctx is
// canceled without producing any output.
func (c *Controller) Attach(ctx context.Context, out io.Writer) error {
	existing, err := c.findContainer(ctx)
	if err != nil {
		if errors.Is(err, errContainerNotFound) {
			return fmt.Errorf("cloud provider: container not found; call Start first")
		}
		return fmt.Errorf("cloud provider: find container: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("cloud provider: container not found; call Start first")
	}

	lg, ok := c.eng.(currus.Logger)
	if !ok {
		// Engine doesn't support log streaming; just wait for ctx to be done.
		<-ctx.Done()
		return nil
	}

	rc, err := lg.ContainerLogs(ctx, existing.ID, currus.ContainerLogsOpts{Follow: true})
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("cloud provider: container logs: %w", err)
	}
	defer rc.Close() //nolint:errcheck

	_, copyErr := io.Copy(out, rc)
	if ctx.Err() != nil {
		return nil // normal cancellation
	}
	return copyErr
}

// removeGatewayContainers finds and removes all gateway sidecar containers
// (envoy proxies) spawned by cloud-provider-kind for the configured cluster.
func (c *Controller) removeGatewayContainers(ctx context.Context) error {
	all, err := c.eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	if err != nil {
		return err
	}
	for i := range all {
		if all[i].Labels[gatewayClusterLabel] == c.cfg.ClusterName {
			if removeErr := c.eng.RemoveContainer(ctx, all[i].ID, currus.RemoveContainerOpts{Force: true}); removeErr != nil {
				return removeErr
			}
		}
	}
	return nil
}

// GatewayPorts returns the published host ports from gateway envoy containers
// for the configured cluster. The map keys are container ports (e.g. 80); the
// values are the corresponding host ports assigned by Docker (e.g. 32769).
// Returns nil when ClusterName is empty, no gateway containers are found, or
// the engine does not support container inspection.
func (c *Controller) GatewayPorts(ctx context.Context) map[uint16]uint16 {
	if c.cfg.ClusterName == "" {
		return nil
	}
	ins, ok := c.eng.(currus.Inspector)
	if !ok {
		return nil
	}
	all, err := c.eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	if err != nil {
		return nil
	}
	ports := make(map[uint16]uint16)
	for i := range all {
		if all[i].Labels[gatewayClusterLabel] != c.cfg.ClusterName {
			continue
		}
		info, inspectErr := ins.Inspect(ctx, all[i].ID)
		if inspectErr != nil {
			continue
		}
		for _, p := range info.Ports {
			if p.Host != 0 {
				ports[p.Container] = p.Host
			}
		}
	}
	if len(ports) == 0 {
		return nil
	}
	return ports
}

// findContainer returns the cloud-provider-kind container if it exists,
// or nil if it does not. It filters by the owner label.
func (c *Controller) findContainer(ctx context.Context) (*currus.Container, error) {
	all, err := c.eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Labels[ownerLabel] == ownerValue {
			return &all[i], nil
		}
	}
	return nil, errContainerNotFound
}

// socketPath returns the bind-mountable daemon socket path. It uses
// Endpoint.DaemonSocket which currus auto-resolves for VM-based setups
// (Lima, Colima, Docker Desktop, OrbStack). SocketPath in config overrides
// auto-detection as an escape hatch.
func (c *Controller) socketPath() (string, error) {
	if c.cfg.SocketPath != "" {
		return c.cfg.SocketPath, nil
	}
	if er, ok := c.eng.(currus.EndpointReporter); ok {
		if sock := er.Endpoint().DaemonSocket; sock != "" {
			return sock, nil
		}
	}
	// Default to the standard Docker socket.
	return "/var/run/docker.sock", nil
}
