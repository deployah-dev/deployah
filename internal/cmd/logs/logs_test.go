// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package logs

import (
	"testing"

	"github.com/stern/stern/stern"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
)

func TestLogContainerStates_IncludesTerminated(t *testing.T) {
	t.Parallel()

	states, err := logContainerStates()
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, stern.RUNNING, string(states[0]))
	assert.Equal(t, stern.TERMINATED, string(states[1]))
}

func TestRunLogs_FlagValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "template and template-file together",
			args: []string{"logs", "shop", "--template", "{{.Message}}", "--template-file", "log.tmpl"},
			want: "cannot specify both --template and --template-file",
		},
		{
			name: "resource missing name",
			args: []string{"logs", "shop", "--resource", "job/"},
			want: "resource format must be",
		},
		{
			name: "resource missing slash",
			args: []string{"logs", "shop", "--resource", "job"},
			want: "resource format must be",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			io, _, _, _ := nabattest.NewIO()
			app := nabat.MustNew("deployah", nabat.WithIO(io))
			Register(app)
			err := nabattest.Run(t, app, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
