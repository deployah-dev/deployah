package deploy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// stubHelmClient implements [session.HelmClient] for unit tests. Only
// GetRelease is wired; all other methods panic if called unexpectedly.
type stubHelmClient struct {
	release    *v1.Release
	releaseErr error
}

func (s *stubHelmClient) IsReachable() error { return nil }

func (s *stubHelmClient) InstallApp(context.Context, *spec.Spec, string, bool, *spec.ResolvedSpec) error {
	panic("unexpected InstallApp call")
}

func (s *stubHelmClient) DeleteRelease(context.Context, string, string, bool) error {
	panic("unexpected DeleteRelease call")
}

func (s *stubHelmClient) GetRelease(_ context.Context, _, _ string) (*v1.Release, error) {
	if s.releaseErr != nil {
		return nil, s.releaseErr
	}
	return s.release, nil
}

func (s *stubHelmClient) ListReleases(context.Context, labels.Selector) ([]*v1.Release, error) {
	panic("unexpected ListReleases call")
}

func (s *stubHelmClient) GetReleaseHistory(context.Context, string, string) ([]*v1.Release, error) {
	panic("unexpected GetReleaseHistory call")
}

func (s *stubHelmClient) RollbackRelease(context.Context, string, int, time.Duration) error {
	panic("unexpected RollbackRelease call")
}

var _ session.HelmClient = (*stubHelmClient)(nil)

// nabatContext builds a minimal *nabat.Context for tests that call functions
// requiring it (e.g. checkHostnameGuard, which logs warnings).
func nabatContext(t *testing.T) *nabat.Context {
	t.Helper()
	var captured *nabat.Context
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	app.MustCommand("run", nabat.WithRun(func(c *nabat.Context) error {
		captured = c
		return nil
	}))
	require.NoError(t, nabattest.Run(t, app, []string{"run"}))
	return captured
}

// TestRequiredAPIs_CertManagerAdded includes cert-manager for certManager mode.
func TestRequiredAPIs_CertManagerAdded(t *testing.T) {
	m := &spec.Spec{
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("api")}},
		},
	}
	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.example.com", TLSMode: spec.TLSModeCertManager},
		},
	}
	reqs := requiredAPIs(m, "production", resolved)

	var hasCertManager bool
	for _, r := range reqs {
		for _, gv := range r.GroupVersions {
			if gv == "cert-manager.io/v1" {
				hasCertManager = true
			}
		}
	}
	assert.True(t, hasCertManager, "requiredAPIs must include cert-manager.io/v1 for certManager TLS mode")
}

// TestRequiredAPIs_NoCertManagerForSelfSigned omits cert-manager for selfSigned.
func TestRequiredAPIs_NoCertManagerForSelfSigned(t *testing.T) {
	m := &spec.Spec{
		Components: map[string]spec.Component{
			"api": {Expose: &spec.Expose{Domain: "public", Subdomain: new("api")}},
		},
	}
	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.local.nip.io", TLSMode: spec.TLSModeSelfSigned},
		},
	}
	reqs := requiredAPIs(m, "local", resolved)

	for _, r := range reqs {
		for _, gv := range r.GroupVersions {
			assert.NotEqual(t, "cert-manager.io/v1", gv,
				"selfSigned TLS must not require cert-manager")
		}
	}
}

// TestCheckHostnameGuard_FirstInstall_Passes passes when no prior release exists.
func TestCheckHostnameGuard_FirstInstall_Passes(t *testing.T) {
	c := nabatContext(t)
	helm := &stubHelmClient{releaseErr: errors.New("not found")}

	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.example.com"},
		},
	}

	err := checkHostnameGuard(c, helm, "shop", "production", resolved)
	assert.NoError(t, err, "first install (no prior release) must pass")
}

// TestCheckHostnameGuard_SameFQDN_Passes passes when the FQDN is unchanged.
func TestCheckHostnameGuard_SameFQDN_Passes(t *testing.T) {
	c := nabatContext(t)
	helm := &stubHelmClient{
		release: &v1.Release{
			Config: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{
						"components": map[string]any{
							"api": map[string]any{
								"fqdn": "api.example.com",
							},
						},
					},
				},
			},
		},
	}

	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.example.com"},
		},
	}

	err := checkHostnameGuard(c, helm, "shop", "production", resolved)
	assert.NoError(t, err, "unchanged FQDN must pass")
}

// TestCheckHostnameGuard_ChangedFQDN_Blocks blocks when a component FQDN changes.
func TestCheckHostnameGuard_ChangedFQDN_Blocks(t *testing.T) {
	c := nabatContext(t)
	helm := &stubHelmClient{
		release: &v1.Release{
			Config: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{
						"components": map[string]any{
							"api": map[string]any{
								"fqdn": "api.old-domain.com",
							},
						},
					},
				},
			},
		},
	}

	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.new-domain.com"},
		},
	}

	err := checkHostnameGuard(c, helm, "shop", "production", resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname change detected")
	assert.Contains(t, err.Error(), "api.old-domain.com")
	assert.Contains(t, err.Error(), "api.new-domain.com")
	assert.Contains(t, err.Error(), "--force-hostname-change")
}

// TestCheckHostnameGuard_LegacyFallback_Blocks blocks legacy ingress host changes.
func TestCheckHostnameGuard_LegacyFallback_Blocks(t *testing.T) {
	c := nabatContext(t)
	// Old release without deployah.resolved block; hostname stored in
	// component-level ingress.hostname.
	helm := &stubHelmClient{
		release: &v1.Release{
			Config: map[string]any{
				"api": map[string]any{
					"ingress": map[string]any{
						"hostname": "api.old-domain.com",
					},
				},
			},
		},
	}

	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.new-domain.com"},
		},
	}

	err := checkHostnameGuard(c, helm, "shop", "production", resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hostname change detected")
}

// TestCheckHostnameGuard_NewComponent_Passes passes when no prior FQDN exists.
func TestCheckHostnameGuard_NewComponent_Passes(t *testing.T) {
	c := nabatContext(t)
	helm := &stubHelmClient{
		release: &v1.Release{
			Config: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{
						"components": map[string]any{},
					},
				},
			},
		},
	}

	resolved := &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {FQDN: "api.example.com"},
		},
	}

	err := checkHostnameGuard(c, helm, "shop", "production", resolved)
	assert.NoError(t, err, "new component with no prior FQDN must pass")
}
