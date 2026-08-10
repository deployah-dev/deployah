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
	p, err := buildLivenessProbe(nil, "", "30s", "")
	require.NoError(t, err)
	assert.Equal(t, 2, p["failureThreshold"])
	assert.Equal(t, 30, p["periodSeconds"])
}

// TestBuildLivenessProbe_RestartAfterOnlyDefaultsInterval verifies interval
// defaulting.
func TestBuildLivenessProbe_RestartAfterOnlyDefaultsInterval(t *testing.T) {
	t.Parallel()

	// restartAfter provided, interval omitted -> interval defaults to 10s -> 120/10=12
	p, err := buildLivenessProbe(nil, "", "", "2m")
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
				APIVersion: "v1-alpha.4",
				Project:    "shop",
				Components: map[string]spec.Component{"web": comp},
			}
			require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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
		APIVersion: "v1-alpha.4",
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
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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
		APIVersion: "v1-alpha.4",
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
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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
		APIVersion: "v1-alpha.4",
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
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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
		APIVersion: "v1-alpha.4",
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
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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
		APIVersion: "v1-alpha.4",
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
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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
		APIVersion: "v1-alpha.4",
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
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

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

	annotations, ok := web["commonAnnotations"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, spec.SourceSpec, annotations[spec.AnnotationSource])
	assert.Equal(t, "shop", annotations[spec.AnnotationProject])

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

// TestMapSpecToChartValues_StatefulComponent maps StatefulSet workload values.
func TestMapSpecToChartValues_StatefulComponent(t *testing.T) {
	t.Parallel()

	replicas := 2
	m := &spec.Spec{
		APIVersion: "v1-alpha.4",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"db": {
				Role:     spec.ComponentRoleService,
				Kind:     spec.ComponentKindStateful,
				Image:    "postgres:16",
				Port:     5432,
				Replicas: &replicas,
				Persistence: &spec.Persistence{
					Size:      "20Gi",
					MountPath: "/var/lib/postgresql/data",
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

	resolved := &spec.ResolvedSpec{
		Spec: m,
		Env:  spec.NormalizeEnv("production"),
		Components: map[string]spec.ResolvedComponent{
			"db": {
				StorageClass: "fast-ssd",
				MergedProfile: &spec.PlatformProfile{
					PVCRetentionPolicy: &spec.PVCRetentionPolicy{
						WhenDeleted: "Delete",
						WhenScaled:  "Retain",
					},
				},
			},
		},
	}

	vals, err := MapSpecToChartValues(m, "production", resolved)
	require.NoError(t, err)

	db := mustNestedMap(t, vals, "db")
	assert.Equal(t, "StatefulSet", db["workloadKind"])
	assert.Equal(t, 2, db["replicaCount"])

	persistence := mustNestedMap(t, db, "persistence")
	assert.Equal(t, true, persistence["enabled"])
	assert.Equal(t, "20Gi", persistence["size"])
	assert.Equal(t, "/var/lib/postgresql/data", persistence["mountPath"])
	assert.Equal(t, "fast-ssd", persistence["storageClass"])
	assert.Equal(t, []string{"ReadWriteOncePod"}, persistence["accessModes"])

	statefulSet := mustNestedMap(t, db, "statefulSet")
	retention := mustNestedMap(t, statefulSet, "persistentVolumeClaimRetentionPolicy")
	assert.Equal(t, "Delete", retention["whenDeleted"])
	assert.Equal(t, "Retain", retention["whenScaled"])

	deployah := mustNestedMap(t, vals, "deployah")
	resolvedBlock := mustNestedMap(t, deployah, "resolved")
	components := mustNestedMap(t, resolvedBlock, "components")
	dbResolved := mustNestedMap(t, components, "db")
	assert.Equal(t, "StatefulSet", dbResolved["workloadKind"])
	assert.Equal(t, "20Gi", dbResolved["persistenceSize"])
}

// TestMapSpecToChartValues_StatefulIdentityOnly maps StatefulSet without PVC.
func TestMapSpecToChartValues_StatefulIdentityOnly(t *testing.T) {
	t.Parallel()

	replicas := 2
	m := &spec.Spec{
		APIVersion: "v1-alpha.4",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"peer": {
				Role:     spec.ComponentRoleService,
				Kind:     spec.ComponentKindStateful,
				Image:    "redis:7-alpine",
				Port:     6379,
				Replicas: &replicas,
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

	vals, err := MapSpecToChartValues(m, "production", nil)
	require.NoError(t, err)

	peer := mustNestedMap(t, vals, "peer")
	assert.Equal(t, "StatefulSet", peer["workloadKind"])
	assert.Equal(t, 2, peer["replicaCount"])
	_, hasPersistence := peer["persistence"]
	assert.False(t, hasPersistence, "identity-only stateful must not enable persistence")
	assert.Contains(t, peer, "statefulSet")
}

// TestMapSpecToChartValues_StatelessWithPersistence forces Recreate strategy.
func TestMapSpecToChartValues_StatelessWithPersistence(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		APIVersion: "v1-alpha.4",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"web": {
				Role:  spec.ComponentRoleService,
				Kind:  spec.ComponentKindStateless,
				Image: "nginx:1.0.0",
				Port:  80,
				Persistence: &spec.Persistence{
					Size:      "1Gi",
					MountPath: "/data",
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

	vals, err := MapSpecToChartValues(m, "production", nil)
	require.NoError(t, err)

	web := mustNestedMap(t, vals, "web")
	assert.Equal(t, "Deployment", web["workloadKind"])
	persistence := mustNestedMap(t, web, "persistence")
	assert.Equal(t, true, persistence["enabled"])
	assert.Equal(t, []string{"ReadWriteOnce"}, persistence["accessModes"])
	strategy := mustNestedMap(t, web, "updateStrategy")
	assert.Equal(t, "Recreate", strategy["type"])
}

// TestMapSpecToChartValues_Replicas maps replicaCount from the spec.
func TestMapSpecToChartValues_Replicas(t *testing.T) {
	t.Parallel()

	replicas := 3
	m := &spec.Spec{
		APIVersion: "v1-alpha.4",
		Project:    "shop",
		Environments: map[string]spec.Environment{
			"production": {},
		},
		Components: map[string]spec.Component{
			"web": {
				Role:     spec.ComponentRoleService,
				Image:    "nginx:1.0.0",
				Port:     80,
				Replicas: &replicas,
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, "v1-alpha.4"))

	vals, err := MapSpecToChartValues(m, "production", nil)
	require.NoError(t, err)
	web := mustNestedMap(t, vals, "web")
	assert.Equal(t, 3, web["replicaCount"])
	assert.Equal(t, "Deployment", web["workloadKind"])
}

// TestParseContainerImage verifies repository/tag/digest extraction across
// bare names, tagged references, digest references, and malformed input.
func TestParseContainerImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		imageRef   string
		wantRepo   string
		wantTagDig string
	}{
		{name: "empty string returns empty repo and tag", imageRef: "", wantRepo: "", wantTagDig: ""},
		{name: "bare name normalizes to docker.io/library and has no tag", imageRef: "nginx", wantRepo: "docker.io/library/nginx", wantTagDig: ""},
		{name: "name with tag", imageRef: "nginx:1.25", wantRepo: "docker.io/library/nginx", wantTagDig: "1.25"},
		{name: "full registry path with tag", imageRef: "ghcr.io/org/repo:v1", wantRepo: "ghcr.io/org/repo", wantTagDig: "v1"},
		{
			name:       "digest reference prefers digest over tag",
			imageRef:   "nginx@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			wantRepo:   "docker.io/library/nginx",
			wantTagDig: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		},
		{
			name:       "unparseable reference falls back to raw string with empty tag",
			imageRef:   "InvalidUPPERCASE",
			wantRepo:   "InvalidUPPERCASE",
			wantTagDig: "",
		},
		{
			name:       "garbage input falls back to raw string with empty tag",
			imageRef:   "not a valid ref!!",
			wantRepo:   "not a valid ref!!",
			wantTagDig: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, tagOrDigest := parseContainerImage(tt.imageRef)
			assert.Equal(t, tt.wantRepo, repo)
			assert.Equal(t, tt.wantTagDig, tagOrDigest)
		})
	}
}

// TestGenerateReleaseName verifies release name composition, including
// normalization of wildcard "/" environment names to their k8s-safe form.
func TestGenerateReleaseName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		projectName     string
		environmentName string
		want            string
	}{
		{name: "plain names", projectName: "shop", environmentName: "production", want: "shop-production"},
		{name: "wildcard environment normalized", projectName: "shop", environmentName: "review/pr-42", want: "shop-review-pr-42"},
		{name: "empty environment", projectName: "shop", environmentName: "", want: "shop-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := GenerateReleaseName(tt.projectName, tt.environmentName)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestToValuesMap verifies JSON round-tripping of arbitrary structs and maps
// into Helm-friendly map[string]any, including nil, marshal-failure, and
// nested-value cases.
func TestToValuesMap(t *testing.T) {
	t.Parallel()

	type nested struct {
		Value int `json:"value"`
	}
	type outer struct {
		Name   string `json:"name"`
		Nested nested `json:"nested"`
	}

	tests := []struct {
		name    string
		input   any
		wantErr bool
		check   func(t *testing.T, out map[string]any)
	}{
		{
			name:  "nil input yields empty map",
			input: nil,
			check: func(t *testing.T, out map[string]any) { t.Helper(); assert.Empty(t, out) },
		},
		{
			name:  "struct with nested fields round-trips",
			input: outer{Name: "x", Nested: nested{Value: 5}},
			check: func(t *testing.T, out map[string]any) {
				t.Helper()
				assert.Equal(t, "x", out["name"])
				inner, ok := out["nested"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 5, inner["value"])
			},
		},
		{
			name:    "unmarshalable value returns error",
			input:   struct{ C chan int }{C: make(chan int)},
			wantErr: true,
		},
		{
			name:  "empty struct yields empty map, not nil",
			input: struct{}{},
			check: func(t *testing.T, out map[string]any) {
				t.Helper()
				assert.NotNil(t, out)
				assert.Empty(t, out)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := toValuesMap(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, out)
		})
	}
}

// TestToValuesSlice verifies JSON round-tripping of arbitrary slices into
// []any, including nil, empty, nested, and marshal-failure cases.
func TestToValuesSlice(t *testing.T) {
	t.Parallel()

	type item struct {
		Key string `json:"key"`
	}

	tests := []struct {
		name    string
		input   any
		wantErr bool
		check   func(t *testing.T, out []any)
	}{
		{
			name:  "nil input yields nil slice",
			input: nil,
			check: func(t *testing.T, out []any) { t.Helper(); assert.Nil(t, out) },
		},
		{
			name:  "empty slice round-trips to empty slice",
			input: []int{},
			check: func(t *testing.T, out []any) { t.Helper(); assert.Empty(t, out) },
		},
		{
			name:  "slice of structs round-trips nested objects",
			input: []item{{Key: "a"}, {Key: "b"}},
			check: func(t *testing.T, out []any) {
				t.Helper()
				require.Len(t, out, 2)
				first, ok := out[0].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "a", first["key"])
			},
		},
		{
			name:    "unmarshalable value returns error",
			input:   []chan int{make(chan int)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := toValuesSlice(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, out)
		})
	}
}

// TestNormalizeJSONNumbers verifies whole-number float64 values from JSON
// decoding are converted to int, while fractional values and other types
// are left untouched, across nested maps and slices.
func TestNormalizeJSONNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input map[string]any
		want  map[string]any
	}{
		{
			name:  "whole float in map becomes int",
			input: map[string]any{"periodSeconds": 5.0},
			want:  map[string]any{"periodSeconds": 5},
		},
		{
			name:  "fractional float in map is untouched",
			input: map[string]any{"cpu": 2.5},
			want:  map[string]any{"cpu": 2.5},
		},
		{
			name:  "nested maps are normalized recursively",
			input: map[string]any{"outer": map[string]any{"inner": 3.0}},
			want:  map[string]any{"outer": map[string]any{"inner": 3}},
		},
		{
			name:  "nested slices are normalized recursively",
			input: map[string]any{"items": []any{1.0, 2.5, map[string]any{"count": 4.0}}},
			want:  map[string]any{"items": []any{1, 2.5, map[string]any{"count": 4}}},
		},
		{
			name:  "non-numeric types are untouched",
			input: map[string]any{"name": "web", "enabled": true, "count": nil},
			want:  map[string]any{"name": "web", "enabled": true, "count": nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			normalizeJSONNumbers(tt.input)
			assert.Equal(t, tt.want, tt.input)
		})
	}
}

func TestMapSpecToChartValues_WorkerStateless(t *testing.T) {
	t.Parallel()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"worker": {
				Role:    spec.ComponentRoleWorker,
				Image:   "ghcr.io/acme/worker:1.0.0",
				Command: []string{"sleep", "infinity"},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	values, err := MapSpecToChartValues(manifest, "dev", nil)
	require.NoError(t, err)
	worker := mustNestedMap(t, values, "worker")
	assert.Equal(t, "Deployment", worker["workloadKind"])
	assert.Nil(t, worker["ports"])
	svc := mustNestedMap(t, worker, "service")
	assert.Equal(t, false, svc["enabled"])
	assert.Equal(t, 60, worker["terminationGracePeriodSeconds"])
	assert.Nil(t, worker["startupProbe"])
	assert.Nil(t, worker["readinessProbe"])
	resolved := mustNestedMap(t, mustNestedMap(t, mustNestedMap(t, values, "deployah"), "resolved"), "components")
	assert.Equal(t, "worker", mustNestedMap(t, resolved, "worker")["role"])
}

func TestMapSpecToChartValues_WorkerStatefulIdentityPort(t *testing.T) {
	t.Parallel()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"worker": {
				Role:  spec.ComponentRoleWorker,
				Kind:  spec.ComponentKindStateful,
				Image: "ghcr.io/acme/worker:1.0.0",
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	values, err := MapSpecToChartValues(manifest, "dev", nil)
	require.NoError(t, err)
	worker := mustNestedMap(t, values, "worker")
	assert.Equal(t, "StatefulSet", worker["workloadKind"])
	ports, ok := worker["ports"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, ports, 1)
	assert.Equal(t, spec.IdentityPortName, ports[0]["name"])
	assert.Equal(t, spec.IdentityPortNumber, ports[0]["containerPort"])
	svc := mustNestedMap(t, worker, "service")
	assert.Equal(t, false, svc["enabled"])
	svcPorts, ok := svc["ports"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, svcPorts, 1)
	assert.Equal(t, spec.IdentityPortName, svcPorts[0]["name"])
}

func TestMapSpecToChartValues_WorkerMetrics(t *testing.T) {
	t.Parallel()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"worker": {
				Role:    spec.ComponentRoleWorker,
				Image:   "ghcr.io/acme/worker:1.0.0",
				Metrics: &spec.ComponentMetrics{Port: 9090},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	profile := &spec.PlatformProfile{Metrics: &spec.ProfileMetrics{
		MonitorLabels: map[string]string{"release": "kube-prometheus-stack"},
	}}
	resolved := &spec.ResolvedSpec{Components: map[string]spec.ResolvedComponent{
		"worker": {MergedProfile: profile},
	}}
	values, err := MapSpecToChartValues(manifest, "dev", resolved)
	require.NoError(t, err)
	worker := mustNestedMap(t, values, "worker")
	ports, ok := worker["ports"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, ports, 1)
	assert.Equal(t, spec.MetricsPortName, ports[0]["name"])
	assert.Equal(t, 9090, ports[0]["containerPort"])
	pm := mustNestedMap(t, worker, "podMonitor")
	assert.Equal(t, true, pm["enabled"])
	assert.Equal(t, spec.MetricsPortName, pm["port"])
	assert.Equal(t, "/metrics", pm["path"])
	assert.Equal(t, map[string]string{"release": "kube-prometheus-stack"}, pm["labels"])
}

func TestMapSpecToChartValues_MonitorProfileFields(t *testing.T) {
	t.Parallel()
	honor := true
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/api:1.0.0",
				Port:  8080,
				Metrics: &spec.ComponentMetrics{
					Port:          9090,
					Interval:      "10s",
					ScrapeTimeout: "5s",
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	profile := &spec.PlatformProfile{Metrics: &spec.ProfileMetrics{
		MonitorLabels:     map[string]string{"release": "prom"},
		MonitorNamespace:  "monitoring",
		Interval:          "30s",
		ScrapeTimeout:     "10s",
		JobLabel:          "app",
		HonorLabels:       &honor,
		Annotations:       map[string]string{"team": "obs"},
		Relabelings:       []any{map[string]any{"action": "keep"}},
		MetricRelabelings: []any{map[string]any{"action": "drop"}},
	}}
	resolved := &spec.ResolvedSpec{Components: map[string]spec.ResolvedComponent{
		"api": {MergedProfile: profile},
	}}
	values, err := MapSpecToChartValues(manifest, "dev", resolved)
	require.NoError(t, err)
	sm := mustNestedMap(t, mustNestedMap(t, values, "api"), "serviceMonitor")
	assert.Equal(t, true, sm["enabled"])
	assert.Equal(t, "monitoring", sm["namespace"])
	assert.Equal(t, "10s", sm["interval"], "component interval overrides profile")
	assert.Equal(t, "5s", sm["scrapeTimeout"], "component scrapeTimeout overrides profile")
	assert.Equal(t, "app", sm["jobLabel"])
	assert.Equal(t, true, sm["honorLabels"])
	assert.Equal(t, map[string]string{"team": "obs"}, sm["annotations"])
	assert.Equal(t, map[string]string{"release": "prom"}, sm["labels"])
	require.Len(t, sm["relabelings"], 1)
	require.Len(t, sm["metricRelabelings"], 1)
}

func TestMapSpecToChartValues_NilProfileMetricsSkipped(t *testing.T) {
	t.Parallel()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {
				Role:    spec.ComponentRoleService,
				Image:   "ghcr.io/acme/api:1.0.0",
				Port:    8080,
				Metrics: &spec.ComponentMetrics{},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	resolved := &spec.ResolvedSpec{Components: map[string]spec.ResolvedComponent{
		"api": {MergedProfile: &spec.PlatformProfile{}},
	}}
	values, err := MapSpecToChartValues(manifest, "dev", resolved)
	require.NoError(t, err)
	sm := mustNestedMap(t, mustNestedMap(t, values, "api"), "serviceMonitor")
	assert.Equal(t, true, sm["enabled"])
	assert.Nil(t, sm["labels"])
	assert.Nil(t, sm["namespace"])
}

func TestMapSpecToChartValues_ServiceMetricsDedicatedPort(t *testing.T) {
	t.Parallel()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {
				Role:    spec.ComponentRoleService,
				Image:   "ghcr.io/acme/api:1.0.0",
				Port:    8080,
				Metrics: &spec.ComponentMetrics{Port: 9090, Path: "/metrics"},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	profile := &spec.PlatformProfile{Metrics: &spec.ProfileMetrics{
		MonitorLabels: map[string]string{"release": "prom"},
	}}
	resolved := &spec.ResolvedSpec{Components: map[string]spec.ResolvedComponent{
		"api": {MergedProfile: profile},
	}}
	values, err := MapSpecToChartValues(manifest, "dev", resolved)
	require.NoError(t, err)
	api := mustNestedMap(t, values, "api")
	ports, ok := api["ports"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, ports, 2)
	sm := mustNestedMap(t, api, "serviceMonitor")
	assert.Equal(t, true, sm["enabled"])
	assert.Equal(t, spec.MetricsPortName, sm["port"])
	svc := mustNestedMap(t, api, "service")
	svcPorts, ok := svc["ports"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, svcPorts, 2)
}

func TestMapSpecToChartValues_WorkerExecHealth(t *testing.T) {
	t.Parallel()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"worker": {
				Role:  spec.ComponentRoleWorker,
				Image: "ghcr.io/acme/worker:1.0.0",
				Health: &spec.Health{
					Alive: &spec.HealthAlive{Exec: []string{"pgrep", "-f", "worker"}},
				},
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	values, err := MapSpecToChartValues(manifest, "dev", nil)
	require.NoError(t, err)
	worker := mustNestedMap(t, values, "worker")
	assert.Nil(t, worker["startupProbe"])
	assert.Nil(t, worker["readinessProbe"])
	liveness := mustNestedMap(t, worker, "livenessProbe")
	assert.Equal(t, true, liveness["enabled"])
	exec := mustNestedMap(t, liveness, "exec")
	assert.Equal(t, []any{"pgrep", "-f", "worker"}, exec["command"])
}

func TestApplyPortsAndService_ServiceMetricsDefaultsToAppPort(t *testing.T) {
	t.Parallel()
	vals := map[string]any{}
	err := applyPortsAndService(vals, spec.Component{
		Role:    spec.ComponentRoleService,
		Port:    8080,
		Metrics: &spec.ComponentMetrics{}, // port 0 -> use app port, no dedicated metrics port
	})
	require.NoError(t, err)
	ports, ok := vals["ports"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, ports, 1)
	assert.Equal(t, "http", ports[0]["name"])
}

func TestApplyPortsAndService_ServiceWithoutPortIsNoop(t *testing.T) {
	t.Parallel()
	vals := map[string]any{}
	require.NoError(t, applyPortsAndService(vals, spec.Component{
		Role: spec.ComponentRoleService,
		Port: 0,
	}))
	assert.Empty(t, vals)
}

func TestApplyShutdownTimeout_InvalidDuration(t *testing.T) {
	t.Parallel()
	err := applyShutdownTimeout(map[string]any{}, spec.Component{ShutdownTimeout: "nope"})
	require.Error(t, err)
}

func TestBuildProbeValues_WorkerAliveDisabled(t *testing.T) {
	t.Parallel()
	probes, err := buildProbeValues(spec.Component{
		Role:   spec.ComponentRoleWorker,
		Health: &spec.Health{Alive: &spec.HealthAlive{Disabled: true}},
	})
	require.NoError(t, err)
	assert.Empty(t, probes)
}

func TestBuildProbeValues_WorkerWithoutExec(t *testing.T) {
	t.Parallel()
	probes, err := buildProbeValues(spec.Component{Role: spec.ComponentRoleWorker})
	require.NoError(t, err)
	assert.Empty(t, probes)
}

func TestCopyMonitorProfile_RelabelingsMustBeSlice(t *testing.T) {
	t.Parallel()
	monitor := map[string]any{}
	err := copyMonitorProfile(monitor, &spec.PlatformProfile{
		Metrics: &spec.ProfileMetrics{
			Relabelings: []any{make(chan int)}, // not JSON-marshalable
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "relabelings")
}

func TestApplyMetricsValues_CopyProfileError(t *testing.T) {
	t.Parallel()
	err := applyMetricsValues(map[string]any{}, spec.Component{
		Role:    spec.ComponentRoleService,
		Port:    8080,
		Metrics: &spec.ComponentMetrics{Path: "/metrics"},
	}, &spec.PlatformProfile{
		Metrics: &spec.ProfileMetrics{
			MetricRelabelings: []any{make(chan int)},
		},
	})
	require.Error(t, err)
}
