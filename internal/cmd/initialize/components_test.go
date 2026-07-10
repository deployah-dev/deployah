package initialize

import (
	"testing"

	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
)

// TestPresetLabel verifies preset labels include the preset name and its
// request-level CPU/memory values, so the merged resources select is
// self-explanatory without consulting docs.
func TestPresetLabel(t *testing.T) {
	t.Parallel()

	for _, p := range presetOrder {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			label := presetLabel(p)
			assert.Contains(t, label, string(p))
			assert.Contains(t, label, "CPU")
			assert.Contains(t, label, "memory")
		})
	}
}

// TestPresetFromLabel verifies presetLabel and presetFromLabel are inverse
// operations for every known preset, and that presetFromLabel rejects the
// "Custom..." label and arbitrary strings.
func TestPresetFromLabel(t *testing.T) {
	t.Parallel()

	for _, p := range presetOrder {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			got, ok := presetFromLabel(presetLabel(p))
			require.True(t, ok)
			assert.Equal(t, p, got)
		})
	}

	t.Run("custom label does not match a preset", func(t *testing.T) {
		t.Parallel()
		_, ok := presetFromLabel(customResourcesLabel)
		assert.False(t, ok)
	})

	t.Run("unrecognized label does not match a preset", func(t *testing.T) {
		t.Parallel()
		_, ok := presetFromLabel("not a real label")
		assert.False(t, ok)
	})
}

// TestPresetOrderCoversAllMappedPresets guards against presetOrder drifting
// from spec.ResourcePresetMappings (e.g. a new preset added to one but not
// the other).
func TestPresetOrderCoversAllMappedPresets(t *testing.T) {
	t.Parallel()

	assert.Len(t, presetOrder, len(spec.ResourcePresetMappings))
	for _, p := range presetOrder {
		_, ok := spec.ResourcePresetMappings[p]
		assert.True(t, ok, "presetOrder entry %q has no spec.ResourcePresetMappings entry", p)
	}
}

// TestRoleFromLabel verifies roleLabels and roleFromLabel are inverse
// operations for every known role.
func TestRoleFromLabel(t *testing.T) {
	t.Parallel()

	for _, r := range roleOrder {
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()
			got, ok := roleFromLabel(roleLabels[r])
			require.True(t, ok)
			assert.Equal(t, r, got)
		})
	}

	t.Run("unrecognized label does not match a role", func(t *testing.T) {
		t.Parallel()
		_, ok := roleFromLabel("not a real label")
		assert.False(t, ok)
	})
}

// TestKindFromLabel verifies kindLabels and kindFromLabel are inverse
// operations for every known kind.
func TestKindFromLabel(t *testing.T) {
	t.Parallel()

	for _, k := range kindOrder {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			got, ok := kindFromLabel(kindLabels[k])
			require.True(t, ok)
			assert.Equal(t, k, got)
		})
	}

	t.Run("unrecognized label does not match a kind", func(t *testing.T) {
		t.Parallel()
		_, ok := kindFromLabel("not a real label")
		assert.False(t, ok)
	})
}

// TestNeedsHealthCheckQuestion verifies the health-check question follows
// [spec.Component.ListensOnPort]: worker and job components never get the
// question, and a service component without a port doesn't either.
func TestNeedsHealthCheckQuestion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component spec.Component
		want      bool
	}{
		{
			name:      "service with port needs the question",
			component: spec.Component{Role: spec.ComponentRoleService, Port: 8080},
			want:      true,
		},
		{
			name:      "service without a port does not",
			component: spec.Component{Role: spec.ComponentRoleService, Port: 0},
			want:      false,
		},
		{
			name:      "worker with a port set does not",
			component: spec.Component{Role: spec.ComponentRoleWorker, Port: 8080},
			want:      false,
		},
		{
			name:      "job with a port set does not",
			component: spec.Component{Role: spec.ComponentRoleJob, Port: 8080},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, needsHealthCheckQuestion(tt.component))
		})
	}
}

// TestShlexSplit_QuotedArguments locks in the behavior collectComponentCommand
// and collectComponentArgs rely on: quoted segments survive as a single
// token instead of being split on every space like [strings.Fields] would.
func TestShlexSplit_QuotedArguments(t *testing.T) {
	t.Parallel()

	tokens, err := shlex.Split(`"hello world" --flag`)
	require.NoError(t, err)
	assert.Equal(t, []string{"hello world", "--flag"}, tokens)
}
