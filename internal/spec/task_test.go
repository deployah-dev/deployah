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
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func shopSpec(tasks map[string]Task) *Spec {
	return &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]Component{
			"api": {
				Role:  ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Env:   map[string]string{"DATABASE_URL": "postgres://db", "LOG": "info"},
			},
		},
		Tasks: tasks,
	}
}

func TestFanout_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		want    Fanout
		wantErr string
	}{
		{
			name: "scalar integer sets count and parallelism 1",
			yaml: "fanout: 4\n",
			want: Fanout{Count: 4, Parallelism: DefaultFanoutParallelism},
		},
		{
			name: "object form",
			yaml: "fanout:\n  count: 4\n  parallelism: 2\n",
			want: Fanout{Count: 4, Parallelism: 2},
		},
		{
			name: "object omits parallelism",
			yaml: "fanout:\n  count: 3\n",
			want: Fanout{Count: 3, Parallelism: 0},
		},
		{
			name: "null is the omitted default",
			yaml: "fanout: null\n",
			want: Fanout{},
		},
		{
			name:    "zero scalar is rejected",
			yaml:    "fanout: 0\n",
			wantErr: "at least 1",
		},
		{
			name:    "string is rejected",
			yaml:    "fanout: lots\n",
			wantErr: "integer or an object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var task Task
			err := yaml.Unmarshal([]byte(tt.yaml), &task)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, task.Fanout)
		})
	}
}

func TestFanout_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Fanout
		want string
	}{
		{name: "count only emits integer", in: Fanout{Count: 4, Parallelism: 1}, want: "4"},
		{name: "parallelism 0 emits integer", in: Fanout{Count: 4}, want: "4"},
		{name: "parallelism above 1 emits object", in: Fanout{Count: 4, Parallelism: 2}, want: `{"count":4,"parallelism":2}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.in.MarshalJSON()
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestTask_FanoutRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("omitted fanout stays omitted", func(t *testing.T) {
		t.Parallel()
		in := Task{On: TaskOnManual, Image: "busybox:1.36"}
		data, err := yaml.Marshal(in)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "fanout")
		var out Task
		require.NoError(t, yaml.Unmarshal(data, &out))
		assert.Equal(t, Fanout{}, out.Fanout)
	})

	t.Run("scalar fanout round-trips", func(t *testing.T) {
		t.Parallel()
		in := Task{On: TaskOnManual, Image: "busybox:1.36", Fanout: Fanout{Count: 4, Parallelism: 1}}
		data, err := yaml.Marshal(in)
		require.NoError(t, err)
		var out Task
		require.NoError(t, yaml.Unmarshal(data, &out))
		assert.Equal(t, Fanout{Count: 4, Parallelism: DefaultFanoutParallelism}, out.Fanout)
	})
}

func TestTask_MergeFrom(t *testing.T) {
	t.Parallel()

	parent := Component{
		Image:          "ghcr.io/acme/shop:1.2.3",
		Env:            map[string]string{"DATABASE_URL": "postgres://db", "LOG": "info"},
		EnvFile:        ".env",
		ConfigFile:     "config.yaml",
		Environments:   []string{"prod"},
		Profiles:       []string{"default"},
		ResourcePreset: ResourcePresetSmall,
		Command:        []string{"api"},
		Args:           []string{"--serve"},
		Port:           8080,
	}

	t.Run("copies inheritable fields and overlays env", func(t *testing.T) {
		t.Parallel()
		task := Task{
			From:    "api",
			On:      TaskOnPreDeploy,
			Command: []string{"migrate", "up"},
			Env:     map[string]string{"LOG": "debug", "EXTRA": "1"},
		}
		got := task.MergeFrom(&parent)
		assert.Equal(t, parent.Image, got.Image)
		assert.Equal(t, []string{"migrate", "up"}, got.Command)
		assert.Nil(t, got.Args)
		assert.Equal(t, map[string]string{
			"DATABASE_URL": "postgres://db",
			"LOG":          "debug",
			"EXTRA":        "1",
		}, got.Env)
		assert.Equal(t, parent.EnvFile, got.EnvFile)
		assert.Equal(t, parent.ConfigFile, got.ConfigFile)
		assert.Equal(t, parent.Environments, got.Environments)
		assert.Equal(t, parent.Profiles, got.Profiles)
		assert.Equal(t, parent.ResourcePreset, got.ResourcePreset)
	})

	t.Run("task image replaces parent image", func(t *testing.T) {
		t.Parallel()
		task := Task{From: "api", Image: "busybox:1.36", On: TaskOnManual}
		got := task.MergeFrom(&parent)
		assert.Equal(t, "busybox:1.36", got.Image)
	})

	t.Run("nil parent leaves the task unchanged", func(t *testing.T) {
		t.Parallel()
		task := Task{Image: "busybox:1.36", On: TaskOnManual, Command: []string{"true"}}
		assert.Equal(t, task, task.MergeFrom(nil))
	})

	t.Run("merged env does not alias the task or the parent", func(t *testing.T) {
		t.Parallel()
		taskEnv := map[string]string{"EXTRA": "1"}
		parentEnv := map[string]string{"LOG": "info"}
		got := Task{From: "api", On: TaskOnManual, Env: taskEnv}.MergeFrom(&Component{
			Image: "ghcr.io/acme/shop:1.2.3",
			Env:   parentEnv,
		})
		got.Env["EXTRA"] = "mutated"
		assert.Equal(t, map[string]string{"EXTRA": "1"}, taskEnv)
		assert.Equal(t, map[string]string{"LOG": "info"}, parentEnv)
	})

	t.Run("task env is copied when the parent has none", func(t *testing.T) {
		t.Parallel()
		taskEnv := map[string]string{"EXTRA": "1"}
		got := Task{From: "api", On: TaskOnManual, Env: taskEnv}.MergeFrom(&Component{
			Image: "ghcr.io/acme/shop:1.2.3",
		})
		got.Env["EXTRA"] = "mutated"
		assert.Equal(t, map[string]string{"EXTRA": "1"}, taskEnv)
	})

	t.Run("no env stays nil", func(t *testing.T) {
		t.Parallel()
		got := Task{From: "api", On: TaskOnManual}.MergeFrom(&Component{
			Image: "ghcr.io/acme/shop:1.2.3",
		})
		assert.Nil(t, got.Env)
	})

	t.Run("parent resource quantities are cloned", func(t *testing.T) {
		t.Parallel()
		cpu := MustQuantity("100m")
		got := Task{From: "api", On: TaskOnManual, Command: []string{"true"}}.MergeFrom(&Component{
			Image:     "ghcr.io/acme/shop:1.2.3",
			Resources: Resources{CPU: cpu},
		})
		require.NotNil(t, got.Resources.CPU)
		assert.NotSame(t, cpu, got.Resources.CPU)
		got.Resources.CPU.Add(*MustQuantity("1"))
		assert.True(t, cpu.Equal(*MustQuantity("100m")))
	})
}

func TestMergedTask_InheritsParentResourcesAfterDefaults(t *testing.T) {
	t.Parallel()

	m := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]Component{
			"api": {
				Role:           ComponentRoleService,
				Image:          "ghcr.io/acme/shop:1.2.3",
				ResourcePreset: ResourcePresetNano,
			},
		},
		Tasks: map[string]Task{
			"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"migrate"}},
			"own": {
				From:           "api",
				On:             TaskOnManual,
				Command:        []string{"true"},
				ResourcePreset: ResourcePresetLarge,
			},
			"standalone": {Image: "busybox:1.36", On: TaskOnManual, Command: []string{"true"}},
		},
	}
	require.NoError(t, FillSpecWithDefaults(m, CurrentManifestVersion))

	migrate, ok := m.MergedTask("migrate")
	require.True(t, ok)
	require.NotNil(t, migrate.Resources.CPU)
	assert.True(t, migrate.Resources.CPU.Equal(*MustQuantity("100m")),
		"MergedTask(migrate) CPU = %s, want nano 100m", migrate.Resources.CPU.String())

	own, ok := m.MergedTask("own")
	require.True(t, ok)
	require.NotNil(t, own.Resources.Memory)
	assert.True(t, own.Resources.Memory.Equal(*MustQuantity("2048Mi")),
		"MergedTask(own) memory = %s, want large 2048Mi", own.Resources.Memory.String())

	standalone, ok := m.MergedTask("standalone")
	require.True(t, ok)
	require.NotNil(t, standalone.Resources.CPU)
	assert.True(t, standalone.Resources.CPU.Equal(*MustQuantity("500m")),
		"MergedTask(standalone) CPU = %s, want small 500m", standalone.Resources.CPU.String())
}

func TestSpec_MergedTask(t *testing.T) {
	t.Parallel()

	m := shopSpec(map[string]Task{
		"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"migrate", "up"}},
	})
	got, ok := m.MergedTask("migrate")
	require.True(t, ok)
	assert.Equal(t, "ghcr.io/acme/shop:1.2.3", got.Image)
	assert.Equal(t, "postgres://db", got.Env["DATABASE_URL"])

	_, ok = m.MergedTask("missing")
	assert.False(t, ok)

	var nilSpec *Spec
	_, ok = nilSpec.MergedTask("migrate")
	assert.False(t, ok)
	assert.Nil(t, nilSpec.TaskNames())
}

func TestSpec_TaskNames(t *testing.T) {
	t.Parallel()

	assert.Nil(t, (*Spec)(nil).TaskNames())
	assert.Nil(t, (&Spec{}).TaskNames())
	m := shopSpec(map[string]Task{
		"seed":    {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}},
		"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}},
	})
	assert.Equal(t, []string{"migrate", "seed"}, m.TaskNames())
}

func TestTask_EffectiveImage(t *testing.T) {
	t.Parallel()

	parent := &Component{Image: "ghcr.io/acme/shop:1.2.3"}
	assert.Equal(t, "busybox:1.36", Task{Image: "busybox:1.36"}.EffectiveImage(parent))
	assert.Equal(t, parent.Image, Task{From: "api"}.EffectiveImage(parent))
	assert.Empty(t, Task{}.EffectiveImage(nil))
}

func TestTask_UsesParentImage(t *testing.T) {
	t.Parallel()

	assert.True(t, Task{From: "api"}.UsesParentImage())
	assert.False(t, Task{From: "api", Image: "busybox:1.36"}.UsesParentImage())
	assert.False(t, Task{Image: "busybox:1.36"}.UsesParentImage())
}

func TestSpec_tasksInEnvironment(t *testing.T) {
	t.Parallel()

	m := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]Component{
			"api": {
				Role:         ComponentRoleService,
				Image:        "ghcr.io/acme/shop:1.2.3",
				Environments: []string{"prod"},
			},
		},
		Tasks: map[string]Task{
			"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"migrate"}},
			"smoke":   {From: "api", On: TaskOnPostDeploy, Environments: []string{"dev", "prod"}, Command: []string{"true"}},
			"nightly": {From: "api", On: TaskOnManual, Environments: []string{"prod"}, Command: []string{"true"}},
		},
	}

	tests := []struct {
		name string
		env  string
		want []string
	}{
		{name: "dev skips inherited prod-only parent", env: "dev", want: []string{"smoke"}},
		{name: "prod includes inherited and explicit", env: "prod", want: []string{"migrate", "nightly", "smoke"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := m.tasksInEnvironment(tt.env)
			names := make([]string, 0, len(got))
			for name := range got {
				names = append(names, name)
			}
			slices.Sort(names)
			assert.Equal(t, tt.want, names)
		})
	}
}

func TestEffectiveTasks(t *testing.T) {
	t.Parallel()

	manifest := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]Component{
			"api": {
				Role:         ComponentRoleService,
				Image:        "ghcr.io/acme/shop:1.2.3",
				Environments: []string{"prod"},
			},
		},
		Tasks: map[string]Task{
			"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"migrate"}},
			"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Command: []string{"seed"}},
			"smoke":   {From: "api", On: TaskOnPostDeploy, Environments: []string{"dev", "prod"}, Command: []string{"true"}},
		},
	}

	tests := []struct {
		name        string
		manifest    *Spec
		environment string
		resolved    *ResolvedSpec
		wantNames   []string
		wantWeight  map[string]int
	}{
		{
			name:        "nil resolved filters by environment",
			manifest:    manifest,
			environment: "dev",
			wantNames:   []string{"smoke"},
			wantWeight:  map[string]int{"smoke": 0},
		},
		{
			name:        "nil resolved assigns after weights",
			manifest:    manifest,
			environment: "prod",
			wantNames:   []string{"migrate", "seed", "smoke"},
			wantWeight:  map[string]int{"migrate": 0, "seed": 1, "smoke": 0},
		},
		{
			name:        "resolved tasks win over manifest filter",
			manifest:    manifest,
			environment: "dev",
			resolved: &ResolvedSpec{
				Tasks: map[string]ResolvedTask{
					"migrate": {Task: Task{On: TaskOnPreDeploy}, HookWeight: 3},
				},
			},
			wantNames:  []string{"migrate"},
			wantWeight: map[string]int{"migrate": 3},
		},
		{
			name:        "nil manifest and resolved is empty",
			environment: "dev",
			wantNames:   []string{},
			wantWeight:  map[string]int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := EffectiveTasks(tt.manifest, tt.environment, tt.resolved)
			require.NoError(t, err)
			names := make([]string, 0, len(got))
			weights := make(map[string]int, len(got))
			for name, rt := range got {
				names = append(names, name)
				weights[name] = rt.HookWeight
			}
			slices.Sort(names)
			assert.Equal(t, tt.wantNames, names)
			assert.Equal(t, tt.wantWeight, weights)
		})
	}
}

func TestEffectiveTasks_Cycle(t *testing.T) {
	t.Parallel()
	_, err := EffectiveTasks(&Spec{
		Tasks: map[string]Task{
			"a": {On: TaskOnPreDeploy, After: []string{"b"}, Command: []string{"true"}},
			"b": {On: TaskOnPreDeploy, After: []string{"a"}, Command: []string{"true"}},
		},
	}, "dev", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestEffectiveTasks_CopyDoesNotAliasResolved(t *testing.T) {
	t.Parallel()
	resolved := &ResolvedSpec{
		Tasks: map[string]ResolvedTask{
			"migrate": {Task: Task{On: TaskOnPreDeploy}},
		},
	}
	got, err := EffectiveTasks(nil, "dev", resolved)
	require.NoError(t, err)
	delete(got, "migrate")
	assert.Contains(t, resolved.Tasks, "migrate")
}

func TestTask_HelmHookEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		on   TaskOn
		want string
	}{
		{name: "preDeploy", on: TaskOnPreDeploy, want: "pre-install,pre-upgrade"},
		{name: "postDeploy", on: TaskOnPostDeploy, want: "post-install,post-upgrade"},
		{name: "manual", on: TaskOnManual, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Task{On: tt.on}.HelmHookEvents())
		})
	}
}

func TestValidateSpecTasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    *Spec
		wantErr string
	}{
		{
			name: "valid preDeploy from parent",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:    "api",
					On:      TaskOnPreDeploy,
					Command: []string{"migrate", "up"},
				},
			}),
		},
		{
			name: "from missing component",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "missing", On: TaskOnPreDeploy, Command: []string{"true"}},
			}),
			wantErr: "does not name a component",
		},
		{
			name: "name collides with a component",
			spec: shopSpec(map[string]Task{
				"api": {From: "api", On: TaskOnManual, Command: []string{"true"}},
			}),
			wantErr: "collides with a component",
		},
		{
			name: "on is required",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", Command: []string{"true"}},
			}),
			wantErr: "on is required",
		},
		{
			name: "on schedule points at issue 35",
			spec: shopSpec(map[string]Task{
				"nightly": {From: "api", On: TaskOn("schedule"), Command: []string{"true"}},
			}),
			wantErr: "issues/35",
		},
		{
			name: "on invalid value",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOn("whenever"), Command: []string{"true"}},
			}),
			wantErr: "is invalid",
		},
		{
			name: "after on manual is rejected",
			spec: shopSpec(map[string]Task{
				"backfill": {From: "api", On: TaskOnManual, After: []string{"migrate"}, Command: []string{"true"}},
			}),
			wantErr: "after is not allowed on manual tasks",
		},
		{
			name: "after cross-phase is rejected",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}},
				"smoke":   {From: "api", On: TaskOnPostDeploy, After: []string{"migrate"}, Command: []string{"true"}},
			}),
			wantErr: "not in the same on phase",
		},
		{
			name: "after missing task",
			spec: shopSpec(map[string]Task{
				"smoke": {From: "api", On: TaskOnPostDeploy, After: []string{"missing"}, Command: []string{"true"}},
			}),
			wantErr: "does not name a task",
		},
		{
			name: "after cycle",
			spec: shopSpec(map[string]Task{
				"a": {From: "api", On: TaskOnPreDeploy, After: []string{"b"}, Command: []string{"true"}},
				"b": {From: "api", On: TaskOnPreDeploy, After: []string{"a"}, Command: []string{"true"}},
			}),
			wantErr: "cycle",
		},
		{
			name: "command required when using parent image",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy},
			}),
			wantErr: "command is required when using the parent image",
		},
		{
			name: "command optional with custom image",
			spec: shopSpec(map[string]Task{
				"tool": {Image: "busybox:1.36", On: TaskOnManual},
			}),
		},
		{
			name: "fanout count greater than 1 on hook is allowed",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:    "api",
					On:      TaskOnPreDeploy,
					Command: []string{"true"},
					Fanout:  Fanout{Count: 4, Parallelism: 1},
				},
			}),
		},
		{
			name: "parallelism greater than count",
			spec: shopSpec(map[string]Task{
				"backfill": {
					From:    "api",
					On:      TaskOnManual,
					Command: []string{"true"},
					Fanout:  Fanout{Count: 2, Parallelism: 4},
				},
			}),
			wantErr: "less than or equal to fanout.count",
		},
		{
			name: "parallelism exceeds indexed job limit",
			spec: shopSpec(map[string]Task{
				"backfill": {
					From:    "api",
					On:      TaskOnManual,
					Command: []string{"true"},
					Fanout:  Fanout{Count: MaxFanoutParallelism + 1, Parallelism: MaxFanoutParallelism + 1},
				},
			}),
			wantErr: "fanout.parallelism must be at most",
		},
		{
			name: "hook timeout equal to default deploy timeout is allowed at spec load",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:    "api",
					On:      TaskOnPreDeploy,
					Command: []string{"true"},
					Timeout: "10m",
				},
			}),
		},
		{
			name: "hook timeout longer than default deploy timeout is allowed at spec load",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:    "api",
					On:      TaskOnPreDeploy,
					Command: []string{"true"},
					Timeout: "15m",
				},
			}),
		},
		{
			name: "after dependency not active in every dependent environment",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, Environments: []string{"prod"}, Command: []string{"true"}},
				"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Command: []string{"true"}},
			}),
			wantErr: "not active in every environment",
		},
		{
			name: "after dependency covers dependent environments",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}},
				"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Environments: []string{"prod"}, Command: []string{"true"}},
			}),
		},
		{
			name: "after same inherited environment filter",
			spec: &Spec{
				APIVersion: CurrentManifestVersion,
				Project:    "shop",
				Components: map[string]Component{
					"api": {
						Role:         ComponentRoleService,
						Image:        "ghcr.io/acme/shop:1.2.3",
						Environments: []string{"prod"},
					},
				},
				Tasks: map[string]Task{
					"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}},
					"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Command: []string{"true"}},
				},
			},
		},
		{
			name: "after prefix on dependency covers specific dependent",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, Environments: []string{"review"}, Command: []string{"true"}},
				"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Environments: []string{"review/pr-123"}, Command: []string{"true"}},
			}),
		},
		{
			name: "after specific dependency does not cover prefix dependent",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, Environments: []string{"review/pr-123"}, Command: []string{"true"}},
				"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Environments: []string{"review"}, Command: []string{"true"}},
			}),
			wantErr: "not active in every environment",
		},
		{
			name: "after inherited prod-only vs dependent everywhere",
			spec: &Spec{
				APIVersion: CurrentManifestVersion,
				Project:    "shop",
				Components: map[string]Component{
					"api": {
						Role:  ComponentRoleService,
						Image: "ghcr.io/acme/shop:1.2.3",
					},
				},
				Tasks: map[string]Task{
					"migrate": {From: "api", On: TaskOnPreDeploy, Environments: []string{"prod"}, Command: []string{"true"}},
					"seed":    {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Command: []string{"true"}},
				},
			},
			wantErr: "not active in every environment",
		},
		{
			name: "task cannot have both resources and resourcePreset",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:           "api",
					On:             TaskOnPreDeploy,
					Command:        []string{"true"},
					ResourcePreset: ResourcePresetSmall,
					Resources:      Resources{CPU: MustQuantity("100m")},
				},
			}),
			wantErr: "cannot have both",
		},
		{
			name: "fanout count exceeds int32",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:    "api",
					On:      TaskOnPreDeploy,
					Command: []string{"true"},
					Fanout:  Fanout{Count: math.MaxInt32 + 1, Parallelism: 1},
				},
			}),
			wantErr: "outside the int32 range",
		},
		{
			name: "from or image required",
			spec: shopSpec(map[string]Task{
				"orphan": {On: TaskOnManual, Command: []string{"true"}},
			}),
			wantErr: "from or image is required",
		},
		{
			name: "invalid task name",
			spec: shopSpec(map[string]Task{
				"Migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}},
			}),
			wantErr: "is invalid",
		},
		{
			name: "after contains an empty name",
			spec: shopSpec(map[string]Task{
				"seed": {From: "api", On: TaskOnPreDeploy, After: []string{"  "}, Command: []string{"true"}},
			}),
			wantErr: "after contains an empty name",
		},
		{
			name: "after includes itself",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, After: []string{"migrate"}, Command: []string{"true"}},
			}),
			wantErr: "after cannot include itself",
		},
		{
			name: "backoffLimit exceeds int32",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:         "api",
					On:           TaskOnPreDeploy,
					Command:      []string{"true"},
					BackoffLimit: new(math.MaxInt32 + 1),
				},
			}),
			wantErr: "outside the int32 range",
		},
		{
			name: "ttlSecondsAfterFinished exceeds int32",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:                    "api",
					On:                      TaskOnPreDeploy,
					Command:                 []string{"true"},
					TTLSecondsAfterFinished: new(math.MaxInt32 + 1),
				},
			}),
			wantErr: "outside the int32 range",
		},
		{
			name: "invalid timeout",
			spec: shopSpec(map[string]Task{
				"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"true"}, Timeout: "not-a-duration"},
			}),
			wantErr: "timeout",
		},
		{
			name: "environments slash-star suffix",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:         "api",
					On:           TaskOnPreDeploy,
					Command:      []string{"true"},
					Environments: []string{"review/*"},
				},
			}),
			wantErr: `"/*" suffix is not supported`,
		},
		{
			name: "empty profile name",
			spec: shopSpec(map[string]Task{
				"migrate": {
					From:     "api",
					On:       TaskOnPreDeploy,
					Command:  []string{"true"},
					Profiles: []string{" "},
				},
			}),
			wantErr: "profile name must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSpecTasks(tt.spec)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateSpecTasks_Nil(t *testing.T) {
	t.Parallel()

	err := ValidateSpecTasks(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec cannot be nil")
}

func TestCheckHookTaskTimeouts(t *testing.T) {
	t.Parallel()

	tasks := map[string]Task{
		"migrate":  {On: TaskOnPreDeploy, Timeout: "5m"},
		"backfill": {On: TaskOnManual, Timeout: "1h"},
	}

	require.NoError(t, CheckHookTaskTimeouts(tasks, 10*time.Minute))
	err := CheckHookTaskTimeouts(tasks, 5*time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate")
	assert.NotContains(t, err.Error(), "backfill")

	long := map[string]Task{
		"migrate": {On: TaskOnPreDeploy, Timeout: "15m"},
	}
	require.NoError(t, CheckHookTaskTimeouts(long, 20*time.Minute))
	err = CheckHookTaskTimeouts(long, 10*time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate")

	err = CheckHookTaskTimeouts(tasks, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session timeout must be a positive duration")

	err = CheckHookTaskTimeouts(map[string]Task{
		"migrate": {On: TaskOnPreDeploy, Timeout: "not-a-duration"},
	}, 10*time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.NotContains(t, err.Error(), "use --detach")
}

func TestCheckTaskTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		task    Task
		limit   time.Duration
		wantErr string
	}{
		{
			name:  "hook under limit",
			task:  Task{On: TaskOnPreDeploy, Timeout: "5m"},
			limit: 10 * time.Minute,
		},
		{
			name:    "hook at limit",
			task:    Task{On: TaskOnPreDeploy, Timeout: "5m"},
			limit:   5 * time.Minute,
			wantErr: "use --detach or raise --timeout",
		},
		{
			name:    "manual over limit",
			task:    Task{On: TaskOnManual, Timeout: "30m"},
			limit:   10 * time.Minute,
			wantErr: "use --detach or raise --timeout",
		},
		{
			name:  "manual empty timeout skipped",
			task:  Task{On: TaskOnManual},
			limit: 10 * time.Minute,
		},
		{
			name:    "hook empty timeout uses default",
			task:    Task{On: TaskOnPreDeploy},
			limit:   5 * time.Minute,
			wantErr: DefaultHookTaskTimeout,
		},
		{
			name:    "non-positive limit",
			task:    Task{On: TaskOnManual, Timeout: "1m"},
			limit:   0,
			wantErr: "session timeout must be a positive duration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckTaskTimeout("migrate", tt.task, tt.limit)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCheckTaskTimeout_InvalidDuration(t *testing.T) {
	t.Parallel()

	err := CheckTaskTimeout("migrate", Task{On: TaskOnManual, Timeout: "not-a-duration"}, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.NotContains(t, err.Error(), "use --detach")
}

func TestCheckHookTaskTimeouts_UsesEnvFilter(t *testing.T) {
	t.Parallel()

	m := &Spec{
		Components: map[string]Component{
			"api": {Image: "x", Environments: []string{"prod"}},
		},
		Tasks: map[string]Task{
			"migrate": {From: "api", On: TaskOnPreDeploy, Timeout: "9m", Command: []string{"true"}},
		},
	}
	require.NoError(t, CheckHookTaskTimeouts(m.tasksInEnvironment("dev"), 5*time.Minute))
	err := CheckHookTaskTimeouts(m.Tasks, 5*time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate")
}

func TestValidateAPIVersion_SupportedVersions(t *testing.T) {
	t.Parallel()

	got, err := ValidateAPIVersion(map[string]any{"apiVersion": CurrentManifestVersion})
	require.NoError(t, err)
	assert.Equal(t, CurrentManifestVersion, got)

	_, err = ValidateAPIVersion(map[string]any{"apiVersion": "v1-alpha.4"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported spec schema version")
	assert.Contains(t, err.Error(), "v1-alpha.4")
}
