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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func baseConfig() *config {
	return &config{
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeout:   defaultManagerTimeout,
		runtime:   RuntimeAuto,
		eventFunc: func(Event) {},
	}
}

// TestWithLogger_nil_fallsBackToDiscard stores nil when WithLogger receives nil.
func TestWithLogger_nil_fallsBackToDiscard(t *testing.T) {
	cfg := baseConfig()
	WithLogger(nil)(cfg)
	// nil logger is normalised in New(); here we just verify the option stores nil.
	assert.Nil(t, cfg.logger)
}

// TestWithRuntime sets the host container engine on the manager config.
func TestWithRuntime(t *testing.T) {
	cfg := baseConfig()
	WithRuntime(RuntimePodman)(cfg)
	assert.Equal(t, RuntimePodman, cfg.runtime)
}

// TestWithKubernetesVersion pins the Kubernetes version on the manager config.
func TestWithKubernetesVersion(t *testing.T) {
	cfg := baseConfig()
	WithKubernetesVersion("1.30")(cfg)
	assert.Equal(t, "1.30", cfg.k8sVersion)
}

// TestWithTimeout sets the per-operation timeout on the manager config.
func TestWithTimeout(t *testing.T) {
	cfg := baseConfig()
	WithTimeout(99 * time.Second)(cfg)
	assert.Equal(t, 99*time.Second, cfg.timeout)
}

// TestWithEventHandler registers a manager-level progress callback.
func TestWithEventHandler(t *testing.T) {
	cfg := baseConfig()
	var called bool
	WithEventHandler(func(Event) { called = true })(cfg)
	cfg.eventFunc(Event{})
	assert.True(t, called)
}

// TestWithKubeconfigDir overrides the directory used for kubeconfig copies.
func TestWithKubeconfigDir(t *testing.T) {
	cfg := baseConfig()
	WithKubeconfigDir("/tmp/test")(cfg)
	assert.Equal(t, "/tmp/test", cfg.kubeconfigDir)
}

// TestCreateOptions_portMappings_append accumulates port mappings across calls.
func TestCreateOptions_portMappings_append(t *testing.T) {
	cc := &createConfig{base: baseConfig()}
	WithPortMappings(PortMapping{HostPort: 80, ContainerPort: 8080})(cc)
	WithPortMappings(PortMapping{HostPort: 443, ContainerPort: 8443})(cc)
	assert.Len(t, cc.portMappings, 2)
}

// TestCreateOptions_withKindConfig stores raw Kind cluster YAML on create config.
func TestCreateOptions_withKindConfig(t *testing.T) {
	cc := &createConfig{base: baseConfig()}
	raw := []byte("kind: Cluster")
	WithKindConfig(raw)(cc)
	assert.Equal(t, raw, cc.rawKindConfig)
}

// TestCreateOptions_withCreateIfMissing enables idempotent create behavior.
func TestCreateOptions_withCreateIfMissing(t *testing.T) {
	cc := &createConfig{base: baseConfig()}
	assert.False(t, cc.createIfMissing)
	WithCreateIfMissing()(cc)
	assert.True(t, cc.createIfMissing)
}

// TestCreateOptions_withRetainOnFailure keeps partial clusters on create failure.
func TestCreateOptions_withRetainOnFailure(t *testing.T) {
	cc := &createConfig{base: baseConfig()}
	WithRetainOnFailure(true)(cc)
	assert.True(t, cc.retainOnFail)
}

// TestDeleteOptions_withIgnoreMissing treats a missing cluster as success
// on delete.
func TestDeleteOptions_withIgnoreMissing(t *testing.T) {
	dc := &deleteConfig{base: baseConfig()}
	assert.False(t, dc.ignoreMissing)
	WithIgnoreMissing()(dc)
	assert.True(t, dc.ignoreMissing)
}

// TestCreateEventHandler_fansOut verifies that both the per-call and the
// manager-level handlers receive every event (fan-out, not override).
func TestCreateEventHandler_fansOut(t *testing.T) {
	var managerCalled, perCallCalled bool
	cc := &createConfig{
		base: &config{eventFunc: func(Event) { managerCalled = true }},
	}
	WithCreateEventHandler(func(Event) { perCallCalled = true })(cc)

	cc.emit(Event{})
	assert.True(t, perCallCalled)
	assert.True(t, managerCalled)
}
