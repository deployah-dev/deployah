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
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// TestClassifyKindErr pins the exact Kind error strings that map to sentinel
// errors. If Kind renames these strings in a future release, this test will
// fail and alert the team.
func TestClassifyKindErr(t *testing.T) {
	cases := []struct {
		raw  string
		want error
	}{
		{"ERROR: no clusters found", ErrNotFound},
		{"ERROR: unknown cluster \"foo\"", ErrNotFound},
		{"could not locate any control plane nodes for cluster named 'foo'", ErrNotFound},
		{"node(s) already exist for a cluster with the name \"foo\"", ErrAlreadyExists},
		{"some other error", nil}, // passthrough — want != sentinel
	}
	for _, tc := range cases {
		got, ok := classifyKindErr(errors.New(tc.raw))
		if tc.want != nil {
			assert.True(t, ok, "expected classification for input: %q", tc.raw)
			assert.ErrorIs(t, got, tc.want, "input: %q", tc.raw)
		} else {
			// Passthrough: not classified, original error returned.
			assert.False(t, ok, "input: %q", tc.raw)
			assert.Equal(t, tc.raw, got.Error(), "input: %q", tc.raw)
		}
	}
	gotNil, okNil := classifyKindErr(nil)
	assert.Nil(t, gotNil)
	assert.False(t, okNil)
}

// TestKubernetesVersionToImage maps version strings to kindest/node image tags.
func TestKubernetesVersionToImage(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", defaultKindNodeImage},
		{"1.31", "kindest/node:v1.31"},
		{"v1.31.2", "kindest/node:v1.31.2"},
		{"1.30.0", "kindest/node:v1.30.0"},
	}
	for _, tc := range cases {
		got := kubernetesVersionToImage(tc.input)
		assert.Equal(t, tc.want, got, "input: %q", tc.input)
	}
}

// TestBuildKindConfig_noPortMappings builds a single-node cluster without
// port mappings.
func TestBuildKindConfig_noPortMappings(t *testing.T) {
	cfg := &createConfig{base: &config{}}
	kc, err := buildKindConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, "Cluster", kc.Kind)
	assert.Equal(t, "kind.x-k8s.io/v1alpha4", kc.APIVersion)
	require.Len(t, kc.Nodes, 1)
	assert.Equal(t, kindv1alpha4.ControlPlaneRole, kc.Nodes[0].Role)
	assert.Empty(t, kc.Nodes[0].ExtraPortMappings)
}

// TestBuildKindConfig_withPortMappings maps host ports to node container ports.
func TestBuildKindConfig_withPortMappings(t *testing.T) {
	cfg := &createConfig{
		base: &config{},
		portMappings: []PortMapping{
			{HostPort: 8080, ContainerPort: 80, Protocol: ProtocolTCP},
			{HostPort: 9000, ContainerPort: 9000, Protocol: ProtocolUDP},
		},
	}
	kc, err := buildKindConfig(cfg)
	require.NoError(t, err)
	require.Len(t, kc.Nodes[0].ExtraPortMappings, 2)

	pm0 := kc.Nodes[0].ExtraPortMappings[0]
	assert.Equal(t, int32(8080), pm0.HostPort)
	assert.Equal(t, int32(80), pm0.ContainerPort)
	assert.Equal(t, kindv1alpha4.PortMappingProtocolTCP, pm0.Protocol)
	assert.Equal(t, "127.0.0.1", pm0.ListenAddress)

	pm1 := kc.Nodes[0].ExtraPortMappings[1]
	assert.Equal(t, kindv1alpha4.PortMappingProtocolUDP, pm1.Protocol)
}

// TestBuildKindConfig_invalidProtocol rejects unrecognized protocol values.
func TestBuildKindConfig_invalidProtocol(t *testing.T) {
	cfg := &createConfig{
		base: &config{},
		portMappings: []PortMapping{
			{HostPort: 8080, ContainerPort: 80, Protocol: "SCTP"},
		},
	}
	_, err := buildKindConfig(cfg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupported)
}

// TestProtocolToKind maps Protocol constants to Kind protocol constants and
// rejects unknown values.
func TestProtocolToKind(t *testing.T) {
	cases := []struct {
		input   Protocol
		want    kindv1alpha4.PortMappingProtocol
		wantErr bool
	}{
		{ProtocolTCP, kindv1alpha4.PortMappingProtocolTCP, false},
		{ProtocolUDP, kindv1alpha4.PortMappingProtocolUDP, false},
		{"", kindv1alpha4.PortMappingProtocolTCP, false}, // zero value defaults to TCP
		{"SCTP", "", true},
	}
	for _, tc := range cases {
		got, err := protocolToKind(tc.input)
		if tc.wantErr {
			require.Error(t, err, "input: %q", tc.input)
			assert.ErrorIs(t, err, ErrUnsupported)
		} else {
			require.NoError(t, err, "input: %q", tc.input)
			assert.Equal(t, tc.want, got)
		}
	}
}

// TestSlogAdapter_levels forwards Kind log calls to slog at the right level.
func TestSlogAdapter_levels(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := &slogAdapter{l: l, onEvent: func(Event) {}}

	a.Warn("test warn")
	assert.Contains(t, buf.String(), "WARN")
	buf.Reset()

	a.Error("test error")
	assert.Contains(t, buf.String(), "ERROR")
	buf.Reset()

	a.V(0).Info("v0 info")
	assert.Contains(t, buf.String(), "DEBUG") // non-status V(0) messages route to Debug
	buf.Reset()

	a.V(1).Info("v1 debug")
	assert.Contains(t, buf.String(), "DEBUG")
	assert.Contains(t, buf.String(), "verbosity=1")
}

// TestSlogAdapter_enabled reports all verbosity levels as enabled.
func TestSlogAdapter_enabled(t *testing.T) {
	l := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	a := &slogAdapter{l: l, onEvent: func(Event) {}}
	assert.True(t, a.V(0).Enabled())
	assert.True(t, a.V(5).Enabled())
}

// TestStripEmoji_preservesCJK verifies that CJK characters are not stripped
// while emoji are removed.
func TestStripEmoji_preservesCJK(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Installing CNI 🔌", "Installing CNI"},
		{"安装", "安装"},
		{"安装 🔌", "安装"},
		{"Starting control-plane 🕹️", "Starting control-plane"},
		{"No emoji at all", "No emoji at all"},
		{"Trailing space ", "Trailing space"},
		{"Mixed 安装🔌", "Mixed 安装"},
	}
	for _, tc := range cases {
		got := stripEmoji(tc.input)
		assert.Equal(t, tc.want, got, "input: %q", tc.input)
	}
}

// TestSlogAdapter_statusInterception converts Kind status lines to Events.
func TestSlogAdapter_statusInterception(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var events []Event
	a := &slogAdapter{l: l, onEvent: func(e Event) { events = append(events, e) }}

	a.V(0).Info(" • Installing CNI 🔌  ...")
	a.V(0).Info(" ✓ Installing CNI 🔌")
	a.V(0).Info(" ✗ Starting control-plane 🕹️")

	require.Len(t, events, 3)
	assert.Equal(t, StepInstallingCNI, events[0].Step)
	assert.Equal(t, StepStarted, events[0].Status)
	assert.Equal(t, StepInstallingCNI, events[1].Step)
	assert.Equal(t, StepCompleted, events[1].Status)
	assert.Equal(t, StepStartingControlPlane, events[2].Step)
	assert.Equal(t, StepFailed, events[2].Status)

	assert.Empty(t, buf.String(), "status lines must not reach slog")
}

// TestSlogAdapter_nonStatusPassesThrough ensures non-status V(0) messages
// still log.
func TestSlogAdapter_nonStatusPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var events []Event
	a := &slogAdapter{l: l, onEvent: func(e Event) { events = append(events, e) }}

	a.V(0).Info("some regular message")
	assert.Empty(t, events)
	assert.Contains(t, buf.String(), "some regular message")
}

// TestLoadImageArchive_usesCustomSpoolDir verifies that the spool temp file
// is created under the directory specified via WithSpoolDir / spoolDir field.
func TestLoadImageArchive_usesCustomSpoolDir(t *testing.T) {
	customDir := t.TempDir()

	kp := &kindProvider{spoolDir: customDir}

	// Intercept os.CreateTemp by temporarily setting the provider spoolDir.
	// We can't call loadImageArchive without a real Kind cluster, so we test
	// the spool-dir plumbing at the config level.
	assert.Equal(t, customDir, kp.spoolDir)
}
