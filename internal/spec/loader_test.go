package spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSanitizeEnvName verifies sanitizeEnvName removes path separators and
// wildcards from environment names.
func TestSanitizeEnvName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name unchanged",
			input:    "production",
			expected: "production",
		},
		{
			name:     "wildcard suffix removed",
			input:    "review/*",
			expected: "review",
		},
		{
			name:     "forward slash removed",
			input:    "feature/branch",
			expected: "featurebranch",
		},
		{
			name:     "backslash removed",
			input:    "windows\\path",
			expected: "windowspath",
		},
		{
			name:     "asterisk removed",
			input:    "stage*",
			expected: "stage",
		},
		{
			name:     "question mark removed",
			input:    "test?env",
			expected: "testenv",
		},
		{
			name:     "multiple special chars removed",
			input:    "dev/*/test?env",
			expected: "devtestenv",
		},
		{
			name:     "complex wildcard pattern",
			input:    "feature/pr-123/*",
			expected: "featurepr-123",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeEnvName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveEnvFileWithSanitization verifies env file resolution for names
// containing wildcards and path separators.
func TestResolveEnvFileWithSanitization(t *testing.T) {
	tests := []struct {
		name             string
		envName          string
		setupFiles       []string
		expectedPath     string
		expectedExplicit bool
	}{
		{
			name:    "wildcard environment name finds sanitized file",
			envName: "review/*",
			setupFiles: []string{
				".env.review", // Should find this file
			},
			expectedPath:     ".env.review",
			expectedExplicit: false,
		},
		{
			name:    "path separator environment name finds sanitized file",
			envName: "feature/branch",
			setupFiles: []string{
				".env.featurebranch", // Should find this file
			},
			expectedPath:     ".env.featurebranch",
			expectedExplicit: false,
		},
		{
			name:    "multiple special chars in environment name",
			envName: "dev/*/test?env",
			setupFiles: []string{
				".deployah/.env.devtestenv", // Should find this file in .deployah directory
			},
			expectedPath:     filepath.Join(".deployah", ".env.devtestenv"),
			expectedExplicit: false,
		},
		{
			name:    "fallback to default .env when sanitized file not found",
			envName: "review/*",
			setupFiles: []string{
				".env", // Should fallback to this
			},
			expectedPath:     ".env",
			expectedExplicit: false,
		},
		{
			name:             "no files found",
			envName:          "review/*",
			setupFiles:       []string{},
			expectedPath:     "",
			expectedExplicit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			for _, file := range tt.setupFiles {
				dir := filepath.Dir(file)
				if dir != "." && dir != "" {
					mkdirErr := os.MkdirAll(dir, 0o750)
					require.NoError(t, mkdirErr)
				}
				writeErr := os.WriteFile(file, []byte("TEST_VAR=test"), 0o600)
				require.NoError(t, writeErr)
			}

			env := &Environment{}

			path, explicit, resolveErr := resolveEnvFile(env, tt.envName)
			require.NoError(t, resolveErr)
			assert.Equal(t, tt.expectedPath, path)
			assert.Equal(t, tt.expectedExplicit, explicit)
		})
	}
}

// TestResolveEnvFileExplicitWithWildcard verifies explicit env files work
// when the environment name contains wildcards.
func TestResolveEnvFileExplicitWithWildcard(t *testing.T) {
	t.Chdir(t.TempDir())

	explicitFile := "custom.env"
	err := os.WriteFile(explicitFile, []byte("EXPLICIT_VAR=explicit"), 0o600)
	require.NoError(t, err)

	env := &Environment{
		EnvFile: explicitFile,
	}

	path, explicit, err := resolveEnvFile(env, "review/*")
	require.NoError(t, err)
	assert.Equal(t, explicitFile, path)
	assert.True(t, explicit)
}
