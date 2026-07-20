package helm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

	// Startup is still active because liveness is on.
	assert.Contains(t, probes, "startupProbe")
	assert.NotContains(t, probes, "readinessProbe")
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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

	// Startup is still active because readiness is on.
	assert.Contains(t, probes, "startupProbe")
	assert.Contains(t, probes, "readinessProbe")
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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

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
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

	liveness := mustNestedMap(t, probes, "livenessProbe")
	assert.Equal(t, 10, liveness["periodSeconds"])
	assert.Equal(t, 6, liveness["failureThreshold"])
}

// TestBuildProbeValues_PortName verifies probes reference the named "http"
// container port.
func TestBuildProbeValues_PortName(t *testing.T) {
	t.Parallel()

	c := serviceComponent()
	probes, err := buildProbeValues(c)
	require.NoError(t, err)

	startup := mustNestedMap(t, probes, "startupProbe")
	tcpSocket := mustNestedMap(t, startup, "tcpSocket")
	assert.Equal(t, "http", tcpSocket["port"])
}

// TestBuildLivenessProbe_IntervalOnlyDefaultsRestartAfter verifies
// restartAfter defaulting.
func TestBuildLivenessProbe_IntervalOnlyDefaultsRestartAfter(t *testing.T) {
	t.Parallel()

	// interval provided, restartAfter omitted -> defaults to 60s -> 60/30=2
	p, err := buildLivenessProbe("", "30s", "")
	require.NoError(t, err)
	assert.Equal(t, 2, p["failureThreshold"])
	assert.Equal(t, 30, p["periodSeconds"])
}

// TestBuildLivenessProbe_RestartAfterOnlyDefaultsInterval verifies interval
// defaulting.
func TestBuildLivenessProbe_RestartAfterOnlyDefaultsInterval(t *testing.T) {
	t.Parallel()

	// restartAfter provided, interval omitted -> interval defaults to 10s -> 120/10=12
	p, err := buildLivenessProbe("", "", "2m")
	require.NoError(t, err)
	assert.Equal(t, 12, p["failureThreshold"])
	assert.Equal(t, 10, p["periodSeconds"])
}

// TestMapSpecToChartValues_EnvironmentFilterPrefixMatch verifies the
// component environments filter uses the same exact-then-prefix matching as
// spec.Resolve, so resolution and the generated chart agree on wildcard
// deploys like "review/pr-123".
func TestMapSpecToChartValues_EnvironmentFilterPrefixMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filter      []string
		environment string
		wantActive  bool
	}{
		{"exact match", []string{"production"}, "production", true},
		{"prefix match on wildcard deploy", []string{"review"}, "review/pr-123", true},
		{"no match", []string{"production"}, "staging", false},
		{"empty filter is active everywhere", nil, "staging", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			comp := serviceComponent()
			comp.Environments = tt.filter
			m := &spec.Spec{
				APIVersion: "v1-alpha.2",
				Project:    "shop",
				Components: map[string]spec.Component{"web": comp},
			}
			require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.2"))

			vals, err := MapSpecToChartValues(m, tt.environment, nil)
			require.NoError(t, err)

			_, active := vals["web"]
			assert.Equal(t, tt.wantActive, active)
		})
	}
}

// TestMapSpecToChartValues_SelfSignedTLS verifies selfSigned mode enables
// ingress TLS and emits a single ingress.secrets entry carrying the
// materialized cert/key, with no selfSigned or existingSecret key (the old
// template-side generation path is gone; certs are materialized in Go
// before this function runs).
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
				FQDN:       "api.127.0.0.1.nip.io",
				TLSMode:    spec.TLSModeSelfSigned,
				TLSCertPEM: []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n"),
				TLSKeyPEM:  []byte("-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n"),
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
	_, hasSelfSigned := ingress["selfSigned"]
	assert.False(t, hasSelfSigned, "selfSigned key must not be set; certs are emitted via ingress.secrets")
	_, hasExistingSecret := ingress["existingSecret"]
	assert.False(t, hasExistingSecret, "existingSecret must not be set for selfSigned mode")

	secrets, ok := ingress["secrets"].([]map[string]any)
	require.True(t, ok, "ingress.secrets must be a []map[string]any")
	require.Len(t, secrets, 1)
	assert.Equal(t, "api.127.0.0.1.nip.io-tls", secrets[0]["name"])
	assert.Equal(t, string(resolved.Components["api"].TLSCertPEM), secrets[0]["certificate"])
	assert.Equal(t, string(resolved.Components["api"].TLSKeyPEM), secrets[0]["key"])
}

// TestMapSpecToChartValues_SelfSignedTLS_Unmaterialized verifies that
// MapSpecToChartValues hard-errors when a selfSigned component's cert/key
// were never materialized, instead of silently falling back to the old
// non-deterministic template-side generation.
func TestMapSpecToChartValues_SelfSignedTLS_Unmaterialized(t *testing.T) {
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
				// TLSCertPEM/TLSKeyPEM intentionally left empty.
			},
		},
	}

	_, err := MapSpecToChartValues(m, "local", resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not materialized")
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

// TestMapSpecToChartValues_Autoscaling maps enabled HPA settings into values.
func TestMapSpecToChartValues_Autoscaling(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"web": {
				Role:  spec.ComponentRoleService,
				Image: "nginx:1.0.0",
				Port:  80,
				Autoscaling: &spec.Autoscaling{
					Enabled:     true,
					MinReplicas: 2,
					MaxReplicas: 10,
					Metrics: []spec.Metric{
						{Type: spec.MetricTypeCPU, Target: 70},
						{Type: spec.MetricTypeMemory, Target: 80},
					},
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.2"))

	vals, err := MapSpecToChartValues(m, "production", nil)
	require.NoError(t, err)

	web := mustNestedMap(t, vals, "web")
	as := mustNestedMap(t, web, "autoscaling")
	assert.Equal(t, true, as["enabled"])
	assert.Equal(t, 2, as["minReplicas"])
	assert.Equal(t, 10, as["maxReplicas"])
	assert.Equal(t, 70, as["targetCPU"])
	assert.Equal(t, 80, as["targetMemory"])
}

// TestMapSpecToChartValues_Profiles verifies merged profile fields land in
// component Helm values and the deployah.resolved block.
func TestMapSpecToChartValues_Profiles(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"web": {
				Role:  spec.ComponentRoleService,
				Image: "nginx:1.0.0",
				Port:  80,
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.2"))

	resolved := &spec.ResolvedSpec{
		Spec: m,
		Env:  spec.NormalizeEnv("production"),
		Components: map[string]spec.ResolvedComponent{
			"web": {
				Profiles: []string{"default", "public-web"},
				MergedProfile: &spec.PlatformProfile{
					NodeSelector: map[string]string{"workload": "general"},
					PodLabels:    map[string]string{"tier": "web"},
					PodAnnotations: map[string]string{
						"deployah.dev/profile": "public-web",
					},
					Tolerations: []corev1.Toleration{
						{Key: "ingress", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
					},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: new(true),
					},
					ContainerSecurityContext: &corev1.SecurityContext{
						ReadOnlyRootFilesystem: new(true),
					},
				},
			},
		},
	}

	vals, err := MapSpecToChartValues(m, "production", resolved)
	require.NoError(t, err)

	web := mustNestedMap(t, vals, "web")
	assert.Equal(t, map[string]string{"workload": "general"}, web["nodeSelector"])
	assert.Equal(t, map[string]string{"tier": "web"}, web["podLabels"])
	assert.Equal(t, map[string]string{"deployah.dev/profile": "public-web"}, web["podAnnotations"])

	labels, ok := web["commonLabels"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "web", labels["tier"])
	assert.Equal(t, "shop", labels["deployah.dev/project"])

	tolerations, ok := web["tolerations"].([]any)
	require.True(t, ok)
	require.Len(t, tolerations, 1)
	tol0, ok := tolerations[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ingress", tol0["key"])

	psc := mustNestedMap(t, web, "podSecurityContext")
	assert.Equal(t, true, psc["enabled"])
	assert.Equal(t, true, psc["runAsNonRoot"])

	csc := mustNestedMap(t, web, "containerSecurityContext")
	assert.Equal(t, true, csc["enabled"])
	assert.Equal(t, true, csc["readOnlyRootFilesystem"])

	deployah := mustNestedMap(t, vals, "deployah")
	resolvedBlock := mustNestedMap(t, deployah, "resolved")
	components := mustNestedMap(t, resolvedBlock, "components")
	webResolved := mustNestedMap(t, components, "web")
	assert.Equal(t, []string{"default", "public-web"}, webResolved["profiles"])
}
