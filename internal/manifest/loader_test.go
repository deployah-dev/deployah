package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestResolveEnvFileWithSanitization(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(oldWd)
		require.NoError(t, err)
	}()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

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
			// Clean up any previous test files
			cleanupFiles := []string{
				".env", ".env.review", ".env.featurebranch", ".env.devtestenv",
				filepath.Join(".deployah", ".env"), filepath.Join(".deployah", ".env.review"),
				filepath.Join(".deployah", ".env.featurebranch"), filepath.Join(".deployah", ".env.devtestenv"),
			}
			for _, file := range cleanupFiles {
				os.Remove(file)
			}
			os.RemoveAll(".deployah")

			// Setup test files
			for _, file := range tt.setupFiles {
				dir := filepath.Dir(file)
				if dir != "." && dir != "" {
					err := os.MkdirAll(dir, 0755)
					require.NoError(t, err)
				}
				err := os.WriteFile(file, []byte("TEST_VAR=test"), 0644)
				require.NoError(t, err)
			}

			env := &Environment{
				Name: tt.envName,
			}

			path, explicit, err := resolveEnvFile(env)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedPath, path)
			assert.Equal(t, tt.expectedExplicit, explicit)

			// Clean up
			for _, file := range tt.setupFiles {
				os.Remove(file)
			}
			os.RemoveAll(".deployah")
		})
	}
}

func TestResolveEnvFileExplicitWithWildcard(t *testing.T) {
	// Test that explicit env files work even with wildcard names
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		err := os.Chdir(oldWd)
		require.NoError(t, err)
	}()
	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	// Create an explicit env file
	explicitFile := "custom.env"
	err = os.WriteFile(explicitFile, []byte("EXPLICIT_VAR=explicit"), 0644)
	require.NoError(t, err)

	env := &Environment{
		Name:    "review/*",   // Wildcard name
		EnvFile: explicitFile, // But explicit file is set
	}

	path, explicit, err := resolveEnvFile(env)
	require.NoError(t, err)
	assert.Equal(t, explicitFile, path)
	assert.True(t, explicit)

	// Clean up
	os.Remove(explicitFile)
}
