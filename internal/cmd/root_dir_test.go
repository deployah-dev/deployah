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

package cmd_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/testing/testpath"
)

// TestWithDirResolvesSpec loads the spec through WithDir
// and leaves the process directory unchanged.
func TestWithDirResolvesSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dir        string
		args       []string
		wantStdout string
	}{
		{
			name:       "env file relative to virtual dir",
			dir:        "with-env",
			args:       []string{"resolve", "dev"},
			wantStdout: "LOG_LEVEL: info",
		},
		{
			name:       "explicit --spec is Abs against virtual dir",
			dir:        "explicit-spec",
			args:       []string{"resolve", "dev", "--spec", "app.yaml"},
			wantStdout: "LOG_LEVEL: info",
		},
		{
			name:       "literal image without env file",
			dir:        "literal",
			args:       []string{"plan", "dev", "--offline"},
			wantStdout: "validation: OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, out, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, nabattest.WithDir(testpath.Dir(t, "testdata", "cwd", tt.dir)))
			require.NoErrorf(t, err, "%s under WithDir\nstderr:\n%s", tt.args[0], errOut.String())
			assert.Contains(t, out.String(), tt.wantStdout)
		})
	}
}

// TestCwdFlagResolvesSpec resolves the spec through --cwd and -C.
func TestCwdFlagResolvesSpec(t *testing.T) {
	t.Parallel()

	withEnv := testpath.AbsDir(t, "testdata", "cwd", "with-env")
	explicit := testpath.AbsDir(t, "testdata", "cwd", "explicit-spec")

	tests := []struct {
		name       string
		args       []string
		opts       []nabattest.RunOption
		wantStdout string
	}{
		{
			name:       "absolute --cwd",
			args:       []string{"resolve", "dev", "--cwd", withEnv},
			wantStdout: "LOG_LEVEL: info",
		},
		{
			name:       "short -C before the command",
			args:       []string{"-C", withEnv, "resolve", "dev"},
			wantStdout: "LOG_LEVEL: info",
		},
		{
			name:       "relative --cwd",
			args:       []string{"resolve", "dev", "--cwd", testpath.Dir(t, "testdata", "cwd", "with-env")},
			wantStdout: "LOG_LEVEL: info",
		},
		{
			name:       "relative --spec is joined to --cwd",
			args:       []string{"resolve", "dev", "--cwd", explicit, "--spec", "app.yaml"},
			wantStdout: "LOG_LEVEL: info",
		},
		{
			name:       "--cwd wins over WithDir",
			args:       []string{"resolve", "dev", "--cwd", withEnv},
			opts:       []nabattest.RunOption{nabattest.WithDir(testpath.Dir(t, "testdata", "cwd", "literal"))},
			wantStdout: "LOG_LEVEL: info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, out, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, tt.opts...)
			require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
			assert.Contains(t, out.String(), tt.wantStdout)
		})
	}
}

// TestResolveSpec_Error fails when that directory cannot supply a usable spec.
func TestResolveSpec_Error(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()

	tests := []struct {
		name    string
		args    []string
		opts    []nabattest.RunOption
		wantErr string
	}{
		{
			name:    "missing required substitution variable",
			args:    []string{"plan", "dev", "--offline"},
			opts:    []nabattest.RunOption{nabattest.WithDir(testpath.Dir(t, "testdata", "cwd", "missing-var"))},
			wantErr: "variable",
		},
		{
			name:    "missing spec",
			args:    []string{"resolve", "dev", "--cwd", empty},
			wantErr: filepath.Join(empty, "deployah.yaml"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, _, _ := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, tt.opts...)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
