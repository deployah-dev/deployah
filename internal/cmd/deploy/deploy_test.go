package deploy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"
	"helm.sh/helm/v4/pkg/release/common"
	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/testing/nabatctx"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// stubHelmClient implements [session.HelmClient] for unit tests. Only
// GetRelease and RenderManifests are wired; every other method panics if
// called unexpectedly.
type stubHelmClient struct {
	release    *v1.Release
	releaseErr error

	// renderResults is consumed in order across RenderManifests calls (Nth
	// call can differ from the first); renderErr, if set, is returned on
	// every call instead.
	renderResults   []*render.RenderResult
	renderErr       error
	renderCallCount int

	installErr       error
	installCallCount int
	lastCRDs         []extras.RawFile
	lastSkipCRDs     bool
}

func (s *stubHelmClient) IsReachable() error { return nil }

func (s *stubHelmClient) InstallApp(_ context.Context, _ bool, _ *spec.ResolvedSpec, _ postrenderer.PostRenderer, crds []extras.RawFile, skipCRDs bool) error {
	s.installCallCount++
	s.lastCRDs = crds
	s.lastSkipCRDs = skipCRDs
	return s.installErr
}

func (s *stubHelmClient) RenderManifests(context.Context, *spec.ResolvedSpec, postrenderer.PostRenderer, []extras.RawFile) (*render.RenderResult, func(), error) {
	if s.renderErr != nil {
		return nil, func() {}, s.renderErr
	}
	i := s.renderCallCount
	s.renderCallCount++
	if i >= len(s.renderResults) {
		panic(fmt.Sprintf("unexpected RenderManifests call #%d: only %d result(s) configured", i+1, len(s.renderResults)))
	}
	return s.renderResults[i], func() {}, nil
}

func (s *stubHelmClient) RenderOffline(context.Context, *spec.ResolvedSpec, postrenderer.PostRenderer, []extras.RawFile) (*render.RenderResult, func(), error) {
	panic("unexpected RenderOffline call")
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

// GetReleaseHistory derives a single-entry history from release/releaseErr,
// defaulting Info to "deployed" when unset, so old GetRelease-based test
// fixtures still work against LastSuccessfulRelease's status check.
func (s *stubHelmClient) GetReleaseHistory(context.Context, string, string) ([]*v1.Release, error) {
	if s.releaseErr != nil {
		return nil, s.releaseErr
	}
	if s.release == nil {
		return nil, nil
	}
	rel := s.release
	if rel.Info == nil {
		relCopy := *rel
		relCopy.Info = &v1.Info{Status: common.StatusDeployed}
		rel = &relCopy
	}
	return []*v1.Release{rel}, nil
}

func (s *stubHelmClient) RollbackRelease(context.Context, string, int, time.Duration) error {
	panic("unexpected RollbackRelease call")
}

var _ session.HelmClient = (*stubHelmClient)(nil)

// releaseWithResolvedFQDN builds a prior release whose deployah.resolved
// block records component's FQDN. An empty component name yields an empty
// components map (no prior FQDN for any component).
func releaseWithResolvedFQDN(component, fqdn string) *v1.Release {
	components := map[string]any{}
	if component != "" {
		components[component] = map[string]any{"fqdn": fqdn}
	}
	return &v1.Release{
		Chart: &chart.Chart{
			Values: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{
						"components": components,
					},
				},
			},
		},
	}
}

// releaseWithIngressHostname builds a pre-deployah.resolved release that only
// has ingress.hostname (no longer consulted by the hostname guard).
func releaseWithIngressHostname(component, hostname string) *v1.Release {
	return &v1.Release{
		Chart: &chart.Chart{
			Values: map[string]any{
				component: map[string]any{
					"ingress": map[string]any{
						"hostname": hostname,
					},
				},
			},
		},
	}
}

// TestCheckHostnameGuard covers first-install, history errors, FQDN changes,
// --force-hostname-change.
func TestCheckHostnameGuard(t *testing.T) {
	t.Parallel()

	resolvedAPI := func(fqdn string) *spec.ResolvedSpec {
		return &spec.ResolvedSpec{
			Components: map[string]spec.ResolvedComponent{
				"api": {FQDN: fqdn},
			},
		}
	}

	tests := []struct {
		name        string
		stub        *stubHelmClient
		resolved    *spec.ResolvedSpec
		force       bool
		wantErr     bool
		errContains []string
		wantStderr  []string
	}{
		{
			name:     "first install empty history passes",
			stub:     &stubHelmClient{},
			resolved: resolvedAPI("api.example.com"),
		},
		{
			name:     "ErrReleaseNotFound passes like fresh install",
			stub:     &stubHelmClient{releaseErr: helm.ErrReleaseNotFound},
			resolved: resolvedAPI("api.example.com"),
		},
		{
			name:        "history error fails closed",
			stub:        &stubHelmClient{releaseErr: errors.New("connection refused")},
			resolved:    resolvedAPI("api.example.com"),
			wantErr:     true,
			errContains: []string{"hostname guard", "connection refused"},
		},
		{
			name:     "unchanged FQDN passes",
			stub:     &stubHelmClient{release: releaseWithResolvedFQDN("api", "api.example.com")},
			resolved: resolvedAPI("api.example.com"),
		},
		{
			name:        "changed FQDN blocks",
			stub:        &stubHelmClient{release: releaseWithResolvedFQDN("api", "api.old-domain.com")},
			resolved:    resolvedAPI("api.new-domain.com"),
			wantErr:     true,
			errContains: []string{"hostname change detected", "api.old-domain.com", "api.new-domain.com", "--force-hostname-change"},
		},
		{
			name:     "pre-resolved releases without deployah.resolved are not inspected",
			stub:     &stubHelmClient{release: releaseWithIngressHostname("api", "api.old-domain.com")},
			resolved: resolvedAPI("api.new-domain.com"),
		},
		{
			name:       "force warns instead of blocking",
			stub:       &stubHelmClient{release: releaseWithResolvedFQDN("api", "api.old-domain.com")},
			resolved:   resolvedAPI("api.new-domain.com"),
			force:      true,
			wantStderr: []string{"api.old-domain.com", "api.new-domain.com", "--force-hostname-change is set"},
		},
		{
			name:     "new component with no prior FQDN passes",
			stub:     &stubHelmClient{release: releaseWithResolvedFQDN("", "")},
			resolved: resolvedAPI("api.example.com"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := nabatctx.New(t, "test")

			err := checkHostnameGuard(h.Context, tt.stub, "shop", "production", tt.resolved, tt.force)
			if tt.wantErr {
				require.Error(t, err)
				for _, s := range tt.errContains {
					assert.Contains(t, err.Error(), s)
				}
				return
			}
			require.NoError(t, err)
			for _, s := range tt.wantStderr {
				assert.Contains(t, h.Stderr.String(), s)
			}
		})
	}
}
