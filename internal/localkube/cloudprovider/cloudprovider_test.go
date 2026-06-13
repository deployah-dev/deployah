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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopherly.dev/currus"
	"gopherly.dev/currus/currustest"
)

// TestStart_RunsAsRoot verifies that the cloud-provider-kind container is
// created with User "0" so it can access the Docker socket even when the
// socket is mounted via a file-sharing layer (e.g. Lima) that loses ownership.
func TestStart_RunsAsRoot(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	ctrl := New(eng, Config{})
	require.NoError(t, ctrl.Start(ctx))

	containers, err := eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	require.NoError(t, err)
	require.Len(t, containers, 1)

	info, err := eng.Inspect(ctx, containers[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "0", info.Security.User)
}

// TestStart_RejectsRootless verifies that Start returns ErrRootlessUnsupported
// when the engine is running in rootless mode, and that no containers are
// created before the check.
func TestStart_RejectsRootless(t *testing.T) {
	eng := currustest.New(currustest.WithCaps(currus.Caps{Rootless: true}))
	ctx := context.Background()

	ctrl := New(eng, Config{})
	err := ctrl.Start(ctx)

	require.ErrorIs(t, err, ErrRootlessUnsupported)

	containers, listErr := eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	require.NoError(t, listErr)
	assert.Empty(t, containers)
}

// TestStop_RemovesGatewayContainers verifies that Stop removes both the main
// cloud-provider container and gateway sidecar containers for the cluster.
func TestStop_RemovesGatewayContainers(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	// Simulate a running cloud-provider-kind container.
	cpID, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image:  DefaultImage,
		Name:   containerName,
		Labels: map[string]string{ownerLabel: ownerValue},
	})
	require.NoError(t, err)
	require.NoError(t, eng.StartContainer(ctx, cpID))

	// Simulate a gateway envoy container spawned by cloud-provider-kind.
	gwID, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image: "envoyproxy/envoy:v1.33.2",
		Name:  "kindccm-gw-abc123",
		Labels: map[string]string{
			"io.x-k8s.cloud-provider-kind.cluster":      "mycluster",
			"io.x-k8s.cloud-provider-kind.gateway.name": "mycluster/default/my-gateway",
		},
	})
	require.NoError(t, err)
	require.NoError(t, eng.StartContainer(ctx, gwID))

	ctrl := New(eng, Config{ClusterName: "mycluster"})
	require.NoError(t, ctrl.Stop(ctx))

	// Both containers should be gone.
	remaining, err := eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// TestStop_LeavesUnrelatedContainers verifies that Stop does not remove
// containers belonging to other clusters or unrelated containers.
func TestStop_LeavesUnrelatedContainers(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	// Gateway container for a different cluster.
	otherID, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image: "envoyproxy/envoy:v1.33.2",
		Name:  "kindccm-gw-other",
		Labels: map[string]string{
			"io.x-k8s.cloud-provider-kind.cluster": "other-cluster",
		},
	})
	require.NoError(t, err)
	require.NoError(t, eng.StartContainer(ctx, otherID))

	// Unrelated container with no labels.
	unrelatedID, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image: "nginx:latest",
		Name:  "my-web-server",
	})
	require.NoError(t, err)
	require.NoError(t, eng.StartContainer(ctx, unrelatedID))

	ctrl := New(eng, Config{ClusterName: "mycluster"})
	require.NoError(t, ctrl.Stop(ctx))

	remaining, err := eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

// TestStop_NoClusterName_SkipsGatewayCleanup verifies that gateway containers
// are left alone when ClusterName is not set in the config.
func TestStop_NoClusterName_SkipsGatewayCleanup(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	// Gateway container exists but ClusterName is empty — should be left alone.
	gwID, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image: "envoyproxy/envoy:v1.33.2",
		Name:  "kindccm-gw-abc123",
		Labels: map[string]string{
			"io.x-k8s.cloud-provider-kind.cluster": "mycluster",
		},
	})
	require.NoError(t, err)
	require.NoError(t, eng.StartContainer(ctx, gwID))

	ctrl := New(eng, Config{})
	require.NoError(t, ctrl.Stop(ctx))

	remaining, err := eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
}

// TestBuildArgs_IncludesLBPortMapping verifies that --enable-lb-port-mapping is
// always present in the command-line arguments, regardless of other options.
func TestBuildArgs_IncludesLBPortMapping(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"defaults", Config{}},
		{"with gateway channel", Config{GatewayChannel: "experimental"}},
		{"ingress disabled", Config{IngressDefault: false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.cfg.buildArgs()
			assert.Contains(t, args, "--enable-lb-port-mapping")
		})
	}
}

// TestGatewayPorts returns the published host ports from envoy gateway
// containers, keyed by container port.
func TestGatewayPorts(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	// Gateway container for the cluster with published HTTP and HTTPS ports.
	gwID, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image: "envoyproxy/envoy:v1.33.2",
		Name:  "kindccm-gw-abc123",
		Labels: map[string]string{
			gatewayClusterLabel: "mycluster",
		},
		Ports: []currus.Port{
			{Container: 80, Host: 0},  // ephemeral host port
			{Container: 443, Host: 0}, // ephemeral host port
		},
	})
	require.NoError(t, err)
	require.NoError(t, eng.StartContainer(ctx, gwID))

	ctrl := New(eng, Config{ClusterName: "mycluster"})
	ports := ctrl.GatewayPorts(ctx)

	require.NotNil(t, ports)
	assert.Contains(t, ports, uint16(80))
	assert.Contains(t, ports, uint16(443))
	// Ephemeral ports are non-zero.
	assert.NotZero(t, ports[80])
	assert.NotZero(t, ports[443])
}

// TestGatewayPorts_WrongCluster verifies that containers for other clusters
// are not included in the port map.
func TestGatewayPorts_WrongCluster(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	_, err := eng.CreateContainer(ctx, currus.ContainerSpec{
		Image: "envoyproxy/envoy:v1.33.2",
		Name:  "kindccm-gw-other",
		Labels: map[string]string{
			gatewayClusterLabel: "other-cluster",
		},
		Ports: []currus.Port{{Container: 80, Host: 0}},
	})
	require.NoError(t, err)

	ctrl := New(eng, Config{ClusterName: "mycluster"})
	ports := ctrl.GatewayPorts(ctx)

	assert.Nil(t, ports)
}

// TestGatewayPorts_NoClusterName verifies that GatewayPorts returns nil when
// no cluster name is configured.
func TestGatewayPorts_NoClusterName(t *testing.T) {
	eng := currustest.New()
	ctrl := New(eng, Config{})
	assert.Nil(t, ctrl.GatewayPorts(context.Background()))
}

// TestStop_MultipleGatewayContainers verifies that Stop removes all gateway
// containers for the cluster, not just the first one found.
func TestStop_MultipleGatewayContainers(t *testing.T) {
	eng := currustest.New()
	ctx := context.Background()

	// Two gateway containers for the same cluster.
	for _, name := range []string{"kindccm-gw-aaa", "kindccm-gw-bbb"} {
		id, err := eng.CreateContainer(ctx, currus.ContainerSpec{
			Image: "envoyproxy/envoy:v1.33.2",
			Name:  name,
			Labels: map[string]string{
				"io.x-k8s.cloud-provider-kind.cluster": "mycluster",
			},
		})
		require.NoError(t, err)
		require.NoError(t, eng.StartContainer(ctx, id))
	}

	ctrl := New(eng, Config{ClusterName: "mycluster"})
	require.NoError(t, ctrl.Stop(ctx))

	remaining, err := eng.ListContainers(ctx, currus.ListContainersOpts{All: true})
	require.NoError(t, err)
	assert.Empty(t, remaining)
}
