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

package shell

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/remotecommand"
)

// TestShellQuote verifies POSIX-safe quoting via shellQuote.
func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "safe path unquoted", input: "/app/src", want: `/app/src`},
		{name: "spaces", input: "/app/my dir", want: `'/app/my dir'`},
		{name: "metacharacters", input: `/tmp/foo; id`, want: `'/tmp/foo; id'`},
		{name: "command substitution", input: `/tmp/$(id)`, want: `'/tmp/$(id)'`},
		{name: "single quote uses doubles", input: `it's`, want: `"it's"`},
		{name: "empty", input: "", want: `''`},
		{name: "null byte", input: "a\x00b", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := shellQuote(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildExecCommand verifies workdir quoting and command composition.
func TestBuildExecCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		shell   string
		command string
		workdir string
		want    []string
		wantErr bool
	}{
		{
			name:  "interactive shell",
			shell: "bash",
			want:  []string{"bash"},
		},
		{
			name:    "workdir only",
			shell:   "bash",
			workdir: "/app/src",
			want:    []string{"bash", "-c", "cd /app/src && exec bash"},
		},
		{
			name:    "workdir with metacharacters",
			shell:   "sh",
			workdir: `/tmp/foo; id`,
			want:    []string{"sh", "-c", "cd '/tmp/foo; id' && exec sh"},
		},
		{
			name:    "command only",
			shell:   "bash",
			command: "ls -la",
			want:    []string{"bash", "-c", "ls -la"},
		},
		{
			name:    "command with workdir",
			shell:   "bash",
			command: "ls -la",
			workdir: "/app/src",
			want:    []string{"bash", "-c", "cd /app/src && ls -la"},
		},
		{
			name:    "workdir with single quote",
			shell:   "bash",
			workdir: `/tmp/it's`,
			want:    []string{"bash", "-c", `cd "/tmp/it's" && exec bash`},
		},
		{
			name:    "workdir null byte",
			shell:   "bash",
			workdir: "a\x00b",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildExecCommand(tt.shell, tt.command, tt.workdir)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsBrokenPipe verifies typed broken-pipe detection through wrapping.
func TestIsBrokenPipe(t *testing.T) {
	t.Parallel()

	assert.False(t, isBrokenPipe(nil))
	assert.False(t, isBrokenPipe(errors.New("other error")))
	assert.False(t, isBrokenPipe(errors.New("write: broken pipe")))
	assert.True(t, isBrokenPipe(syscall.EPIPE))
	assert.True(t, isBrokenPipe(fmt.Errorf("stream: %w", syscall.EPIPE)))
}

// TestTerminalSizeFromWH clamps negative and oversized terminal dimensions.
func TestTerminalSizeFromWH(t *testing.T) {
	t.Parallel()

	sz := terminalSizeFromWH(80, 24)
	require.Equal(t, uint16(80), sz.Width)
	require.Equal(t, uint16(24), sz.Height)

	sz = terminalSizeFromWH(-1, 1<<20)
	assert.Equal(t, uint16(0), sz.Width)
	assert.Equal(t, uint16(^uint16(0)), sz.Height)
}

// TestTerminalSizeQueue_Next verifies Next returns sizes until the channel
// closes, then returns nil.
func TestTerminalSizeQueue_Next(t *testing.T) {
	t.Parallel()
	q := &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
	q.ch <- remotecommand.TerminalSize{Width: 80, Height: 24}

	sz := q.Next()
	require.NotNil(t, sz)
	assert.Equal(t, uint16(80), sz.Width)
	assert.Equal(t, uint16(24), sz.Height)

	close(q.ch)
	assert.Nil(t, q.Next())
}

// TestStartTerminalResizeWatch_SeedsAndStops verifies the watch seeds an
// initial size and that stop closes the queue after the resize goroutine exits.
func TestStartTerminalResizeWatch_SeedsAndStops(t *testing.T) {
	t.Parallel()
	q := &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 2)}
	calls := 0
	getSize := func(int) (int, int, error) {
		calls++
		return 100, 40, nil
	}

	stop := startTerminalResizeWatch(0, q, getSize)
	sz := q.Next()
	require.NotNil(t, sz)
	assert.Equal(t, uint16(100), sz.Width)
	assert.Equal(t, uint16(40), sz.Height)

	stop()
	assert.Nil(t, q.Next())
	assert.GreaterOrEqual(t, calls, 1)
}

// TestStartTerminalResizeWatch_GetSizeErrorSkipsSeed verifies a failing
// getSize leaves the queue empty until stop closes it.
func TestStartTerminalResizeWatch_GetSizeErrorSkipsSeed(t *testing.T) {
	t.Parallel()
	q := &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
	stop := startTerminalResizeWatch(0, q, func(int) (int, int, error) {
		return 0, 0, errors.New("no tty")
	})
	stop()
	assert.Nil(t, q.Next())
}
