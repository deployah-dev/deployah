package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidatePositiveInteger verifies positive integer validation rules.
func TestValidatePositiveInteger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		fieldName string
		expectErr bool
		errMsg    string
	}{
		{name: "empty string", value: "", fieldName: "replicas", expectErr: true, errMsg: "replicas cannot be empty"},
		{name: "whitespace only", value: "   ", fieldName: "replicas", expectErr: true, errMsg: "must be a positive integer"},
		{name: "zero", value: "0", fieldName: "replicas", expectErr: true, errMsg: "must be a positive integer"},
		{name: "negative", value: "-1", fieldName: "replicas", expectErr: true, errMsg: "must be a positive integer"},
		{name: "non-numeric", value: "abc", fieldName: "replicas", expectErr: true, errMsg: "is invalid"},
		{name: "decimal", value: "1.5", fieldName: "replicas", expectErr: true, errMsg: "is invalid"},
		{name: "valid one", value: "1", fieldName: "replicas", expectErr: false},
		{name: "valid large", value: "999999999999999999999", fieldName: "replicas", expectErr: true, errMsg: "is invalid"},
		{name: "valid typical", value: "3", fieldName: "replicas", expectErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidatePositiveInteger(tt.value, tt.fieldName)
			if tt.expectErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateNonEmpty verifies non-empty string validation rules.
func TestValidateNonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		fieldName string
		expectErr bool
	}{
		{name: "empty string", value: "", fieldName: "name", expectErr: true},
		{name: "spaces only", value: "   ", fieldName: "name", expectErr: true},
		{name: "tabs only", value: "\t\t", fieldName: "name", expectErr: true},
		{name: "mixed whitespace", value: " \t\n ", fieldName: "name", expectErr: true},
		{name: "valid string", value: "my-app", fieldName: "name", expectErr: false},
		{name: "valid with surrounding whitespace", value: "  my-app  ", fieldName: "name", expectErr: false},
		{name: "unicode whitespace only", value: "\u00A0\u2003", fieldName: "name", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateNonEmpty(tt.value, tt.fieldName)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "name cannot be empty or contain only whitespace")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateMinMaxReplicas verifies min/max replica bound checking.
func TestValidateMinMaxReplicas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		minStr    string
		maxStr    string
		expectErr bool
		errMsg    string
	}{
		{name: "min non-numeric", minStr: "abc", maxStr: "5", expectErr: true, errMsg: "minimum replicas 'abc' is invalid"},
		{name: "max non-numeric", minStr: "1", maxStr: "xyz", expectErr: true, errMsg: "maximum replicas 'xyz' is invalid"},
		{name: "both non-numeric", minStr: "abc", maxStr: "xyz", expectErr: true, errMsg: "minimum replicas 'abc' is invalid"},
		{name: "min greater than max", minStr: "10", maxStr: "5", expectErr: true, errMsg: "cannot be greater than maximum"},
		{name: "min zero", minStr: "0", maxStr: "5", expectErr: true, errMsg: "must be at least 1"},
		{name: "min negative", minStr: "-1", maxStr: "5", expectErr: true, errMsg: "must be at least 1"},
		{name: "max exceeds 100", minStr: "1", maxStr: "101", expectErr: true, errMsg: "cannot exceed 100"},
		{name: "max exactly 100", minStr: "1", maxStr: "100", expectErr: false},
		{name: "min equals max", minStr: "5", maxStr: "5", expectErr: false},
		{name: "typical valid range", minStr: "2", maxStr: "10", expectErr: false},
		{name: "min exactly one", minStr: "1", maxStr: "1", expectErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateMinMaxReplicas(tt.minStr, tt.maxStr)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateResourceString verifies CPU, memory, and ephemeral storage
// resource string validation.
func TestValidateResourceString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		fieldName string
		expectErr bool
		errMsg    string
	}{
		// Empty is always allowed regardless of field.
		{name: "empty CPU allowed", value: "", fieldName: "CPU", expectErr: false},
		{name: "empty Memory allowed", value: "", fieldName: "Memory", expectErr: false},

		// CPU: millicores.
		{name: "CPU millicores valid", value: "500m", fieldName: "CPU", expectErr: false},
		{name: "CPU millicores zero", value: "0m", fieldName: "CPU", expectErr: false},
		{name: "CPU millicores missing number", value: "m", fieldName: "CPU", expectErr: true, errMsg: "missing number before 'm'"},
		{name: "CPU millicores negative", value: "-100m", fieldName: "CPU", expectErr: true, errMsg: "requires a non-negative integer"},
		{name: "CPU millicores non-numeric", value: "abcm", fieldName: "CPU", expectErr: true, errMsg: "requires a non-negative integer"},

		// CPU: plain numbers.
		{name: "CPU integer valid", value: "1", fieldName: "CPU", expectErr: false},
		{name: "CPU decimal valid", value: "2.5", fieldName: "CPU", expectErr: false},
		{name: "CPU negative decimal invalid", value: "-1", fieldName: "CPU", expectErr: true, errMsg: "must be a number"},
		{name: "CPU non-numeric invalid", value: "abc", fieldName: "CPU", expectErr: true, errMsg: "must be a number"},
		{name: "CPU with surrounding whitespace", value: "  1  ", fieldName: "CPU", expectErr: false},

		// Memory: unit suffix required.
		{name: "Memory valid Mi", value: "512Mi", fieldName: "Memory", expectErr: false},
		{name: "Memory valid Gi", value: "1Gi", fieldName: "Memory", expectErr: false},
		{name: "Memory valid Ki", value: "1024Ki", fieldName: "Memory", expectErr: false},
		{name: "Memory valid Ti", value: "1Ti", fieldName: "Memory", expectErr: false},
		{name: "Memory missing unit", value: "512", fieldName: "Memory", expectErr: true, errMsg: "must include unit"},
		{name: "Memory wrong unit suffix", value: "512MB", fieldName: "Memory", expectErr: true, errMsg: "must include unit"},

		// EphemeralStorage shares Memory's rules.
		{name: "EphemeralStorage valid", value: "10Gi", fieldName: "EphemeralStorage", expectErr: false},
		{name: "EphemeralStorage missing unit", value: "10", fieldName: "EphemeralStorage", expectErr: true, errMsg: "must include unit"},

		// Unknown field names fall through without validation.
		{name: "unknown field passes through", value: "anything", fieldName: "Unknown", expectErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateResourceString(tt.value, tt.fieldName)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
