package initialize

import (
	"testing"

	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/spec"
)

// nonInteractiveContext returns a Context whose IO is not a TTY. Prompts
// without WithDefault or WithPrefill fail; Select and MultiSelect return
// their defaults.
func nonInteractiveContext(t *testing.T) *nabat.Context {
	t.Helper()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	return nabattest.Context(t, app)
}

// TestPresetLabel verifies each preset label includes the preset name and
// its request-level CPU/memory values.
func TestPresetLabel(t *testing.T) {
	t.Parallel()

	for _, item := range presets {
		t.Run(string(item.value), func(t *testing.T) {
			t.Parallel()
			label := presetLabel(item.value)
			assert.Equal(t, item.label, label)
			assert.Contains(t, label, string(item.value))
			assert.Contains(t, label, "CPU")
			assert.Contains(t, label, "memory")
		})
	}
}

// TestPresetLabel_UnknownFallsBackToFormat verifies an unknown preset still
// renders a label, with "?" for missing CPU and memory.
func TestPresetLabel_UnknownFallsBackToFormat(t *testing.T) {
	t.Parallel()

	unknown := spec.ResourcePreset("not-a-real-preset")
	got := presetLabel(unknown)
	assert.Equal(t, formatPresetLabel(unknown), got)
	assert.Equal(t, "not-a-real-preset - ? CPU / ? memory", got)
}

// TestPresetFromLabel verifies presets.label and presets.fromLabel are
// inverse operations for every known preset, and that fromLabel rejects
// the "Custom..." label and arbitrary strings.
func TestPresetFromLabel(t *testing.T) {
	t.Parallel()

	for _, item := range presets {
		t.Run(string(item.value), func(t *testing.T) {
			t.Parallel()
			got, ok := presets.fromLabel(item.label)
			assert.True(t, ok)
			assert.Equal(t, item.value, got)
		})
	}

	tests := []struct {
		name  string
		label string
	}{
		{name: "custom label does not match a preset", label: customResourcesLabel},
		{name: "unrecognized label does not match a preset", label: "not a real label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := presets.fromLabel(tt.label)
			assert.False(t, ok)
			assert.Empty(t, got)
		})
	}
}

// TestPresetsCoverAllMappedPresets verifies presets and
// spec.ResourcePresetMappings list the same presets.
func TestPresetsCoverAllMappedPresets(t *testing.T) {
	t.Parallel()

	assert.Len(t, presets, len(spec.ResourcePresetMappings))
	for _, item := range presets {
		_, ok := spec.ResourcePresetMappings[item.value]
		assert.True(t, ok, "presets entry %q has no spec.ResourcePresetMappings entry", item.value)
	}
}

// TestRoleFromLabel verifies roles.label and roles.fromLabel are inverse
// operations for every known role.
func TestRoleFromLabel(t *testing.T) {
	t.Parallel()

	for _, item := range roles {
		t.Run(string(item.value), func(t *testing.T) {
			t.Parallel()
			got, ok := roles.fromLabel(item.label)
			assert.True(t, ok)
			assert.Equal(t, item.value, got)
		})
	}

	t.Run("unrecognized label does not match a role", func(t *testing.T) {
		t.Parallel()
		got, ok := roles.fromLabel("not a real label")
		assert.False(t, ok)
		assert.Empty(t, got)
	})
}

// TestKindFromLabel verifies kinds.label and kinds.fromLabel are inverse
// operations for every known kind.
func TestKindFromLabel(t *testing.T) {
	t.Parallel()

	for _, item := range kinds {
		t.Run(string(item.value), func(t *testing.T) {
			t.Parallel()
			got, ok := kinds.fromLabel(item.label)
			assert.True(t, ok)
			assert.Equal(t, item.value, got)
		})
	}

	t.Run("unrecognized label does not match a kind", func(t *testing.T) {
		t.Parallel()
		got, ok := kinds.fromLabel("not a real label")
		assert.False(t, ok)
		assert.Empty(t, got)
	})
}

// TestShlexSplit verifies quoted segments stay one token, which
// collectComponentCommand and collectComponentArgs rely on.
func TestShlexSplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "quoted segment stays one token", input: `"hello world" --flag`, want: []string{"hello world", "--flag"}},
		{name: "unquoted splits on spaces", input: "hello world --flag", want: []string{"hello", "world", "--flag"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tokens, err := shlex.Split(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tokens)
		})
	}
}

// TestCollectComponentMetricsPort_DefaultDeclines verifies declining the
// metrics question leaves Metrics unset.
func TestCollectComponentMetricsPort_DefaultDeclines(t *testing.T) {
	t.Parallel()

	c := nonInteractiveContext(t)
	comp := &spec.Component{Role: spec.ComponentRoleWorker}
	require.NoError(t, collectComponentMetricsPort(c, comp, "worker"))
	assert.Nil(t, comp.Metrics)
}

// TestCollectComponentExecHealth_DefaultDeclines verifies declining the exec
// health question leaves Health unset.
func TestCollectComponentExecHealth_DefaultDeclines(t *testing.T) {
	t.Parallel()

	c := nonInteractiveContext(t)
	comp := &spec.Component{Role: spec.ComponentRoleWorker}
	require.NoError(t, collectComponentExecHealth(c, comp, "worker"))
	assert.Nil(t, comp.Health)
}

// TestCollectComponentAdvanced_DefaultSkips verifies declining the advanced
// gate leaves kind, health, command, args, and metrics unset.
func TestCollectComponentAdvanced_DefaultSkips(t *testing.T) {
	t.Parallel()

	c := nonInteractiveContext(t)
	comp := &spec.Component{Role: spec.ComponentRoleWorker, Image: "worker:1"}
	require.NoError(t, collectComponentAdvanced(c, comp, "worker", []string{"dev"}))
	assert.Empty(t, comp.Kind)
	assert.Nil(t, comp.Health)
	assert.Nil(t, comp.Command)
	assert.Nil(t, comp.Args)
	assert.Nil(t, comp.Metrics)
}

// TestApplyComponentEssentials verifies essentials answers land on the
// component, including custom-resources deferral and invalid selections.
func TestApplyComponentEssentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		answers    componentEssentialsAnswers
		want       spec.Component
		wantCustom bool
	}{
		{
			name: "service local expose small",
			answers: componentEssentialsAnswers{
				roleLabel:     roles.label(spec.ComponentRoleService),
				image:         "nginx:latest",
				portStr:       "8080",
				resourceLabel: presets.label(spec.ResourcePresetSmall),
				expose:        true,
				askExpose:     true,
			},
			want: spec.Component{
				Role:           spec.ComponentRoleService,
				Image:          "nginx:latest",
				Port:           8080,
				ResourcePreset: spec.ResourcePresetSmall,
				Expose:         &spec.Expose{},
			},
		},
		{
			name: "worker discards port and expose",
			answers: componentEssentialsAnswers{
				roleLabel:     roles.label(spec.ComponentRoleWorker),
				image:         "shop/worker:1",
				portStr:       "8080",
				resourceLabel: presets.label(spec.ResourcePresetMicro),
				expose:        true,
				askExpose:     true,
			},
			want: spec.Component{
				Role:           spec.ComponentRoleWorker,
				Image:          "shop/worker:1",
				ResourcePreset: spec.ResourcePresetMicro,
			},
		},
		{
			name: "service without local skips expose",
			answers: componentEssentialsAnswers{
				roleLabel:     roles.label(spec.ComponentRoleService),
				image:         "nginx:latest",
				portStr:       "3000",
				resourceLabel: presets.label(spec.ResourcePresetSmall),
				expose:        true,
				askExpose:     false,
			},
			want: spec.Component{
				Role:           spec.ComponentRoleService,
				Image:          "nginx:latest",
				Port:           3000,
				ResourcePreset: spec.ResourcePresetSmall,
			},
		},
		{
			name: "custom resources defers to follow-up form",
			answers: componentEssentialsAnswers{
				roleLabel:     roles.label(spec.ComponentRoleService),
				image:         "nginx:latest",
				portStr:       "8080",
				resourceLabel: customResourcesLabel,
			},
			want: spec.Component{
				Role:  spec.ComponentRoleService,
				Image: "nginx:latest",
				Port:  8080,
			},
			wantCustom: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got spec.Component
			custom, err := applyComponentEssentials(&got, tt.answers)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCustom, custom)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyComponentEssentials_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answers componentEssentialsAnswers
		want    string
	}{
		{
			name: "unrecognized role",
			answers: componentEssentialsAnswers{
				roleLabel:     "not a role",
				image:         "nginx:latest",
				resourceLabel: presets.label(spec.ResourcePresetSmall),
			},
			want: "unrecognized role selection",
		},
		{
			name: "unrecognized resources",
			answers: componentEssentialsAnswers{
				roleLabel:     roles.label(spec.ComponentRoleService),
				image:         "nginx:latest",
				portStr:       "8080",
				resourceLabel: "not a preset",
			},
			want: "unrecognized resource selection",
		},
		{
			name: "invalid service port",
			answers: componentEssentialsAnswers{
				roleLabel:     roles.label(spec.ComponentRoleService),
				image:         "nginx:latest",
				portStr:       "nope",
				resourceLabel: presets.label(spec.ResourcePresetSmall),
			},
			want: "invalid port number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got spec.Component
			_, err := applyComponentEssentials(&got, tt.answers)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

// TestApplyCollectedMetricsPort verifies wizard port answers land on
// metrics.port, and non-numeric input is rejected.
func TestApplyCollectedMetricsPort(t *testing.T) {
	t.Parallel()

	comp := &spec.Component{}
	require.NoError(t, applyCollectedMetricsPort(comp, "9090"))
	require.NotNil(t, comp.Metrics)
	assert.Equal(t, 9090, comp.Metrics.Port)
}

func TestApplyCollectedMetricsPort_Error(t *testing.T) {
	t.Parallel()

	err := applyCollectedMetricsPort(&spec.Component{}, "nope")
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid metrics port")
}

// TestApplyCollectedExecHealth verifies a space-separated command becomes
// health.alive.exec, and a blank command is rejected.
func TestApplyCollectedExecHealth(t *testing.T) {
	t.Parallel()

	comp := &spec.Component{}
	require.NoError(t, applyCollectedExecHealth(comp, "pgrep -f worker"))
	require.NotNil(t, comp.Health)
	require.NotNil(t, comp.Health.Alive)
	assert.Equal(t, []string{"pgrep", "-f", "worker"}, comp.Health.Alive.Exec)
}

func TestApplyCollectedExecHealth_Error(t *testing.T) {
	t.Parallel()

	err := applyCollectedExecHealth(&spec.Component{}, "   ")
	require.Error(t, err)
	assert.ErrorContains(t, err, "exec command must not be empty")
}

func TestValidateComponentNameUnique(t *testing.T) {
	t.Parallel()
	existing := map[string]spec.Component{"web": {}}
	require.NoError(t, validateComponentNameUnique("api", existing))
}

func TestValidateComponentNameUnique_Error(t *testing.T) {
	t.Parallel()
	existing := map[string]spec.Component{"web": {}}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "invalid name", in: "Web", want: "component name"},
		{name: "duplicate", in: "web", want: "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateComponentNameUnique(tt.in, existing)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestLabeledListLabels(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{roles[0].label, roles[1].label}, roles.labels())
	assert.Equal(t, []string{kinds[0].label, kinds[1].label}, kinds.labels())
}

func TestCollectComponentKind_DefaultStateless(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{}
	require.NoError(t, collectComponentKind(nonInteractiveContext(t), comp, "web"))
	assert.Equal(t, spec.ComponentKindStateless, comp.Kind)
}

func TestCollectComponentReplicas_Default(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{}
	require.NoError(t, collectComponentReplicas(nonInteractiveContext(t), comp, "db"))
	require.NotNil(t, comp.Replicas)
	assert.Equal(t, 1, *comp.Replicas)
}

func TestCollectComponentCommand_DefaultSkips(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{}
	require.NoError(t, collectComponentCommand(nonInteractiveContext(t), comp, "web"))
	assert.Nil(t, comp.Command)
}

func TestCollectComponentArgs_DefaultSkips(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{}
	require.NoError(t, collectComponentArgs(nonInteractiveContext(t), comp, "web"))
	assert.Nil(t, comp.Args)
}

func TestCollectComponentEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("single environment is assigned without a prompt", func(t *testing.T) {
		t.Parallel()
		comp := &spec.Component{}
		require.NoError(t, collectComponentEnvironments(nonInteractiveContext(t), comp, "web", []string{"local"}))
		assert.Equal(t, []string{"local"}, comp.Environments)
	})

	t.Run("multiple environments keep the default selection", func(t *testing.T) {
		t.Parallel()
		comp := &spec.Component{}
		require.NoError(t, collectComponentEnvironments(nonInteractiveContext(t), comp, "web", []string{"local", "staging"}))
		assert.Equal(t, []string{"local", "staging"}, comp.Environments)
	})
}

func TestCollectComponentEnvironments_Error(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{}
	err := collectComponentEnvironments(nonInteractiveContext(t), comp, "web", nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no environments available")
}

func TestCollectComponentAdvancedDetails_WorkerUsesSelectDefaults(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{Role: spec.ComponentRoleWorker, Image: "worker:1"}
	err := collectComponentAdvancedDetails(nonInteractiveContext(t), comp, "worker", []string{"local"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get config files preference")
	assert.Equal(t, spec.ComponentKindStateless, comp.Kind)
	assert.Nil(t, comp.Command)
	assert.Nil(t, comp.Args)
	assert.Nil(t, comp.Metrics)
}

func TestCollectComponents_NameRequiresTTY(t *testing.T) {
	t.Parallel()
	config := &ProjectConfig{Components: map[string]spec.Component{}}
	err := collectComponents(nonInteractiveContext(t), config)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect component name")
}

func TestCollectComponentName_RequiresTTY(t *testing.T) {
	t.Parallel()
	_, err := collectComponentName(nonInteractiveContext(t), map[string]spec.Component{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect component name")
}

func TestCollectComponentConfigFiles_RequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentConfigFiles(nonInteractiveContext(t), &spec.Component{}, "web")
	require.Error(t, err)
	assert.ErrorIs(t, err, nabat.ErrConfirmationRequired)
}

func TestCollectComponentEssentials_ImageRequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentEssentials(nonInteractiveContext(t), &spec.Component{}, "web", []string{"local"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect component image")
}

func TestCollectComponentPersistence_RequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentPersistence(nonInteractiveContext(t), &spec.Component{}, "db")
	require.Error(t, err)
	assert.ErrorIs(t, err, nabat.ErrConfirmationRequired)
}

func TestCollectComponentAutoscaling_RequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentAutoscaling(nonInteractiveContext(t), &spec.Component{}, "web")
	require.Error(t, err)
	assert.ErrorIs(t, err, nabat.ErrConfirmationRequired)
}

func TestCollectComponentEnvironmentVariables_RequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentEnvironmentVariables(nonInteractiveContext(t), &spec.Component{}, "web")
	require.Error(t, err)
	assert.ErrorIs(t, err, nabat.ErrConfirmationRequired)
}

func TestCollectComponentHealth_RequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentHealth(nonInteractiveContext(t), &spec.Component{}, "web")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect health check path")
}

func TestCollectComponentCustomResources_RequiresTTY(t *testing.T) {
	t.Parallel()
	err := collectComponentCustomResources(nonInteractiveContext(t), &spec.Component{}, "web")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get custom resources")
}

func TestCollectComponentExposeOptions_RequiresTTY(t *testing.T) {
	t.Parallel()
	comp := &spec.Component{Expose: &spec.Expose{}}
	err := collectComponentExposeOptions(nonInteractiveContext(t), comp, "web")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get expose options")
}
