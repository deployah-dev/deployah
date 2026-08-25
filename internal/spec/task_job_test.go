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
// See the License for the specific language governing the License.

package spec

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTaskJobSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		task        Task
		count       int
		parallelism int
		want        TaskJobSpec
	}{
		{
			name: "fanout defaults and timeout",
			task: Task{
				Image:   "busybox:1.36",
				Command: []string{"migrate", "up"},
				Env:     map[string]string{"LOG": "debug"},
				Timeout: "5m",
				Fanout:  Fanout{Count: 4, Parallelism: 2},
			},
			want: TaskJobSpec{
				Completions:           4,
				Parallelism:           2,
				BackoffLimit:          int32(DefaultBackoffLimit),
				ActiveDeadlineSeconds: new(int64(300)),
				Image:                 "busybox:1.36",
				Command:               []string{"migrate", "up"},
				Env:                   map[string]string{"LOG": "debug"},
			},
		},
		{
			name:        "count and parallelism overrides clamp",
			task:        Task{Image: "busybox:1.36", Fanout: Fanout{Count: 1, Parallelism: 1}},
			count:       3,
			parallelism: 8,
			want: TaskJobSpec{
				Completions:  3,
				Parallelism:  3,
				BackoffLimit: int32(DefaultBackoffLimit),
				Image:        "busybox:1.36",
			},
		},
		{
			name: "explicit backoff and ttl",
			task: Task{
				Image:                   "busybox:1.36",
				BackoffLimit:            new(5),
				TTLSecondsAfterFinished: new(60),
			},
			want: TaskJobSpec{
				Completions:             1,
				Parallelism:             1,
				BackoffLimit:            5,
				TTLSecondsAfterFinished: new(int32(60)),
				Image:                   "busybox:1.36",
			},
		},
		{
			name: "scheduled empty timeout has no deadline",
			task: Task{
				Image:    "busybox:1.36",
				On:       TaskOnSchedule,
				Schedule: "0 3 * * *",
			},
			want: TaskJobSpec{
				Completions:  1,
				Parallelism:  1,
				BackoffLimit: int32(DefaultBackoffLimit),
				Image:        "busybox:1.36",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewTaskJobSpec(tt.task, tt.count, tt.parallelism)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewTaskJobSpec_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		task        Task
		count       int
		parallelism int
		wantErr     string
	}{
		{
			name:    "invalid timeout",
			task:    Task{Image: "busybox:1.36", Timeout: "nope"},
			wantErr: "timeout",
		},
		{
			name:        "parallelism exceeds indexed job limit",
			task:        Task{Image: "busybox:1.36"},
			count:       MaxFanoutParallelism + 1,
			parallelism: MaxFanoutParallelism + 1,
			wantErr:     "fanout.parallelism",
		},
		{
			name:    "count exceeds int32",
			task:    Task{Image: "busybox:1.36"},
			count:   math.MaxInt32 + 1,
			wantErr: "int32 range",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewTaskJobSpec(tt.task, tt.count, tt.parallelism)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNewTaskJobSpec_ClonesResources(t *testing.T) {
	t.Parallel()

	cpu := MustQuantity("100m")
	got, err := NewTaskJobSpec(Task{
		Image:     "busybox:1.36",
		Resources: Resources{CPU: cpu},
	}, 0, 0)
	require.NoError(t, err)
	require.NotNil(t, got.Resources.CPU)
	assert.NotSame(t, cpu, got.Resources.CPU)
	got.Resources.CPU.Add(*MustQuantity("1"))
	assert.True(t, cpu.Equal(*MustQuantity("100m")))
}
