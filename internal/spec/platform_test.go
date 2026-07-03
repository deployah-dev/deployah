package spec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
)

// writeTempFile writes content to a temp file and returns its path.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// TestLoadPlatform_Valid verifies platform spec behavior.
func TestLoadPlatform_Valid(t *testing.T) {
	yaml := `
apiVersion: platform/v1-alpha.1
environments:
  production:
    context: prod-eks
    domains:
      public:
        baseDomain: example.com
        tls:
          mode: certManager
          issuer: letsencrypt-prod
  local:
    context: kind-deployah
    domains:
      public:
        baseDomain: 127.0.0.1.nip.io
        tls:
          mode: selfSigned
`
	path := writeTempFile(t, yaml)
	p, err := spec.LoadPlatform(path)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "platform/v1-alpha.1", p.APIVersion)
	assert.Len(t, p.Environments, 2)
	prod := p.Environments["production"]
	assert.Equal(t, "prod-eks", prod.Context)
	assert.Equal(t, "example.com", prod.Domains["public"].BaseDomain)
}

// TestLoadPlatform_MissingFile verifies platform spec behavior.
func TestLoadPlatform_MissingFile(t *testing.T) {
	_, err := spec.LoadPlatform("/nonexistent/path.yaml")
	require.Error(t, err)
}

// TestLoadPlatform_InvalidVersion verifies platform spec behavior.
func TestLoadPlatform_InvalidVersion(t *testing.T) {
	yaml := `
apiVersion: platform/v99-unknown
environments:
  local:
    context: kind-deployah
`
	path := writeTempFile(t, yaml)
	_, err := spec.LoadPlatform(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// TestLoadPlatform_CertManagerMissingIssuer verifies platform spec behavior.
func TestLoadPlatform_CertManagerMissingIssuer(t *testing.T) {
	yaml := `
apiVersion: platform/v1-alpha.1
environments:
  prod:
    domains:
      public:
        baseDomain: example.com
        tls:
          mode: certManager
`
	path := writeTempFile(t, yaml)
	_, err := spec.LoadPlatform(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "issuer")
}

// TestLoadPlatform_SecretNameMissingField verifies platform spec behavior.
func TestLoadPlatform_SecretNameMissingField(t *testing.T) {
	yaml := `
apiVersion: platform/v1-alpha.1
environments:
  prod:
    domains:
      public:
        baseDomain: example.com
        tls:
          mode: secretName
`
	path := writeTempFile(t, yaml)
	_, err := spec.LoadPlatform(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secretName")
}

// TestMatchEnvKey_ExactMatch verifies platform spec behavior.
func TestMatchEnvKey_ExactMatch(t *testing.T) {
	keys := []string{"production", "staging", "review"}
	matched, ok := spec.MatchEnvKey("production", keys)
	require.True(t, ok)
	assert.Equal(t, "production", matched)
}

// TestMatchEnvKey_PrefixMatch verifies platform spec behavior.
func TestMatchEnvKey_PrefixMatch(t *testing.T) {
	keys := []string{"production", "review"}
	matched, ok := spec.MatchEnvKey("review/pr-123", keys)
	require.True(t, ok)
	assert.Equal(t, "review", matched)
}

// TestMatchEnvKey_NoMatch verifies platform spec behavior.
func TestMatchEnvKey_NoMatch(t *testing.T) {
	keys := []string{"production", "staging"}
	_, ok := spec.MatchEnvKey("review/pr-123", keys)
	assert.False(t, ok)
}

// TestMatchEnvKey_ExactBeatsPrefix verifies platform spec behavior.
func TestMatchEnvKey_ExactBeatsPrefix(t *testing.T) {
	// "review/pr-123" exact key should win over "review" prefix.
	keys := []string{"review", "review/pr-123"}
	matched, ok := spec.MatchEnvKey("review/pr-123", keys)
	require.True(t, ok)
	assert.Equal(t, "review/pr-123", matched)
}

// --- normalizeEnv / EnvIdentity tests ---

// TestNormalizeEnv_Simple verifies platform spec behavior.
func TestNormalizeEnv_Simple(t *testing.T) {
	id := spec.NormalizeEnv("production")
	assert.Equal(t, "production", id.Original)
	assert.Equal(t, "production", id.MapKey)
	assert.Equal(t, "production", id.K8sSafe)
}

// TestNormalizeEnv_Wildcard verifies platform spec behavior.
func TestNormalizeEnv_Wildcard(t *testing.T) {
	id := spec.NormalizeEnv("review/pr-123")
	assert.Equal(t, "review/pr-123", id.Original)
	assert.Equal(t, "review", id.MapKey)
	assert.Equal(t, "review-pr-123", id.K8sSafe)
}

// --- Resolve tests ---

func minimalPlatform() *spec.PlatformConfig {
	return &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"production": {
				Context: "prod-eks",
				Domains: map[string]spec.PlatformDomain{
					"public": {
						BaseDomain: "example.com",
						TLS: &spec.PlatformTLS{
							Mode:   spec.TLSModeCertManager,
							Issuer: "letsencrypt-prod",
						},
					},
				},
			},
			"local": {
				Context: "kind-deployah",
				Domains: map[string]spec.PlatformDomain{
					"public": {
						BaseDomain: "127.0.0.1.nip.io",
						TLS:        &spec.PlatformTLS{Mode: spec.TLSModeSelfSigned},
					},
				},
			},
		},
	}
}

func minimalSpec(subdomain *string) *spec.Spec {
	return &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
			"local":      {},
		},
		Components: map[string]spec.Component{
			"api": {
				Expose: &spec.Expose{Domain: "public", Subdomain: subdomain},
			},
		},
	}
}

// TestResolve_FQDN verifies platform spec behavior.
func TestResolve_FQDN(t *testing.T) {
	appSpec := minimalSpec(new("api"))
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	resolved, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Empty(t, report.ErrorCode)
	assert.Empty(t, report.Warnings)
	rc, ok := resolved.Components["api"]
	require.True(t, ok)
	assert.Equal(t, "api.example.com", rc.FQDN)
	assert.Equal(t, spec.TLSModeCertManager, rc.TLSMode)
	assert.Equal(t, "prod-eks", resolved.KubeContext)
}

// TestResolve_ApexMode verifies platform spec behavior.
func TestResolve_ApexMode(t *testing.T) {
	appSpec := minimalSpec(nil) // nil subdomain = apex
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	resolved, _, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)
	rc := resolved.Components["api"]
	assert.Equal(t, "example.com", rc.FQDN)
}

// TestResolve_DomainGapError verifies platform spec behavior.
func TestResolve_DomainGapError(t *testing.T) {
	appSpec := minimalSpec(new("api"))
	// staging has no domains defined.
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"staging": {Context: "staging-eks"},
		},
	}
	env := spec.NormalizeEnv("staging")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	// Either a hard error or an error code in the report.
	hasDomainGap := (err != nil) || (report != nil && report.ErrorCode == spec.ErrCodeDomainGap)
	assert.True(t, hasDomainGap, "expected DOMAIN_GAP error, got err=%v report=%+v", err, report)
}

// TestResolve_FQDNCollision verifies platform spec behavior.
func TestResolve_FQDNCollision(t *testing.T) {
	// Two components resolving to the same FQDN (apex on same domain).
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"a": {Expose: &spec.Expose{Domain: "public"}}, // apex
			"b": {Expose: &spec.Expose{Domain: "public"}}, // apex - collision
		},
	}
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	hasColl := (err != nil) || (report != nil && report.ErrorCode == spec.ErrCodeFQDNCollision)
	assert.True(t, hasColl, "expected FQDN_COLLISION, got err=%v report=%+v", err, report)
}

// TestResolve_WildcardStaticSubdomainWarning verifies platform spec behavior.
func TestResolve_WildcardStaticSubdomainWarning(t *testing.T) {
	// review/pr-123 matches the "review" wildcard key; static subdomain warns.
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"review": {
				Context: "staging-eks",
				Domains: map[string]spec.PlatformDomain{
					"public": {
						BaseDomain: "review.example.com",
						TLS:        &spec.PlatformTLS{Mode: spec.TLSModeSelfSigned},
					},
				},
			},
		},
	}
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"review": {},
		},
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("api")}},
		},
	}
	env := spec.NormalizeEnv("review/pr-123")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)
	var hasWarn bool
	for _, w := range report.Warnings {
		if len(w) > 0 {
			hasWarn = true
		}
	}
	assert.True(t, hasWarn, "expected wildcard static subdomain warning")
}

// TestResolve_WildcardDynamicSubdomainNoWarning verifies platform spec behavior.
func TestResolve_WildcardDynamicSubdomainNoWarning(t *testing.T) {
	// Subdomain came from envsubst => no warning even for wildcard env.
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"review": {
				Context: "staging-eks",
				Domains: map[string]spec.PlatformDomain{
					"public": {
						BaseDomain: "review.example.com",
						TLS:        &spec.PlatformTLS{Mode: spec.TLSModeSelfSigned},
					},
				},
			},
		},
	}
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"review": {},
		},
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("pr-123")}},
		},
	}
	env := spec.NormalizeEnv("review/pr-123")
	// Mark "api" as having a dynamic subdomain.
	substReport := spec.SubstitutionReport{DynamicSubdomains: map[string]bool{"api": true}}
	_, report, err := spec.Resolve(appSpec, platform, env, substReport)
	require.NoError(t, err)
	assert.Empty(t, report.Warnings)
}

// TestResolve_PlatformEnvNotFound verifies platform spec behavior.
func TestResolve_PlatformEnvNotFound(t *testing.T) {
	appSpec := minimalSpec(new("api"))
	platform := &spec.PlatformConfig{
		APIVersion:   "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{},
	}
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	hasErr := (err != nil) || (report != nil && report.ErrorCode == spec.ErrCodePlatformEnvNotFound)
	assert.True(t, hasErr, "expected PLATFORM_ENV_NOT_FOUND, got err=%v report=%+v", err, report)
}

// TestSentinelSubstituteRaw_ReplacesTokens verifies platform spec behavior.
func TestSentinelSubstituteRaw_ReplacesTokens(t *testing.T) {
	input := []byte("subdomain: ${PR_NUMBER}")
	out := spec.SentinelSubstituteRaw(input)
	assert.Contains(t, string(out), "placeholder")
	assert.NotContains(t, string(out), "${PR_NUMBER}")
}

// TestSentinelSubstituteRaw_LiteralPassthrough verifies platform spec behavior.
func TestSentinelSubstituteRaw_LiteralPassthrough(t *testing.T) {
	input := []byte("subdomain: api")
	out := spec.SentinelSubstituteRaw(input)
	assert.Equal(t, string(input), string(out))
}

// TestScaffoldLocalPlatformFile_CreatesFile verifies platform spec behavior.
func TestScaffoldLocalPlatformFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")
	created, err := spec.ScaffoldLocalPlatformFile(path, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, created)

	p, loadErr := spec.LoadPlatform(path)
	require.NoError(t, loadErr)
	_, hasLocal := p.Environments["local"]
	assert.True(t, hasLocal)
}

// TestScaffoldLocalPlatformFile_DoesNotOverwrite verifies platform spec behavior.
func TestScaffoldLocalPlatformFile_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")
	// Write a different file first.
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: platform/v1-alpha.1\nenvironments:\n  prod:\n    context: prod\n"), 0o600))

	created, err := spec.ScaffoldLocalPlatformFile(path, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, created)

	// File should still have original content.
	data, readErr := os.ReadFile(path) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "prod")
	assert.NotContains(t, string(data), "local")
}

// TestPlatformEnvContext_DirectMatch verifies platform spec behavior.
func TestPlatformEnvContext_DirectMatch(t *testing.T) {
	p := minimalPlatform()
	ctx := spec.PlatformEnvContext(p, "production")
	assert.Equal(t, "prod-eks", ctx)
}

// TestPlatformEnvContext_WildcardMatch verifies platform spec behavior.
func TestPlatformEnvContext_WildcardMatch(t *testing.T) {
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"review": {Context: "staging-eks"},
		},
	}
	ctx := spec.PlatformEnvContext(platform, "review/pr-42")
	assert.Equal(t, "staging-eks", ctx)
}

// TestPlatformEnvContext_NoMatch verifies platform spec behavior.
func TestPlatformEnvContext_NoMatch(t *testing.T) {
	p := minimalPlatform()
	ctx := spec.PlatformEnvContext(p, "unknown")
	assert.Empty(t, ctx)
}

// TestResolve_ErrorCode_PlatformNotFound verifies platform spec behavior.
func TestResolve_ErrorCode_PlatformNotFound(t *testing.T) {
	// Component uses expose but platform is nil.
	appSpec := minimalSpec(new("api"))
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, nil, env, spec.SubstitutionReport{})
	require.Error(t, err)
	require.NotNil(t, report)
	assert.Equal(t, spec.ErrCodePlatformNotFound, report.ErrorCode)
}

// TestResolve_ErrorCode_PlatformEnvNotFound verifies platform spec behavior.
func TestResolve_ErrorCode_PlatformEnvNotFound(t *testing.T) {
	appSpec := minimalSpec(new("api"))
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"staging": {Context: "staging-eks"},
		},
	}
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.Error(t, err)
	require.NotNil(t, report)
	assert.Equal(t, spec.ErrCodePlatformEnvNotFound, report.ErrorCode)
}

// TestResolve_ErrorCode_DomainGap verifies platform spec behavior.
func TestResolve_ErrorCode_DomainGap(t *testing.T) {
	appSpec := minimalSpec(new("api"))
	// Platform has the env but not the domain referenced by the component.
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"production": {Context: "prod-eks", Domains: map[string]spec.PlatformDomain{}},
		},
	}
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.Error(t, err)
	require.NotNil(t, report)
	assert.Equal(t, spec.ErrCodeDomainGap, report.ErrorCode)
}

// TestResolve_ErrorCode_InvalidDNS verifies platform spec behavior.
func TestResolve_ErrorCode_InvalidDNS(t *testing.T) {
	// Subdomain with invalid characters (not dynamic).
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("INVALID_UPPER_CASE!!")}},
		},
	}
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.Error(t, err)
	require.NotNil(t, report)
	assert.Equal(t, spec.ErrCodeInvalidDNS, report.ErrorCode)
}

// TestResolve_ErrorCode_FQDNCollision verifies platform spec behavior.
func TestResolve_ErrorCode_FQDNCollision(t *testing.T) {
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"a": {Expose: &spec.Expose{Domain: "public"}},
			"b": {Expose: &spec.Expose{Domain: "public"}},
		},
	}
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.Error(t, err)
	require.NotNil(t, report)
	assert.Equal(t, spec.ErrCodeFQDNCollision, report.ErrorCode)
}

// TestResolve_DynamicSubdomainSkipsDNSValidation verifies platform spec behavior.
func TestResolve_DynamicSubdomainSkipsDNSValidation(t *testing.T) {
	// Subdomain contains ${PR_NUMBER} which is not a valid DNS label, but
	// the prescan marks it as dynamic so resolution should succeed.
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"review": {},
		},
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("${PR_NUMBER}")}},
		},
	}
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"review": {
				Context: "staging-eks",
				Domains: map[string]spec.PlatformDomain{
					"public": {BaseDomain: "review.example.com"},
				},
			},
		},
	}
	env := spec.NormalizeEnv("review/pr-42")
	substReport := spec.SubstitutionReport{DynamicSubdomains: map[string]bool{"api": true}}
	resolved, _, err := spec.Resolve(appSpec, platform, env, substReport)
	require.NoError(t, err, "dynamic subdomain should skip DNS validation")
	assert.Equal(t, "${PR_NUMBER}.review.example.com", resolved.Components["api"].FQDN)
}

// TestResolve_StaticInvalidSubdomainFailsDNS verifies platform spec behavior.
func TestResolve_StaticInvalidSubdomainFailsDNS(t *testing.T) {
	// Same invalid subdomain but NOT marked as dynamic: should fail.
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"review": {},
		},
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("${PR_NUMBER}")}},
		},
	}
	platform := &spec.PlatformConfig{
		APIVersion: "platform/v1-alpha.1",
		Environments: map[string]spec.PlatformEnvironment{
			"review": {
				Context: "staging-eks",
				Domains: map[string]spec.PlatformDomain{
					"public": {BaseDomain: "review.example.com"},
				},
			},
		},
	}
	env := spec.NormalizeEnv("review/pr-42")
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.Error(t, err)
	assert.Equal(t, spec.ErrCodeInvalidDNS, report.ErrorCode)
}
