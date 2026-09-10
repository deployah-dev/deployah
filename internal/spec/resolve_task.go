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
	"fmt"
	"maps"
	"slices"
)

// ResolveTask resolves one named task for env. It returns a [ResolvedTask]
// by value: merged task fields, [ResolvedTask.HookWeight] from
// [AssignHookWeights], applied profiles, and that task's runtime environment.
//
// Other active components and tasks are not resolved. Component FQDNs
// and unrelated runtime environments are out of scope.
//
// platform may be nil. Resolution fails with [ErrCodePlatformNotFound]
// when the merged task applies profiles and no platform file is present.
// [ResolveProfileNames] still runs when the merged profile list is empty,
// so a platform default profile applies.
//
// Runtime inheritance uses the raw task's From, Env, and EnvFile.
// Parent ExplicitValues are inherited. Parent entity dotenv is loaded
// only when the task does not set envFile. An unused parent envFile is
// not read merely because the parent component is active in env.
// Environment-level runtime inputs still apply.
//
// It returns an error when the task is unknown, inactive in env, or
// [AssignHookWeights] fails. Hook-cycle handling is strict.
func ResolveTask(
	appSpec *Spec,
	platform *PlatformConfig,
	env EnvIdentity,
	name string,
) (ResolvedTask, error) {
	task, ok := appSpec.MergedTask(name)
	if !ok {
		return ResolvedTask{}, fmt.Errorf("unknown task %s", name)
	}
	if !task.activeInEnvironment(env.Original) {
		return ResolvedTask{}, fmt.Errorf("task %s is skipped in environment %s", name, env.Original)
	}

	weights, err := AssignHookWeights(appSpec.Tasks)
	if err != nil {
		return ResolvedTask{}, err
	}

	rt, err := resolveTaskDeclaration(name, task, platform, matchingPlatformEnv(platform, env), weights[name])
	if err != nil {
		return ResolvedTask{}, err
	}

	resolved := &ResolvedSpec{
		Spec:       appSpec,
		Env:        env,
		Components: map[string]ResolvedComponent{},
		Tasks:      map[string]ResolvedTask{name: rt},
	}
	if attachErr := attachRuntimeEnvironments(appSpec, env, resolved, nil); attachErr != nil {
		return ResolvedTask{}, attachErr
	}
	return resolved.Tasks[name], nil
}

func matchingPlatformEnv(platform *PlatformConfig, env EnvIdentity) *PlatformEnvironment {
	if platform == nil {
		return nil
	}
	keys := slices.Sorted(maps.Keys(platform.Environments))
	if matched, ok := matchEnvKey(env.Original, keys); ok {
		pe := platform.Environments[matched]
		return &pe
	}
	return nil
}
