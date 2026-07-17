// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"
	"k8s.io/apimachinery/pkg/labels"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	planengine "deployah.dev/deployah/internal/plan"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// stubHelmClient implements [session.HelmClient] for plan command tests.
// Only the methods runOnline and runOffline actually call are wired; every
// other method panics if invoked unexpectedly, matching the pattern in
// internal/cmd/deploy/deploy_test.go.
type stubHelmClient struct {
	reachableErr error

	renderResult *render.RenderResult
	renderErr    error

	offlineResult *render.RenderResult
	offlineErr    error

	history    []*v1.Release
	historyErr error
}

func (s *stubHelmClient) IsReachable() error { return s.reachableErr }

func (s *stubHelmClient) RenderManifests(context.Context, *spec.Spec, string, *spec.ResolvedSpec) (*render.RenderResult, func(), error) {
	if s.renderErr != nil {
		return nil, nil, s.renderErr
	}
	return s.renderResult, func() {}, nil
}

func (s *stubHelmClient) RenderOffline(context.Context, *spec.Spec, string, *spec.ResolvedSpec) (*render.RenderResult, func(), error) {
	if s.offlineErr != nil {
		return nil, nil, s.offlineErr
	}
	return s.offlineResult, func() {}, nil
}

func (s *stubHelmClient) GetReleaseHistory(context.Context, string, string) ([]*v1.Release, error) {
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	return s.history, nil
}

func (s *stubHelmClient) InstallApp(context.Context, *spec.Spec, string, bool, *spec.ResolvedSpec) error {
	panic("unexpected InstallApp call")
}

func (s *stubHelmClient) DeleteRelease(context.Context, string, string, bool) error {
	panic("unexpected DeleteRelease call")
}

func (s *stubHelmClient) GetRelease(context.Context, string, string) (*v1.Release, error) {
	panic("unexpected GetRelease call")
}

func (s *stubHelmClient) ListReleases(context.Context, labels.Selector) ([]*v1.Release, error) {
	panic("unexpected ListReleases call")
}

func (s *stubHelmClient) RollbackRelease(context.Context, string, int, time.Duration) error {
	panic("unexpected RollbackRelease call")
}

var _ session.HelmClient = (*stubHelmClient)(nil)

// nabatContext returns a bare *nabat.Context and its captured stdout buffer,
// following the same pattern as internal/cmd/deploy/deploy_test.go's
// nabatContext helper.
func nabatContext(t *testing.T) (*nabat.Context, *bytes.Buffer) {
	t.Helper()
	io, _, out, _ := nabattest.NewIO()
	var captured *nabat.Context
	app := nabat.MustNew("test", nabat.WithIO(io))
	app.MustCommand("run", nabat.WithRun(func(c *nabat.Context) error {
		captured = c
		return nil
	}))
	require.NoError(t, nabattest.Run(t, app, []string{"run"}))
	return captured, out
}

// sessionWithStub builds a [session.Session] whose Helm client is stub.
func sessionWithStub(stub *stubHelmClient) *session.Session {
	return session.New(session.WithHelmFactory(func(*session.Session) (session.HelmClient, error) {
		return stub, nil
	}))
}

func releaseAt(version int, status common.Status, manifest string) *v1.Release {
	return &v1.Release{
		Name:     "web-production",
		Version:  version,
		Manifest: manifest,
		Info:     &v1.Info{Status: status},
	}
}

const deploymentV1 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.2
`

const deploymentV2 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.3
`

const configMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-config
  namespace: default
data:
  key: value
`

const secretV1 = `
apiVersion: v1
kind: Secret
metadata:
  name: web-secret
  namespace: default
data:
  password: b2xk
`

const secretV2 = `
apiVersion: v1
kind: Secret
metadata:
  name: web-secret
  namespace: default
data:
  password: bmV3
`

func testManifest() *spec.Spec {
	return &spec.Spec{Project: "web", APIVersion: "v1-alpha.2"}
}

func testOptions() *Options {
	return &Options{Environment: "production", OutputFormat: outputFormatText}
}

func renderResult(manifest string) *render.RenderResult {
	return &render.RenderResult{
		ReleaseName: "web-production",
		Namespace:   "default",
		Manifest:    manifest,
		Revision:    1,
	}
}

// TestRunOnline covers the common online plan paths: fresh install, image
// bump, add/remove, no-op, detailed-exitcode, secret masking, and failed-
// latest-revision warnings. Drift stream discipline and --offline keep
// their own tests because they need different IO / session wiring.
func TestRunOnline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stub        *stubHelmClient
		detailed    bool
		wantErrIs   error
		contains    []string
		notContains []string
	}{
		{
			name: "fresh install",
			stub: &stubHelmClient{
				historyErr:   helm.ErrReleaseNotFound,
				renderResult: renderResult(deploymentV1 + "---\n" + configMap),
			},
			contains: []string{
				"(fresh install)",
				"+ Deployment/web",
				"+ ConfigMap/web-config",
				"Plan: 2 to add, 0 to change, 0 to destroy.",
			},
		},
		{
			name: "image bump",
			stub: &stubHelmClient{
				history:      []*v1.Release{releaseAt(7, common.StatusDeployed, deploymentV1)},
				renderResult: renderResult(deploymentV2),
			},
			contains: []string{
				"(revision 7)",
				"~ Deployment/web",
				"myapp:v1.2 -> myapp:v1.3",
				"Plan: 0 to add, 1 to change, 0 to destroy.",
			},
		},
		{
			name: "resource added and removed",
			stub: &stubHelmClient{
				history:      []*v1.Release{releaseAt(3, common.StatusDeployed, deploymentV1+"---\n"+secretV1)},
				renderResult: renderResult(deploymentV1 + "---\n" + configMap),
			},
			contains: []string{
				"+ ConfigMap/web-config",
				"- Secret/web-secret",
				"Plan: 1 to add, 0 to change, 1 to destroy.",
			},
		},
		{
			name: "no changes with detailed-exitcode stays success",
			stub: &stubHelmClient{
				history:      []*v1.Release{releaseAt(4, common.StatusDeployed, deploymentV1)},
				renderResult: renderResult(deploymentV1),
			},
			detailed: true,
			contains: []string{"No changes."},
		},
		{
			name: "detailed-exitcode returns ErrChangesPresent",
			stub: &stubHelmClient{
				history:      []*v1.Release{releaseAt(1, common.StatusDeployed, deploymentV1)},
				renderResult: renderResult(deploymentV2),
			},
			detailed:  true,
			wantErrIs: planengine.ErrChangesPresent,
		},
		{
			name: "masked secret hides values",
			stub: &stubHelmClient{
				history:      []*v1.Release{releaseAt(2, common.StatusDeployed, secretV1)},
				renderResult: renderResult(secretV2),
			},
			contains:    []string{"(masked) changed"},
			notContains: []string{"b2xk", "bmV3"},
		},
		{
			name: "failed latest revision surfaces warning",
			stub: &stubHelmClient{
				history: []*v1.Release{
					releaseAt(1, common.StatusDeployed, deploymentV1),
					releaseAt(2, common.StatusFailed, deploymentV2),
				},
				renderResult: renderResult(deploymentV2),
			},
			contains: []string{"Warning:", "revision 2", "(revision 1)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess := sessionWithStub(tt.stub)
			c, out := nabatContext(t)
			opts := testOptions()
			opts.DetailedExitCode = tt.detailed

			err := runOnline(c, sess, testManifest(), opts, nil, theme.ResolvedTheme{})
			if tt.wantErrIs != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErrIs)
				return
			}
			require.NoError(t, err)
			got := out.String()
			for _, s := range tt.contains {
				assert.Contains(t, got, s)
			}
			for _, s := range tt.notContains {
				assert.NotContains(t, got, s)
			}
		})
	}
}

// TestRunOnline_DriftOnFreshInstall_NoStdoutFootprint verifies --drift on a
// fresh install prints its explanation to stderr (via checkDrift's
// c.Info), not stdout, and never touches the cluster's REST config.
func TestRunOnline_DriftOnFreshInstall_NoStdoutFootprint(t *testing.T) {
	t.Parallel()
	stub := &stubHelmClient{
		historyErr:   helm.ErrReleaseNotFound,
		renderResult: renderResult(deploymentV1),
	}
	sess := sessionWithStub(stub)

	io, _, out, errOut := nabattest.NewIO()
	var captured *nabat.Context
	app := nabat.MustNew("test", nabat.WithIO(io))
	app.MustCommand("run", nabat.WithRun(func(c *nabat.Context) error {
		captured = c
		return nil
	}))
	require.NoError(t, nabattest.Run(t, app, []string{"run"}))

	opts := testOptions()
	opts.Drift = true
	err := runOnline(captured, sess, testManifest(), opts, nil, theme.ResolvedTheme{})
	require.NoError(t, err, "checkDrift must short-circuit cleanly without a working cluster config")

	assert.NotContains(t, out.String(), "Drift (cluster changed outside deployah):",
		"a fresh install must not grow a stdout Drift section")
	assert.NotContains(t, out.String(), "no-op on a fresh install",
		"the explanation must not appear in the captured diff body")
	assert.Contains(t, errOut.String(), "no-op on a fresh install",
		"the explanation belongs on stderr, via c.Info")
}

// TestRunOffline_RendersResourceCount verifies --offline reports a resource
// count instead of a diff and never contacts release history.
func TestRunOffline_RendersResourceCount(t *testing.T) {
	t.Parallel()
	stub := &stubHelmClient{
		offlineResult: renderResult(deploymentV1 + "---\n" + configMap),
	}
	sess := sessionWithStub(stub)
	c, out := nabatContext(t)
	c.SetContext(session.WithContext(c.Context(), sess))

	opts := testOptions()
	opts.Offline = true
	err := runOffline(c, testManifest(), opts, nil)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Rendered 2 resources for environment 'production' (no cluster comparison).")
	assert.Contains(t, out.String(), "validation: OK")
}
