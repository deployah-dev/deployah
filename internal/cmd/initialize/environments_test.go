package initialize

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
)

func TestValidateOtherEnvironmentInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		useLocal bool
	}{
		{name: "empty with local", input: "", useLocal: true},
		{name: "staging with local", input: "staging", useLocal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateOtherEnvironmentInput(tt.input, tt.useLocal))
		})
	}
}

func TestValidateOtherEnvironmentInput_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		useLocal bool
		want     string
	}{
		{name: "empty without local", input: "", useLocal: false, want: "at least one environment is required"},
		{name: "duplicate of local", input: "local", useLocal: true, want: "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateOtherEnvironmentInput(tt.input, tt.useLocal)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestApplyEnvironmentAnswers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		useLocal   bool
		otherInput string
		want       []string
	}{
		{name: "local only", useLocal: true, otherInput: "", want: []string{"local"}},
		{name: "local and staging", useLocal: true, otherInput: "staging", want: []string{"local", "staging"}},
		{name: "staging only", useLocal: false, otherInput: "staging", want: []string{"staging"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := &ProjectConfig{}
			require.NoError(t, applyEnvironmentAnswers(config, tt.useLocal, tt.otherInput))
			assert.Equal(t, tt.want, config.EnvironmentNames)
		})
	}
}

// TestParseEnvironmentNames verifies comma-separated names are trimmed and
// appended onto existing names.
func TestParseEnvironmentNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		existing []string
		want     []string
	}{
		{name: "multiple comma-separated", input: " qa , review ,  canary", want: []string{"qa", "review", "canary"}},
		{name: "skips empty entries", input: "qa,,review,", want: []string{"qa", "review"}},
		{name: "empty input keeps existing", existing: []string{"local"}, want: []string{"local"}},
		{name: "appends to existing", input: "qa", existing: []string{"local"}, want: []string{"local", "qa"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseEnvironmentNames(tt.input, slices.Clone(tt.existing))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseEnvironmentNames_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		existing []string
		want     string
	}{
		{name: "rejects wildcard suffix", input: "review/*", want: `"/*" suffix is not supported`},
		{name: "rejects duplicate", input: "local", existing: []string{"local"}, want: "already exists"},
		{name: "rejects invalid name", input: "Invalid_Name", want: "environment name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseEnvironmentNames(tt.input, slices.Clone(tt.existing))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestCollectEnvironments_LocalDefault(t *testing.T) {
	t.Parallel()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	config := &ProjectConfig{}
	require.NoError(t, collectEnvironments(nabattest.Context(t, app), config))
	assert.Equal(t, []string{DefaultEnvironmentName}, config.EnvironmentNames)
}

func TestApplyEnvironmentAnswers_Error(t *testing.T) {
	t.Parallel()
	err := applyEnvironmentAnswers(&ProjectConfig{}, false, "Invalid_Name")
	require.Error(t, err)
	assert.ErrorContains(t, err, "environment name")
}

func TestCollectEnvironmentVariables_RequiresTTY(t *testing.T) {
	t.Parallel()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	_, err := collectEnvironmentVariables(nabattest.Context(t, app))
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect variable details")
}
