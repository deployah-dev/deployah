package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateProjectName verifies ValidateProjectName rules.
func TestValidateProjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{name: "valid simple name", input: "shop"},
		{name: "valid with dashes", input: "my-shop-app"},
		{name: "valid with digits", input: "shop2"},
		{name: "empty is invalid", input: "", expectErr: true},
		{name: "uppercase is invalid", input: "Shop", expectErr: true},
		{name: "leading dash is invalid", input: "-shop", expectErr: true},
		{name: "trailing dash is invalid", input: "shop-", expectErr: true},
		{name: "underscore is invalid", input: "my_shop", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProjectName(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestSanitizeProjectName verifies SanitizeProjectName produces a name that
// satisfies ValidateProjectName's pattern (length aside).
func TestSanitizeProjectName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "already valid", input: "shop", want: "shop"},
		{name: "uppercase lowered", input: "MyShop", want: "myshop"},
		{name: "spaces become a dash", input: "my shop app", want: "my-shop-app"},
		{name: "runs of invalid chars collapse", input: "my___shop", want: "my-shop"},
		{name: "leading and trailing invalid chars dropped", input: "-my-shop-", want: "my-shop"},
		{name: "underscores become dashes", input: "my_shop_app", want: "my-shop-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeProjectName(tt.input)
			assert.Equal(t, tt.want, got)
			if got != "" {
				assert.NoError(t, ValidateProjectName(got))
			}
		})
	}
}

// TestValidateComponentName verifies ValidateComponentName rules.
func TestValidateComponentName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{name: "valid simple name", input: "web"},
		{name: "valid with dashes", input: "web-api"},
		{name: "empty is invalid", input: "", expectErr: true},
		{name: "uppercase is invalid", input: "Web", expectErr: true},
		{name: "underscore is invalid", input: "web_api", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateComponentName(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateEnvName verifies ValidateEnvName rules against the v1-alpha.2
// object-shaped "environments" schema. Top-level environment keys never
// carry a "/*" wildcard suffix; that syntax is only valid in a component's
// "environments" filter list, which is a plain string array with no pattern
// constraint.
func TestValidateEnvName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{name: "valid simple name", input: "production"},
		{name: "valid with dashes", input: "pre-prod"},
		{name: "empty is invalid", input: "", expectErr: true},
		{name: "uppercase is invalid", input: "Production", expectErr: true},
		{name: "wildcard suffix is invalid for a top-level key", input: "review/*", expectErr: true},
		{name: "slash is invalid", input: "review/pr-123", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateEnvName(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateEnvVarName verifies ValidateEnvVarName rules.
func TestValidateEnvVarName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{name: "valid simple name", input: "APP_ENV"},
		{name: "valid single word", input: "DEBUG"},
		{name: "valid with digits", input: "MAX_RETRIES2"},
		{name: "empty is invalid", input: "", expectErr: true},
		{name: "lowercase is invalid", input: "app_env", expectErr: true},
		{name: "leading underscore is invalid", input: "_APP_ENV", expectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateEnvVarName(tt.input)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetValidators_RepeatedCallsDoNotPanic guards against the [sync.Once]
// error-capture regression, where only the first call after init saw the
// error and every later call got a nil validators pointer.
func TestGetValidators_RepeatedCallsDoNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		err1 := ValidateProjectName("shop")
		err2 := ValidateProjectName("shop")
		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}
