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
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"deployah.dev/deployah/internal/localkube/cloudprovider"
)

// TestDefaultCloudProviderConfig verifies that the factory sets expected defaults.
func TestDefaultCloudProviderConfig(t *testing.T) {
	cfg := defaultCloudProviderConfig()
	assert.Equal(t, GatewayStandard, cfg.gatewayChannel)
	assert.True(t, cfg.ingressDefault)
	assert.Equal(t, cloudprovider.DefaultImage, cfg.image)
	assert.Equal(t, "kind", cfg.network)
	assert.Empty(t, cfg.socketPath)
}

// TestCloudProviderOptions verifies that each option mutates the config correctly.
func TestCloudProviderOptions(t *testing.T) {
	cases := []struct {
		name   string
		opt    CloudProviderOption
		verify func(*testing.T, *cloudProviderConfig)
	}{
		{
			name: "WithGatewayAPI_standard",
			opt:  WithGatewayAPI(GatewayStandard),
			verify: func(t *testing.T, cfg *cloudProviderConfig) {
				t.Helper()
				assert.Equal(t, GatewayStandard, cfg.gatewayChannel)
			},
		},
		{
			name: "WithGatewayAPI_experimental",
			opt:  WithGatewayAPI(GatewayExperimental),
			verify: func(t *testing.T, cfg *cloudProviderConfig) {
				t.Helper()
				assert.Equal(t, GatewayExperimental, cfg.gatewayChannel)
			},
		},
		{
			name: "WithGatewayAPI_disabled",
			opt:  WithGatewayAPI(GatewayDisabled),
			verify: func(t *testing.T, cfg *cloudProviderConfig) {
				t.Helper()
				assert.Equal(t, GatewayDisabled, cfg.gatewayChannel)
			},
		},
		{
			name: "WithIngressDefault_false",
			opt:  WithIngressDefault(false),
			verify: func(t *testing.T, cfg *cloudProviderConfig) {
				t.Helper()
				assert.False(t, cfg.ingressDefault)
			},
		},
		{
			name: "WithCloudProviderImage",
			opt:  WithCloudProviderImage("registry.k8s.io/cloud-provider-kind/cloud-provider-kind:v0.1.0"),
			verify: func(t *testing.T, cfg *cloudProviderConfig) {
				t.Helper()
				assert.Equal(t, "registry.k8s.io/cloud-provider-kind/cloud-provider-kind:v0.1.0", cfg.image)
			},
		},
		{
			name: "WithCloudProviderSocket",
			opt:  WithCloudProviderSocket("/run/custom.sock"),
			verify: func(t *testing.T, cfg *cloudProviderConfig) {
				t.Helper()
				assert.Equal(t, "/run/custom.sock", cfg.socketPath)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultCloudProviderConfig()
			tc.opt(cfg)
			tc.verify(t, cfg)
		})
	}
}

// TestGatewayChannelConstants ensures the GatewayChannel constants have the
// values expected by cloud-provider-kind's GatewayReleaseChannel strings.
func TestGatewayChannelConstants(t *testing.T) {
	assert.Equal(t, GatewayChannel("standard"), GatewayStandard)
	assert.Equal(t, GatewayChannel("experimental"), GatewayExperimental)
	assert.Equal(t, GatewayChannel("disabled"), GatewayDisabled)
}

// TestToControllerConfig converts a cloudProviderConfig to a cloudprovider.Config.
func TestToControllerConfig(t *testing.T) {
	cfg := &cloudProviderConfig{
		image:          "custom-image:v1",
		network:        "kind",
		gatewayChannel: GatewayExperimental,
		ingressDefault: false,
		socketPath:     "/run/docker.sock",
	}
	got := cfg.toControllerConfig()

	assert.Equal(t, "custom-image:v1", got.Image)
	assert.Equal(t, "kind", got.Network)
	assert.Equal(t, "experimental", got.GatewayChannel)
	assert.False(t, got.IngressDefault)
	assert.Equal(t, "/run/docker.sock", got.SocketPath)
}

// TestStartCloudProvider_ErrUnsupported verifies that a Manager with no engine
// returns ErrUnsupported when StartCloudProvider is called.
func TestStartCloudProvider_ErrUnsupported(t *testing.T) {
	m := &Manager{
		cfg: &config{
			eventFunc: func(Event) {},
		},
		eng: nil, // no engine
	}
	err := m.StartCloudProvider(nil) //nolint:staticcheck
	assert.ErrorIs(t, err, ErrUnsupported)
}

// TestStopCloudProvider_ErrUnsupported mirrors the Start case.
func TestStopCloudProvider_ErrUnsupported(t *testing.T) {
	m := &Manager{
		cfg: &config{
			eventFunc: func(Event) {},
		},
		eng: nil,
	}
	err := m.StopCloudProvider(nil) //nolint:staticcheck
	assert.ErrorIs(t, err, ErrUnsupported)
}

// TestCloudProviderRunning_falseWithNoEngine verifies that CloudProviderRunning
// returns false when no engine is available.
func TestCloudProviderRunning_falseWithNoEngine(t *testing.T) {
	m := &Manager{
		cfg: &config{eventFunc: func(Event) {}},
		eng: nil,
	}
	assert.False(t, m.CloudProviderRunning(nil)) //nolint:staticcheck
}

// TestWithAttachWriter stores the writer on the cloud provider config.
func TestWithAttachWriter(t *testing.T) {
	cfg := defaultCloudProviderConfig()
	assert.Nil(t, cfg.attachWriter)

	var buf bytes.Buffer
	WithAttachWriter(&buf)(cfg)
	assert.Equal(t, &buf, cfg.attachWriter)
}

// TestAttachCloudProvider_ErrUnsupported verifies that AttachCloudProvider
// returns ErrUnsupported when no engine is available.
func TestAttachCloudProvider_ErrUnsupported(t *testing.T) {
	m := &Manager{
		cfg: &config{eventFunc: func(Event) {}},
		eng: nil,
	}
	err := m.AttachCloudProvider(nil) //nolint:staticcheck
	assert.ErrorIs(t, err, ErrUnsupported)
}
