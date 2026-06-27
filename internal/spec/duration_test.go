package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseDuration verifies duration parsing and validation rules.
func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantSec   int
		expectErr bool
	}{
		// Valid inputs
		{name: "seconds", input: "10s", wantSec: 10},
		{name: "one second", input: "1s", wantSec: 1},
		{name: "large seconds", input: "120s", wantSec: 120},
		{name: "minutes", input: "2m", wantSec: 120},
		{name: "one minute", input: "1m", wantSec: 60},
		{name: "hours", input: "1h", wantSec: 3600},
		{name: "two hours", input: "2h", wantSec: 7200},

		// Invalid inputs
		{name: "empty string", input: "", expectErr: true},
		{name: "no unit", input: "10", expectErr: true},
		{name: "unknown unit", input: "10d", expectErr: true},
		{name: "zero seconds", input: "0s", expectErr: true},
		{name: "negative", input: "-5s", expectErr: true},
		{name: "letters only", input: "abc", expectErr: true},
		// "1.5s" is accepted by time.ParseDuration; the JSON Schema pattern
		// ^[1-9][0-9]*(s|m|h)$ already rejects it before ParseDuration is called.
		{name: "unit only", input: "s", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDuration(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSec, got)
		})
	}
}
