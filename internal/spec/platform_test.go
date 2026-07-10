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

// TestResolve_UnknownEnvironmentNameWarnings verifies typo protection: spec
// environment overrides and component filter entries that match nothing in
// the platform registry warn, while prefix-style entries stay warning-free.
func TestResolve_UnknownEnvironmentNameWarnings(t *testing.T) {
	appSpec := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
			"qa":         {},
		},
		Components: map[string]spec.Component{
			"api":    {Environments: []string{"production"}},
			"review": {Environments: []string{"production/*"}},
			"worker": {Environments: []string{"stagign"}},
		},
	}
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")

	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)

	require.Len(t, report.Warnings, 2)
	assert.Contains(t, report.Warnings[0], `"qa"`)
	assert.Contains(t, report.Warnings[1], `"stagign"`)
	for _, w := range report.Warnings {
		assert.NotContains(t, w, "production/*",
			"prefix-style filter entries must not warn")
	}
}

// TestCrossCheckPlatformReferences verifies expose.domain typos are caught
// against the union of platform domains, ${VAR} domains are skipped, and
// unknown environment names come back as warnings.
func TestCrossCheckPlatformReferences(t *testing.T) {
	appSpec := &spec.Spec{
		Environments: map[string]spec.Environment{"qa": {}},
		Components: map[string]spec.Component{
			"api":     {Expose: &spec.Expose{Domain: "public"}},
			"admin":   {Expose: &spec.Expose{Domain: "pubic"}},
			"dynamic": {Expose: &spec.Expose{Domain: "${DOMAIN}"}},
		},
	}
	platform := minimalPlatform()

	problems, warnings := spec.CrossCheckPlatformReferences(appSpec, platform)

	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], `"pubic"`)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], `"qa"`)

	problems, warnings = spec.CrossCheckPlatformReferences(appSpec, nil)
	assert.Empty(t, problems)
	assert.Empty(t, warnings)
}

// TestResolve_ApexMode verifies apex: true resolves to the bare baseDomain.
func TestResolve_ApexMode(t *testing.T) {
	appSpec := minimalSpec(nil)
	appSpec.Components["api"] = spec.Component{
		Expose: &spec.Expose{Domain: "public", Apex: true},
	}
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	resolved, _, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)
	rc := resolved.Components["api"]
	assert.Equal(t, "example.com", rc.FQDN)
}

// TestResolve_DefaultSubdomainIsComponentName verifies a nil subdomain
// resolves to <component>.<baseDomain>.
func TestResolve_DefaultSubdomainIsComponentName(t *testing.T) {
	appSpec := minimalSpec(nil)
	platform := minimalPlatform()
	env := spec.NormalizeEnv("production")
	resolved, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", resolved.Components["api"].FQDN)

	var hostSource string
	for _, f := range report.Fields {
		if f.Component == "api" && f.Path == "expose.host" {
			hostSource = f.Source
		}
	}
	assert.Contains(t, hostSource, "component name (default)")
}

// TestResolve_DefaultDomain verifies an empty domain resolves to the
// environment's only domain, and to the default-marked one among several.
func TestResolve_DefaultDomain(t *testing.T) {
	appSpec := minimalSpec(nil)
	appSpec.Components["api"] = spec.Component{Expose: &spec.Expose{}}
	env := spec.NormalizeEnv("production")

	// Single domain: used automatically.
	resolved, _, err := spec.Resolve(appSpec, minimalPlatform(), env, spec.SubstitutionReport{})
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", resolved.Components["api"].FQDN)

	// Several domains, one marked default.
	platform := minimalPlatform()
	prod := platform.Environments["production"]
	prod.Domains["internal"] = spec.PlatformDomain{BaseDomain: "internal.corp", Default: true}
	platform.Environments["production"] = prod
	resolved, _, err = spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.NoError(t, err)
	assert.Equal(t, "api.internal.corp", resolved.Components["api"].FQDN)

	// Several domains, none marked default: error listing the keys.
	prod.Domains["internal"] = spec.PlatformDomain{BaseDomain: "internal.corp"}
	platform.Environments["production"] = prod
	_, report, err := spec.Resolve(appSpec, platform, env, spec.SubstitutionReport{})
	require.Error(t, err)
	assert.Equal(t, spec.ErrCodeDomainGap, report.ErrorCode)
	assert.Contains(t, err.Error(), `"internal", "public"`)
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
			"a": {Expose: &spec.Expose{Domain: "public", Apex: true}}, // apex
			"b": {Expose: &spec.Expose{Domain: "public", Apex: true}}, // apex - collision
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

// TestLoadPlatform_RejectsTwoDefaultDomains verifies at most one domain per
// environment may carry default: true.
func TestLoadPlatform_RejectsTwoDefaultDomains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")
	doc := `apiVersion: platform/v1-alpha.1
environments:
  production:
    context: prod
    domains:
      public:
        baseDomain: example.com
        default: true
      internal:
        baseDomain: internal.corp
        default: true
`
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))

	_, err := spec.LoadPlatform(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at most one domain may set default")
}

// TestScaffoldPlatformFile_RegistersAllEnvironments verifies that every
// selected environment is registered: "local" with the full kind entry,
// the rest as empty entries the user fills in later.
func TestScaffoldPlatformFile_RegistersAllEnvironments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")

	created, err := spec.ScaffoldPlatformFile(path, "127.0.0.1", []string{"local", "production"})
	require.NoError(t, err)
	assert.True(t, created)

	p, loadErr := spec.LoadPlatform(path)
	require.NoError(t, loadErr)
	require.Len(t, p.Environments, 2)
	local, hasLocal := p.Environments["local"]
	require.True(t, hasLocal)
	assert.Equal(t, "kind-deployah", local.Context)
	production, hasProduction := p.Environments["production"]
	require.True(t, hasProduction)
	assert.Empty(t, production.Context, "non-local entries are registered without a context")
}

// TestScaffoldPlatformFile_NoLocalStillCreatesFile verifies a file is
// written even without "local": the platform file is the environment
// registry, so every selected name must be registered.
func TestScaffoldPlatformFile_NoLocalStillCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")

	created, err := spec.ScaffoldPlatformFile(path, "127.0.0.1", []string{"staging", "production"})
	require.NoError(t, err)
	assert.True(t, created)

	p, loadErr := spec.LoadPlatform(path)
	require.NoError(t, loadErr)
	require.Len(t, p.Environments, 2)
}

// TestScaffoldPlatformFile_NoEnvironmentsWritesNothing verifies nothing is
// written for an empty name list: the platform schema requires at least one
// environment entry.
func TestScaffoldPlatformFile_NoEnvironmentsWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")

	created, err := spec.ScaffoldPlatformFile(path, "127.0.0.1", nil)
	require.NoError(t, err)
	assert.False(t, created)

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "no platform file should have been written")
}

// TestScaffoldPlatformFile_DoesNotOverwriteExisting verifies an existing
// platform file is left untouched regardless of envNames.
func TestScaffoldPlatformFile_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.platform.yaml")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: platform/v1-alpha.1\nenvironments:\n  prod:\n    context: prod\n"), 0o600))

	created, err := spec.ScaffoldPlatformFile(path, "127.0.0.1", []string{"local"})
	require.NoError(t, err)
	assert.False(t, created)

	data, readErr := os.ReadFile(path) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "prod")
	assert.NotContains(t, string(data), "local")
}

// TestMissingPlatformEnvironments_NilPlatform verifies every requested name
// is reported missing when no platform config exists yet.
func TestMissingPlatformEnvironments_NilPlatform(t *testing.T) {
	missing := spec.MissingPlatformEnvironments(nil, []string{"production", "staging"})
	assert.Equal(t, []string{"production", "staging"}, missing)
}

// TestMissingPlatformEnvironments_PartialCoverage verifies only the names
// absent from platform.Environments are reported, sorted for stable output.
func TestMissingPlatformEnvironments_PartialCoverage(t *testing.T) {
	platform := &spec.PlatformConfig{
		Environments: map[string]spec.PlatformEnvironment{
			"local": {Context: "kind-deployah"},
		},
	}
	missing := spec.MissingPlatformEnvironments(platform, []string{"staging", "local", "production"})
	assert.Equal(t, []string{"production", "staging"}, missing)
}

// TestMissingPlatformEnvironments_FullCoverage verifies an empty slice is
// returned, not nil, is acceptable when every requested name is covered.
func TestMissingPlatformEnvironments_FullCoverage(t *testing.T) {
	platform := &spec.PlatformConfig{
		Environments: map[string]spec.PlatformEnvironment{
			"local": {Context: "kind-deployah"},
		},
	}
	missing := spec.MissingPlatformEnvironments(platform, []string{"local"})
	assert.Empty(t, missing)
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
			"a": {Expose: &spec.Expose{Domain: "public", Apex: true}},
			"b": {Expose: &spec.Expose{Domain: "public", Apex: true}},
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
