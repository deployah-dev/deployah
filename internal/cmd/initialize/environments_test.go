package initialize

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOtherEnvironmentInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		useLocal bool
		wantErr  string
	}{
		{name: "empty with local", input: "", useLocal: true},
		{name: "empty without local", input: "", useLocal: false, wantErr: "at least one environment is required"},
		{name: "staging with local", input: "staging", useLocal: true},
		{name: "duplicate of local", input: "local", useLocal: true, wantErr: "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateOtherEnvironmentInput(tt.input, tt.useLocal)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
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

// TestParseEnvironmentNames verifies comma-separated names are trimmed,
// deduplicated, and validated, including the trailing "/*" hint.
func TestParseEnvironmentNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		existing []string
		want     []string
		wantErr  string
	}{
		{name: "rejects wildcard suffix", input: "review/*", wantErr: `"/*" suffix is not supported`},
		{name: "multiple comma-separated", input: " qa , review ,  canary", want: []string{"qa", "review", "canary"}},
		{name: "skips empty entries", input: "qa,,review,", want: []string{"qa", "review"}},
		{name: "empty input keeps existing", existing: []string{"local"}, want: []string{"local"}},
		{name: "appends to existing", input: "qa", existing: []string{"local"}, want: []string{"local", "qa"}},
		{name: "rejects duplicate", input: "local", existing: []string{"local"}, wantErr: "already exists"},
		{name: "rejects invalid name", input: "Invalid_Name", wantErr: "environment name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseEnvironmentNames(tt.input, slices.Clone(tt.existing))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
