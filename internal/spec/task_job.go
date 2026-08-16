// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/License-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package spec

import (
	"fmt"
	"maps"
	"math"
	"slices"
)

// TaskJobSpec is the resolved Job field set used by the CLI builder and Helm
// chart values. Completions and Parallelism are already resolved from fanout
// or caller overrides. TTLSecondsAfterFinished is nil unless the spec set
// it; the CLI applies [DefaultCLIJobTTLSeconds] itself.
type TaskJobSpec struct {
	Completions             int32
	Parallelism             int32
	BackoffLimit            int32
	ActiveDeadlineSeconds   *int64
	TTLSecondsAfterFinished *int32
	Image                   string
	Command                 []string
	Args                    []string
	Env                     map[string]string
	Resources               Resources
}

// NewTaskJobSpec builds the shared Job fields from task. count and
// parallelism override fanout when greater than 0; a value of 0 means use
// the task's fanout defaults. Parallelism is rejected above
// [MaxFanoutParallelism], then clamped to count. It returns a wrapped
// error when [Task.Timeout] is not a valid duration, when parallelism is
// above [MaxFanoutParallelism], or when count, backoffLimit, or
// ttlSecondsAfterFinished do not fit in int32.
func NewTaskJobSpec(task Task, count, parallelism int) (TaskJobSpec, error) {
	if count < 1 {
		count = task.Fanout.EffectiveCount()
	}
	if parallelism < 1 {
		parallelism = task.Fanout.EffectiveParallelism()
	}
	if parallelism > MaxFanoutParallelism {
		return TaskJobSpec{}, fmt.Errorf("fanout.parallelism must be at most %d", MaxFanoutParallelism)
	}
	if parallelism > count {
		parallelism = count
	}

	backoff := DefaultBackoffLimit
	if task.BackoffLimit != nil {
		backoff = *task.BackoffLimit
	}

	completions32, err := toInt32("fanout.count", count)
	if err != nil {
		return TaskJobSpec{}, err
	}
	backoff32, err := toInt32("backoffLimit", backoff)
	if err != nil {
		return TaskJobSpec{}, err
	}

	out := TaskJobSpec{
		Completions:  completions32,
		Parallelism:  int32(parallelism), //nolint:gosec // bounded by MaxFanoutParallelism
		BackoffLimit: backoff32,
		Image:        task.Image,
		Command:      slices.Clone(task.Command),
		Args:         slices.Clone(task.Args),
		Env:          maps.Clone(task.Env),
		Resources: Resources{
			CPU:              cloneQuantity(task.Resources.CPU),
			Memory:           cloneQuantity(task.Resources.Memory),
			EphemeralStorage: cloneQuantity(task.Resources.EphemeralStorage),
		},
	}
	if task.TTLSecondsAfterFinished != nil {
		ttl32, ttlErr := toInt32("ttlSecondsAfterFinished", *task.TTLSecondsAfterFinished)
		if ttlErr != nil {
			return TaskJobSpec{}, ttlErr
		}
		out.TTLSecondsAfterFinished = &ttl32
	}
	if task.Timeout != "" {
		sec, timeoutErr := ParseDuration(task.Timeout)
		if timeoutErr != nil {
			return TaskJobSpec{}, fmt.Errorf("timeout: %w", timeoutErr)
		}
		out.ActiveDeadlineSeconds = new(int64(sec))
	}
	return out, nil
}

func toInt32(name string, n int) (int32, error) {
	if n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("%s %d is outside the int32 range", name, n)
	}
	return int32(n), nil
}
