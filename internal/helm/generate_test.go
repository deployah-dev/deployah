package helm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
)

// serviceComponent returns a minimal service component for test setup.
func serviceComponent() spec.Component {
	return spec.Component{
		Role:  spec.ComponentRoleService,
		Image: "my-app:latest",
		Port:  8080,
	}
}

// mustNestedMap returns a nested map[string]any value for key.
func mustNestedMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, exists := parent[key]
	require.Truef(t, exists, "expected key %q to exist", key)
	m, ok := v.(map[string]any)
	require.Truef(t, ok, "expected key %q to be map[string]any, got %T", key, v)
	return m
}

// TestBuildProbeValues_ZeroConfig verifies zero-config service probes are TCP.
func TestBuildProbeValues_ZeroConfig(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	probes := buildProbeValues(c)

	require.Contains(t, probes, "startupProbe")
	require.Contains(t, probes, "readinessProbe")
	require.Contains(t, probes, "livenessProbe")

	startup := mustNestedMap(t, probes, "startupProbe")
	assert.Equal(t, true, startup["enabled"])
	assert.Contains(t, startup, "tcpSocket")
	assert.NotContains(t, startup, "httpGet")
	assert.Equal(t, spec.DefaultStartupProbePeriod, startup["periodSeconds"])
	assert.Equal(t, spec.DefaultStartupProbeFailureThreshold, startup["failureThreshold"])
	assert.Equal(t, spec.DefaultStartupProbeTimeout, startup["timeoutSeconds"])

	readiness := mustNestedMap(t, probes, "readinessProbe")
	assert.Equal(t, true, readiness["enabled"])
	assert.Contains(t, readiness, "tcpSocket")
	assert.NotContains(t, readiness, "httpGet")

	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Equal(t, true, liveness["enabled"])
	assert.Contains(t, liveness, "tcpSocket")
	assert.NotContains(t, liveness, "httpGet")
}

// TestBuildProbeValues_ReadyPathUpgradesToHTTP verifies ready path upgrades
// startup and readiness probes to HTTP.
func TestBuildProbeValues_ReadyPathUpgradesToHTTP(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	c.Health = &spec.Health{
		Ready: &spec.HealthReady{Path: "/health"},
	}
	probes := buildProbeValues(c)

	require.Contains(t, probes, "startupProbe")
	startup := mustNestedMap(t, probes, "startupProbe")
	assert.Contains(t, startup, "httpGet")
	assert.NotContains(t, startup, "tcpSocket")
	httpGet := mustNestedMap(t, startup, "httpGet")
	assert.Equal(t, "/health", httpGet["path"])

	readiness := mustNestedMap(t, probes, "readinessProbe")
	assert.Contains(t, readiness, "httpGet")

	// Liveness stays TCP since alive has no path.
	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Contains(t, liveness, "tcpSocket")
	assert.NotContains(t, liveness, "httpGet")
}

// TestBuildProbeValues_BothPaths verifies all probes use HTTP with provided paths.
func TestBuildProbeValues_BothPaths(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	c.Health = &spec.Health{
		Ready: &spec.HealthReady{Path: "/health"},
		Alive: &spec.HealthAlive{Path: "/livez"},
	}
	probes := buildProbeValues(c)

	startup := mustNestedMap(t, probes, "startupProbe")
	assert.Contains(t, startup, "httpGet")
	assert.Equal(t, "/health", mustNestedMap(t, startup, "httpGet")["path"])

	readiness := mustNestedMap(t, probes, "readinessProbe")
	assert.Contains(t, readiness, "httpGet")
	assert.Equal(t, "/health", mustNestedMap(t, readiness, "httpGet")["path"])

	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Contains(t, liveness, "httpGet")
	assert.Equal(t, "/livez", mustNestedMap(t, liveness, "httpGet")["path"])
}

// TestBuildProbeValues_ReadyDisabled verifies ready=false omits readiness but
// keeps startup and liveness.
func TestBuildProbeValues_ReadyDisabled(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	c.Health = &spec.Health{
		Ready: &spec.HealthReady{Disabled: true},
	}
	probes := buildProbeValues(c)

	// Startup is still active because liveness is on.
	assert.Contains(t, probes, "startupProbe")
	// Readiness is absent.
	assert.NotContains(t, probes, "readinessProbe")
	// Liveness defaults to TCP.
	assert.Contains(t, probes, "livenessProbe")
}

// TestBuildProbeValues_AliveDisabled verifies alive=false omits liveness but
// keeps startup and readiness.
func TestBuildProbeValues_AliveDisabled(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	c.Health = &spec.Health{
		Alive: &spec.HealthAlive{Disabled: true},
	}
	probes := buildProbeValues(c)

	// Startup is still active because readiness is on.
	assert.Contains(t, probes, "startupProbe")
	assert.Contains(t, probes, "readinessProbe")
	// Liveness is absent.
	assert.NotContains(t, probes, "livenessProbe")
}

// TestBuildProbeValues_BothDisabled verifies both checks can be disabled.
func TestBuildProbeValues_BothDisabled(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	c.Health = &spec.Health{
		Ready: &spec.HealthReady{Disabled: true},
		Alive: &spec.HealthAlive{Disabled: true},
	}
	probes := buildProbeValues(c)

	assert.Empty(t, probes)
}

// TestBuildProbeValues_CustomIntervalAndRestartAfter verifies custom liveness
// timing values.
func TestBuildProbeValues_CustomIntervalAndRestartAfter(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	c.Health = &spec.Health{
		Alive: &spec.HealthAlive{
			Path:         "/livez",
			Interval:     "30s",
			RestartAfter: "2m", // 120s / 30s = 4
		},
	}
	probes := buildProbeValues(c)

	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Equal(t, 30, liveness["periodSeconds"])
	assert.Equal(t, 4, liveness["failureThreshold"])
}

// TestBuildProbeValues_RestartAfterRoundsUp verifies failureThreshold uses
// ceil division.
func TestBuildProbeValues_RestartAfterRoundsUp(t *testing.T) {
	t.Parallel()

	// 65s / 10s = 6.5 -> ceil = 7
	c := serviceComponent()
	c.Health = &spec.Health{
		Alive: &spec.HealthAlive{
			Interval:     "10s",
			RestartAfter: "65s",
		},
	}
	probes := buildProbeValues(c)

	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Equal(t, 7, liveness["failureThreshold"])
}

// TestBuildProbeValues_DefaultLivenessTimingWhenFieldsOmitted verifies default
// liveness values.
func TestBuildProbeValues_DefaultLivenessTimingWhenFieldsOmitted(t *testing.T) {
	t.Parallel()

	// Both interval and restartAfter omitted; should use defaults 10s/60s -> threshold 6.
	c := serviceComponent()
	c.Health = &spec.Health{
		Alive: &spec.HealthAlive{Path: "/livez"},
	}
	probes := buildProbeValues(c)

	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Equal(t, 10, liveness["periodSeconds"])
	assert.Equal(t, 6, liveness["failureThreshold"])
}

// TestBuildProbeValues_PortName verifies probes reference the named "http"
// container port.
func TestBuildProbeValues_PortName(t *testing.T) {
	t.Parallel()

	// TCP probes must reference the named port "http".
	c := serviceComponent()
	probes := buildProbeValues(c)

	startup := mustNestedMap(t, probes, "startupProbe")
	tcpSocket := mustNestedMap(t, startup, "tcpSocket")
	assert.Equal(t, "http", tcpSocket["port"])
}

// TestBuildLivenessProbe_IntervalOnlyDefaultsRestartAfter verifies
// restartAfter defaulting.
func TestBuildLivenessProbe_IntervalOnlyDefaultsRestartAfter(t *testing.T) {
	t.Parallel()

	// interval provided, restartAfter omitted -> defaults to 60s -> 60/30=2
	p := buildLivenessProbe("", "30s", "")
	assert.Equal(t, 2, p["failureThreshold"])
	assert.Equal(t, 30, p["periodSeconds"])
}

// TestBuildLivenessProbe_RestartAfterOnlyDefaultsInterval verifies interval
// defaulting.
func TestBuildLivenessProbe_RestartAfterOnlyDefaultsInterval(t *testing.T) {
	t.Parallel()

	// restartAfter provided, interval omitted -> interval defaults to 10s -> 120/10=12
	p := buildLivenessProbe("", "", "2m")
	assert.Equal(t, 12, p["failureThreshold"])
	assert.Equal(t, 10, p["periodSeconds"])
}

// TestMapSpecToChartValues_SelfSignedTLS verifies that the selfSigned TLS mode
// sets ingress.selfSigned:true and does not set existingSecret (the chart
// derives the secret name from the hostname).
func TestMapSpecToChartValues_SelfSignedTLS(t *testing.T) {
	t.Parallel()

	subdomain := "api"
	m := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"local": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "shop-api:latest",
				Port:  8080,
				Expose: &spec.Expose{
					Domain:    "public",
					Subdomain: &subdomain,
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.2"))

	resolved := &spec.ResolvedSpec{
		Spec: m,
		Env:  spec.NormalizeEnv("local"),
		Components: map[string]spec.ResolvedComponent{
			"api": {
				FQDN:    "api.127.0.0.1.nip.io",
				TLSMode: spec.TLSModeSelfSigned,
			},
		},
	}

	vals, err := MapSpecToChartValues(m, "local", resolved)
	require.NoError(t, err)

	apiVals := mustNestedMap(t, vals, "api")
	ingress := mustNestedMap(t, apiVals, "ingress")

	assert.Equal(t, true, ingress["enabled"])
	assert.Equal(t, "api.127.0.0.1.nip.io", ingress["hostname"])
	assert.Equal(t, true, ingress["tls"])
	assert.Equal(t, true, ingress["selfSigned"], "selfSigned must be true for selfSigned TLS mode")
	_, hasExistingSecret := ingress["existingSecret"]
	assert.False(t, hasExistingSecret, "existingSecret must not be set for selfSigned mode")
}

// TestMapSpecToChartValues_SecretNameTLS verifies secretName mode sets
// existingSecret.
func TestMapSpecToChartValues_SecretNameTLS(t *testing.T) {
	t.Parallel()

	subdomain := "api"
	m := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "shop-api:latest",
				Port:  8080,
				Expose: &spec.Expose{
					Domain:    "public",
					Subdomain: &subdomain,
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.2"))

	resolved := &spec.ResolvedSpec{
		Spec: m,
		Env:  spec.NormalizeEnv("production"),
		Components: map[string]spec.ResolvedComponent{
			"api": {
				FQDN:          "api.example.com",
				TLSMode:       spec.TLSModeSecretName,
				TLSSecretName: "wildcard-example-com",
			},
		},
	}

	vals, err := MapSpecToChartValues(m, "production", resolved)
	require.NoError(t, err)

	apiVals := mustNestedMap(t, vals, "api")
	ingress := mustNestedMap(t, apiVals, "ingress")

	assert.Equal(t, true, ingress["tls"])
	assert.Equal(t, "wildcard-example-com", ingress["existingSecret"])
}

// TestMapSpecToChartValues_CertManagerTLS verifies certManager mode sets the
// annotation.
func TestMapSpecToChartValues_CertManagerTLS(t *testing.T) {
	t.Parallel()

	subdomain := "api"
	m := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "shop-api:latest",
				Port:  8080,
				Expose: &spec.Expose{
					Domain:    "public",
					Subdomain: &subdomain,
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.2"))

	resolved := &spec.ResolvedSpec{
		Spec: m,
		Env:  spec.NormalizeEnv("production"),
		Components: map[string]spec.ResolvedComponent{
			"api": {
				FQDN:      "api.example.com",
				TLSMode:   spec.TLSModeCertManager,
				TLSIssuer: "letsencrypt-prod",
			},
		},
	}

	vals, err := MapSpecToChartValues(m, "production", resolved)
	require.NoError(t, err)

	apiVals := mustNestedMap(t, vals, "api")
	ingress := mustNestedMap(t, apiVals, "ingress")

	assert.Equal(t, true, ingress["tls"])
	annotations, ok := ingress["annotations"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "letsencrypt-prod", annotations["cert-manager.io/cluster-issuer"])
}
