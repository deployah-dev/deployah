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
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// TaskOn is when a [Task] runs.
type TaskOn string

const (
	// TaskOnPreDeploy runs the task before other resources on install and
	// upgrade.
	TaskOnPreDeploy TaskOn = "preDeploy"
	// TaskOnPostDeploy runs the task after the app is ready on install and
	// upgrade.
	TaskOnPostDeploy TaskOn = "postDeploy"
	// TaskOnManual runs the task only via the CLI.
	TaskOnManual TaskOn = "manual"
)

// IsHook reports whether o is a deploy hook (preDeploy or postDeploy).
func (o TaskOn) IsHook() bool {
	return o == TaskOnPreDeploy || o == TaskOnPostDeploy
}

// Task is run-to-completion work in a spec.
type Task struct {
	// From names a component whose env, envFile, configFile, environments,
	// profiles, and resources are copied. Command, args, and service-only
	// fields are not copied.
	From string `json:"from,omitempty" yaml:"from,omitempty"`
	// Image is the container image. When empty, the image from From is
	// used. When set, it replaces the parent image.
	Image string `json:"image,omitempty" yaml:"image,omitempty"`
	// Command overrides the container entrypoint. Required when using the
	// parent image.
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
	// Args overrides the container command arguments.
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
	// On selects when the task runs. Required.
	On TaskOn `json:"on" yaml:"on"`
	// After lists task names that must finish first in the same On phase.
	// Not allowed when On is manual.
	After []string `json:"after,omitempty" yaml:"after,omitempty"`
	// Env overlays inherited environment variables.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// EnvFile is a task-specific dotenv path. Replaces the inherited path
	// when set.
	EnvFile string `json:"envFile,omitempty" yaml:"envFile,omitempty"`
	// ConfigFile is a task-specific config path. Replaces the inherited
	// path when set.
	ConfigFile string `json:"configFile,omitempty" yaml:"configFile,omitempty"`
	// Environments limits the task to the named environments. Replaces the
	// inherited filter when set.
	Environments []string `json:"environments,omitempty" yaml:"environments,omitempty"`
	// Profiles lists platform profile names. Replaces the inherited list
	// when set.
	Profiles []string `json:"profiles,omitempty" yaml:"profiles,omitempty"`
	// Resources sets explicit CPU, memory, and storage requests.
	Resources Resources `json:"resources" yaml:"resources,omitempty"`
	// ResourcePreset selects a named resource profile when Resources is
	// empty.
	ResourcePreset ResourcePreset `json:"resourcePreset,omitempty" yaml:"resourcePreset,omitempty"`
	// Fanout sets how many indexed copies to run. Omit for one copy.
	Fanout Fanout `json:"fanout,omitzero" yaml:"fanout,omitempty"`
	// Timeout is how long a single run may take (for example "5m").
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// BackoffLimit is how many retries are allowed before the run fails.
	// Nil means [DefaultBackoffLimit].
	BackoffLimit *int `json:"backoffLimit,omitempty" yaml:"backoffLimit,omitempty"`
	// TTLSecondsAfterFinished is seconds to keep a finished run.
	TTLSecondsAfterFinished *int `json:"ttlSecondsAfterFinished,omitempty" yaml:"ttlSecondsAfterFinished,omitempty"`
}

// Fanout unmarshals a YAML/JSON integer (count, parallelism 1) or
// {count, parallelism}.
type Fanout struct {
	// Count is how many indexed copies to run.
	Count int `json:"count,omitempty" yaml:"count,omitempty"`
	// Parallelism is how many copies may run at once. Must not exceed
	// Count or [MaxFanoutParallelism].
	Parallelism int `json:"parallelism,omitempty" yaml:"parallelism,omitempty"`
}

// UnmarshalJSON handles integer and object forms:
//
//	fanout: 4
//	fanout: {count: 4, parallelism: 2}
//
// A JSON null is treated as the omitted default.
func (f *Fanout) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = Fanout{}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		if n < 1 {
			return fmt.Errorf("fanout: count must be at least 1")
		}
		*f = Fanout{Count: n, Parallelism: DefaultFanoutParallelism}
		return nil
	}
	type fanoutAlias Fanout
	var alias fanoutAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("fanout: expected an integer or an object with count: %w", err)
	}
	*f = Fanout(alias)
	return nil
}

// MarshalJSON emits an integer when parallelism is 0 or 1, otherwise an
// object. The zero value is omitted by the [Task.Fanout] omitzero tag.
func (f Fanout) MarshalJSON() ([]byte, error) {
	if f.Parallelism <= 1 {
		return json.Marshal(f.Count)
	}
	type fanoutAlias Fanout
	return json.Marshal(fanoutAlias(f))
}

// EffectiveCount returns Count, or [DefaultFanoutCount] when Count is 0.
func (f Fanout) EffectiveCount() int {
	if f.Count <= 0 {
		return DefaultFanoutCount
	}
	return f.Count
}

// EffectiveParallelism returns Parallelism, or [DefaultFanoutParallelism]
// when Parallelism is 0.
func (f Fanout) EffectiveParallelism() int {
	if f.Parallelism <= 0 {
		return DefaultFanoutParallelism
	}
	return f.Parallelism
}

// EffectiveImage returns the task image, or parent.Image when the task
// image is empty.
func (t Task) EffectiveImage(parent *Component) string {
	if t.Image != "" {
		return t.Image
	}
	if parent != nil {
		return parent.Image
	}
	return ""
}

// UsesParentImage reports whether the task runs the parent component image.
func (t Task) UsesParentImage() bool {
	return t.Image == "" && t.From != ""
}

// MergeFrom copies inheritable fields from parent when the task left them
// empty. Task env overlays parent env. Command, args, and service-only
// fields are never copied. parent may be nil when From is empty.
func (t Task) MergeFrom(parent *Component) Task {
	if parent == nil {
		return t
	}
	out := t
	if out.Image == "" {
		out.Image = parent.Image
	}
	if out.EnvFile == "" {
		out.EnvFile = parent.EnvFile
	}
	if out.ConfigFile == "" {
		out.ConfigFile = parent.ConfigFile
	}
	if len(out.Environments) == 0 {
		out.Environments = slices.Clone(parent.Environments)
	}
	if len(out.Profiles) == 0 {
		out.Profiles = slices.Clone(parent.Profiles)
	}
	if out.ResourcePreset == "" && !out.Resources.ResourcesSet() {
		out.ResourcePreset = parent.ResourcePreset
		if parent.Resources.ResourcesSet() {
			out.Resources = Resources{
				CPU:              cloneQuantity(parent.Resources.CPU),
				Memory:           cloneQuantity(parent.Resources.Memory),
				EphemeralStorage: cloneQuantity(parent.Resources.EphemeralStorage),
			}
		}
	}
	// Always build a fresh map so the caller cannot reach back into the
	// parent component or the task through the merged result.
	if len(parent.Env) > 0 || len(out.Env) > 0 {
		merged := make(map[string]string, len(parent.Env)+len(out.Env))
		maps.Copy(merged, parent.Env)
		maps.Copy(merged, out.Env)
		out.Env = merged
	}
	return out
}

// HelmHookEvents returns the Helm hook event list for t.On, or empty for
// manual tasks.
func (t Task) HelmHookEvents() string {
	switch t.On {
	case TaskOnPreDeploy:
		return "pre-install,pre-upgrade"
	case TaskOnPostDeploy:
		return "post-install,post-upgrade"
	default:
		return ""
	}
}

// TaskNames returns the sorted list of task names. Returns nil when no
// tasks are defined.
func (m *Spec) TaskNames() []string {
	if m == nil || len(m.Tasks) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m.Tasks))
	for k := range m.Tasks {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// MergedTask returns the named task after [Task.MergeFrom] with its
// parent component. The boolean is false when the name is unknown.
func (m *Spec) MergedTask(name string) (Task, bool) {
	if m == nil {
		return Task{}, false
	}
	task, ok := m.Tasks[name]
	if !ok {
		return Task{}, false
	}
	return task.MergeFrom(m.componentRef(task.From)), true
}

// tasksInEnvironment returns merged tasks that apply to env. An empty
// environments list on a task means every environment.
func (m *Spec) tasksInEnvironment(env string) map[string]Task {
	names := m.TaskNames()
	out := make(map[string]Task, len(names))
	for _, name := range names {
		task, ok := m.MergedTask(name)
		if !ok || !task.activeInEnvironment(env) {
			continue
		}
		out[name] = task
	}
	return out
}

// EffectiveTasks returns the tasks that apply to environment.
// When resolved is non-nil, it returns a new map of resolved.Tasks, which
// resolution already filtered to that environment. The map is safe to add
// to or delete from; the [ResolvedTask] values still alias the resolved
// spec (env, profiles, and merged profile). When resolved is nil, it
// returns merged tasks from manifest that apply to environment, with hook
// weights from [AssignHookWeights].
func EffectiveTasks(manifest *Spec, environment string, resolved *ResolvedSpec) (map[string]ResolvedTask, error) {
	if resolved != nil {
		out := make(map[string]ResolvedTask, len(resolved.Tasks))
		maps.Copy(out, resolved.Tasks)
		return out, nil
	}
	if manifest == nil {
		return map[string]ResolvedTask{}, nil
	}
	weights, err := AssignHookWeights(manifest.Tasks)
	if err != nil {
		return nil, err
	}
	envTasks := manifest.tasksInEnvironment(environment)
	out := make(map[string]ResolvedTask, len(envTasks))
	for name, task := range envTasks {
		out[name] = ResolvedTask{Task: task, HookWeight: weights[name]}
	}
	return out, nil
}

func (t Task) activeInEnvironment(env string) bool {
	if len(t.Environments) == 0 {
		return true
	}
	_, ok := matchEnvKey(env, t.Environments)
	return ok
}

func (m *Spec) componentRef(name string) *Component {
	if m == nil || name == "" {
		return nil
	}
	parent, ok := m.Components[name]
	if !ok {
		return nil
	}
	cp := parent
	return &cp
}

// scheduleOnToken is rejected with a pointer at issue #35.
const scheduleOnToken = "schedule"
