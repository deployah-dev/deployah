package initialize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseEnvironmentNames_RejectsWildcardSuffix verifies a trailing "/*"
// is rejected with a hint: plain names already prefix-match at deploy time.
func TestParseEnvironmentNames_RejectsWildcardSuffix(t *testing.T) {
	t.Parallel()

	_, err := parseEnvironmentNames("review/*", nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, `"/*" suffix is not supported`)
}

// TestParseEnvironmentNames_MultipleCommaSeparated verifies multiple names
// with surrounding whitespace are all parsed.
func TestParseEnvironmentNames_MultipleCommaSeparated(t *testing.T) {
	t.Parallel()

	names, err := parseEnvironmentNames(" qa , review ,  canary", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"qa", "review", "canary"}, names)
}

// TestParseEnvironmentNames_SkipsEmptyEntries verifies stray commas do not
// produce empty environment names.
func TestParseEnvironmentNames_SkipsEmptyEntries(t *testing.T) {
	t.Parallel()

	names, err := parseEnvironmentNames("qa,,review,", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"qa", "review"}, names)
}

// TestParseEnvironmentNames_EmptyInputKeepsExisting verifies an empty
// input (the "skip" answer) returns the collected names unchanged.
func TestParseEnvironmentNames_EmptyInputKeepsExisting(t *testing.T) {
	t.Parallel()

	names, err := parseEnvironmentNames("", []string{"local"})
	require.NoError(t, err)
	assert.Equal(t, []string{"local"}, names)
}

// TestParseEnvironmentNames_AppendsToExisting verifies new names are
// appended after the names already collected.
func TestParseEnvironmentNames_AppendsToExisting(t *testing.T) {
	t.Parallel()

	names, err := parseEnvironmentNames("qa", []string{"local"})
	require.NoError(t, err)
	assert.Equal(t, []string{"local", "qa"}, names)
}

// TestParseEnvironmentNames_RejectsDuplicate verifies a name already
// collected is rejected.
func TestParseEnvironmentNames_RejectsDuplicate(t *testing.T) {
	t.Parallel()

	_, err := parseEnvironmentNames("local", []string{"local"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// TestParseEnvironmentNames_RejectsInvalidName verifies a name that fails
// schema pattern validation is rejected with a clear error.
func TestParseEnvironmentNames_RejectsInvalidName(t *testing.T) {
	t.Parallel()

	_, err := parseEnvironmentNames("Invalid_Name", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid environment name")
}
