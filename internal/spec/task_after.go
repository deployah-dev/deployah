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

package spec

import (
	"fmt"
	"maps"
	"slices"
)

// AssignHookWeights returns Helm hook-weight per task name for hook tasks.
// Manual tasks are omitted. Independent tasks (no after) share weight 0;
// Helm then runs them in name order. A task's weight is one greater than
// the maximum weight of its after dependencies. Cycles return an error.
func AssignHookWeights(tasks map[string]Task) (map[string]int, error) {
	weights := make(map[string]int)
	for _, on := range []TaskOn{TaskOnPreDeploy, TaskOnPostDeploy} {
		phase, err := hookWeightsForPhase(tasks, on)
		if err != nil {
			return nil, err
		}
		maps.Copy(weights, phase)
	}
	return weights, nil
}

func hookWeightsForPhase(tasks map[string]Task, on TaskOn) (map[string]int, error) {
	names := make([]string, 0)
	for name, task := range tasks {
		if task.On == on {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) == 0 {
		return map[string]int{}, nil
	}

	inPhase := make(map[string]struct{}, len(names))
	for _, n := range names {
		inPhase[n] = struct{}{}
	}

	indegree := make(map[string]int, len(names))
	dependents := make(map[string][]string, len(names))
	for _, n := range names {
		indegree[n] = 0
	}
	for _, n := range names {
		for _, dep := range tasks[n].After {
			if _, ok := inPhase[dep]; !ok {
				continue
			}
			dependents[dep] = append(dependents[dep], n)
			indegree[n]++
		}
	}

	weights := make(map[string]int, len(names))
	ready := make([]string, 0)
	for _, n := range names {
		if indegree[n] == 0 {
			ready = append(ready, n)
		}
	}
	slices.Sort(ready)

	seen := 0
	for len(ready) > 0 {
		n := ready[0]
		ready = ready[1:]
		seen++
		maxDep := -1
		for _, dep := range tasks[n].After {
			if _, ok := inPhase[dep]; !ok {
				continue
			}
			if w, ok := weights[dep]; ok && w > maxDep {
				maxDep = w
			}
		}
		weights[n] = maxDep + 1
		next := make([]string, 0)
		for _, child := range dependents[n] {
			indegree[child]--
			if indegree[child] == 0 {
				next = append(next, child)
			}
		}
		slices.Sort(next)
		ready = append(ready, next...)
	}
	if seen != len(names) {
		// Whatever still has an unmet dependency is the cycle plus the
		// tasks waiting behind it. names is sorted, so stuck is too.
		stuck := make([]string, 0, len(names)-seen)
		for _, n := range names {
			if indegree[n] > 0 {
				stuck = append(stuck, n)
			}
		}
		return nil, fmt.Errorf("tasks with on %s: after contains a cycle among %s", on, joinStrings(stuck))
	}
	return weights, nil
}
