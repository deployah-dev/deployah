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

package cmdopts

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// TestClusterHint verifies typed connectivity errors get a recovery hint.
func TestClusterHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantHint bool
	}{
		{name: "nil", err: nil, wantHint: false},
		{name: "unrelated", err: errors.New("chart render failed"), wantHint: false},
		{name: "empty config", err: clientcmd.ErrEmptyConfig, wantHint: true},
		{name: "wrapped empty config", err: fmt.Errorf("rest config: %w", clientcmd.ErrEmptyConfig), wantHint: true},
		{name: "no context", err: clientcmd.ErrNoContext, wantHint: true},
		{
			name:     "wrapped no context",
			err:      fmt.Errorf("target: %w", clientcmd.ErrNoContext),
			wantHint: true,
		},
		{
			name: "op error connection refused",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: syscall.ECONNREFUSED,
			},
			wantHint: true,
		},
		{
			name: "wrapped op error",
			err: fmt.Errorf("helm client: %w", &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: syscall.ECONNREFUSED,
			}),
			wantHint: true,
		},
		{
			name: "url error wrapping dial",
			err: &url.Error{
				Op:  "Get",
				URL: "https://127.0.0.1:6443",
				Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			},
			wantHint: true,
		},
		{
			name: "dns error",
			err: &net.DNSError{
				Err:  "no such host",
				Name: "kubernetes.default",
			},
			wantHint: true,
		},
		{
			name:     "joined with unreachable",
			err:      errors.Join(errors.New("other"), syscall.ECONNREFUSED),
			wantHint: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClusterHint(tt.err)
			if tt.wantHint {
				require.NotEmpty(t, got)
				assert.Contains(t, got, "deployah cluster up")
				return
			}
			assert.Empty(t, got)
		})
	}
}
