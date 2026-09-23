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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	"deployah.dev/deployah/internal/target"
	"deployah.dev/deployah/internal/testing/nabatctx"

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

func (s *stubHelmClient) RenderManifests(context.Context, *spec.ResolvedSpec, postrenderer.PostRenderer, []extras.RawFile) (*render.RenderResult, func(), error) {
	if s.renderErr != nil {
		return nil, nil, s.renderErr
	}
	return s.renderResult, func() {}, nil
}

func (s *stubHelmClient) RenderOffline(context.Context, *spec.ResolvedSpec, postrenderer.PostRenderer, []extras.RawFile) (*render.RenderResult, func(), error) {
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

func (s *stubHelmClient) InstallApp(context.Context, bool, *spec.ResolvedSpec, postrenderer.PostRenderer, []extras.RawFile, bool) error {
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

// sessionWithStub builds a [session.Session] whose Helm client is stub.
func sessionWithStub(stub *stubHelmClient) *session.Session {
	return session.New(session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
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
	return &spec.Spec{Project: "web", APIVersion: spec.CurrentManifestVersion}
}

func testOptions() *Options {
	return &Options{Environment: "production", OutputFormat: outputFormatText}
}

func testResolved(m *spec.Spec) *spec.ResolvedSpec {
	if m == nil {
		m = testManifest()
	}
	return &spec.ResolvedSpec{Spec: m, Env: spec.NormalizeEnv("production")}
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
			h := nabatctx.New(t, "test")
			opts := testOptions()
			opts.DetailedExitCode = tt.detailed

			err := runOnline(h.Context, sess, nil, testManifest(), opts, testResolved(nil))
			if tt.wantErrIs != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErrIs)
				return
			}
			require.NoError(t, err)
			got := h.Stdout.String()
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

	h := nabatctx.New(t, "test")

	opts := testOptions()
	opts.Drift = true
	err := runOnline(h.Context, sess, nil, testManifest(), opts, testResolved(nil))
	require.NoError(t, err, "checkDrift must short-circuit cleanly without a working cluster config")

	assert.NotContains(t, h.Stdout.String(), "Drift (cluster changed outside deployah):",
		"a fresh install must not grow a stdout Drift section")
	assert.NotContains(t, h.Stdout.String(), "no-op on a fresh install",
		"the explanation must not appear in the captured diff body")
	assert.Contains(t, h.Stderr.String(), "no-op on a fresh install",
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
	h := nabatctx.New(t, "test")
	h.Context.SetContext(session.WithContext(h.Context.Context(), sess))

	opts := testOptions()
	opts.Offline = true
	err := runOffline(h.Context, sess, nil, testManifest(), opts, nil)

	require.NoError(t, err)
	assert.Contains(t, h.Stdout.String(), "Rendered 2 resources for environment 'production' (no cluster comparison).")
	assert.Contains(t, h.Stdout.String(), "validation: OK")
}

func writePlanExtras(t *testing.T, dir, relative, content string) {
	t.Helper()
	path := filepath.Join(dir, relative)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func writePlanCRDFile(t *testing.T, dir, name, body string) {
	t.Helper()
	writePlanExtras(t, dir, filepath.Join(".deployah", "crds", name), body)
}

func TestRunOffline_LoadExtrasError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "deployah.yaml")
	writePlanExtras(t, dir, "deployah.yaml", "apiVersion: deployah.dev/v1-alpha.4\nproject: web\n")
	writePlanExtras(t, dir, ".deployah/manifests/bad.yaml", "not: [valid")
	stub := &stubHelmClient{offlineResult: renderResult(deploymentV1)}
	sess := session.New(
		session.WithSpecPath(specPath),
		session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
			return stub, nil
		}),
	)
	h := nabatctx.New(t, "test")
	opts := testOptions()
	opts.Offline = true

	err := runOffline(h.Context, sess, nil, testManifest(), opts, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load extras")
}

const planCRDBody = "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"

func TestRunOffline_PrintsCRDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "deployah.yaml")
	writePlanExtras(t, dir, "deployah.yaml", "apiVersion: deployah.dev/v1-alpha.4\nproject: web\n")
	writePlanCRDFile(t, dir, "widget.yaml", planCRDBody)
	stub := &stubHelmClient{offlineResult: renderResult(deploymentV1)}
	sess := session.New(
		session.WithSpecPath(specPath),
		session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
			return stub, nil
		}),
	)
	h := nabatctx.New(t, "test")
	opts := testOptions()
	opts.Offline = true

	err := runOffline(h.Context, sess, nil, testManifest(), opts, nil)
	require.NoError(t, err)
	assert.Contains(t, h.Stdout.String(), "CustomResourceDefinition/widgets.example.com")
}

func TestRunOnline_PrintsCRDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "deployah.yaml")
	writePlanExtras(t, dir, "deployah.yaml", "apiVersion: deployah.dev/v1-alpha.4\nproject: web\n")
	writePlanCRDFile(t, dir, "widget.yaml", planCRDBody)
	stub := &stubHelmClient{
		historyErr:   helm.ErrReleaseNotFound,
		renderResult: renderResult(deploymentV1),
	}
	sess := session.New(
		session.WithSpecPath(specPath),
		session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
			return stub, nil
		}),
	)
	h := nabatctx.New(t, "test")
	h.Context.SetContext(session.WithContext(h.Context.Context(), sess))

	err := runOnline(h.Context, sess, nil, testManifest(), testOptions(), testResolved(nil))
	require.NoError(t, err)
	got := h.Stdout.String()
	assert.Contains(t, got, "+ CustomResourceDefinition/widgets.example.com")
	assert.Contains(t, got, "Helm install will process this chart CRD")
	assert.Contains(t, got, "name: widgets.example.com")
	assert.NotContains(t, got, "CRD files to process")
}

func TestRunOnline_PrintsCRDsOnUpgrade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		file    string
		history []*v1.Release
	}{
		{
			name:    "existing file",
			file:    "widget.yaml",
			history: []*v1.Release{releaseAt(3, common.StatusDeployed, deploymentV1)},
		},
		{
			name:    "newly added file",
			file:    "new.yaml",
			history: []*v1.Release{releaseAt(3, common.StatusDeployed, deploymentV1)},
		},
		{
			name:    "failed-only history",
			file:    "widget.yaml",
			history: []*v1.Release{releaseAt(1, common.StatusFailed, deploymentV1)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			specPath := filepath.Join(dir, "deployah.yaml")
			writePlanExtras(t, dir, "deployah.yaml", "apiVersion: deployah.dev/v1-alpha.4\nproject: web\n")
			writePlanCRDFile(t, dir, tc.file, planCRDBody)
			result := renderResult(deploymentV1)
			result.IsUpgrade = true
			stub := &stubHelmClient{
				history:      tc.history,
				renderResult: result,
			}
			sess := session.New(
				session.WithSpecPath(specPath),
				session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
					return stub, nil
				}),
			)
			h := nabatctx.New(t, "test")
			h.Context.SetContext(session.WithContext(h.Context.Context(), sess))

			err := runOnline(h.Context, sess, nil, testManifest(), testOptions(), testResolved(nil))
			require.NoError(t, err)
			got := h.Stdout.String()
			assert.Contains(t, got, "CustomResourceDefinition/widgets.example.com")
			assert.Contains(t, got, "Helm upgrade will not process this chart CRD")
			assert.NotContains(t, got, "+ CustomResourceDefinition/")
			assert.NotContains(t, got, "CRD files to process on install:")
		})
	}
}

func TestRunOnline_JSONStdoutUnmarshalsWithCRDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		upgrade     bool
		wantLife    string
		wantProcess bool
	}{
		{name: "fresh install", wantLife: "process", wantProcess: true},
		{name: "upgrade", upgrade: true, wantLife: "upgrade"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			specPath := filepath.Join(dir, "deployah.yaml")
			writePlanExtras(t, dir, "deployah.yaml", "apiVersion: deployah.dev/v1-alpha.4\nproject: web\n")
			writePlanCRDFile(t, dir, "widget.yaml", planCRDBody)
			result := renderResult(deploymentV1)
			stub := &stubHelmClient{renderResult: result}
			if tc.upgrade {
				result.IsUpgrade = true
				stub.history = []*v1.Release{releaseAt(3, common.StatusDeployed, deploymentV1)}
			} else {
				stub.historyErr = helm.ErrReleaseNotFound
			}
			sess := session.New(
				session.WithSpecPath(specPath),
				session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
					return stub, nil
				}),
			)
			h := nabatctx.New(t, "test")
			h.Context.SetContext(session.WithContext(h.Context.Context(), sess))
			opts := testOptions()
			opts.OutputFormat = outputFormatJSON

			err := runOnline(h.Context, sess, nil, testManifest(), opts, testResolved(nil))
			require.NoError(t, err)
			stdout := h.Stdout.String()
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "stdout must be a single JSON document")
			assert.NotContains(t, stdout, "Helm install will process this chart CRD")
			assert.NotContains(t, stdout, "Helm upgrade will not process this chart CRD")
			assert.NotContains(t, stdout, "CRD files to process")
			assert.NotContains(t, h.Stderr.String(), "Helm install will process this chart CRD")
			crds, ok := doc["chart_crds"].([]any)
			require.True(t, ok)
			require.Len(t, crds, 1)
			entry, ok := crds[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "CustomResourceDefinition", entry["kind"])
			assert.Equal(t, "widgets.example.com", entry["name"])
			assert.Equal(t, tc.wantLife, entry["lifecycle"])
			assert.Equal(t, tc.wantProcess, entry["will_process"])
			assert.NotContains(t, entry, "action")
			assert.NotContains(t, entry, "api_version")
		})
	}
}

func TestOutputPlan_JSONSkipCRDsStdoutUnmarshals(t *testing.T) {
	t.Parallel()
	p := &planengine.Plan{Header: planengine.Header{Project: "web", FreshInstall: true}}
	planengine.StampChartCRDs(p, []extras.CRDDoc{{
		Path: "widget.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
		YAML: []byte(planCRDBody),
	}}, false, true)
	h := nabatctx.New(t, "test")
	opts := testOptions()
	opts.OutputFormat = outputFormatJSON
	opts.DetailedExitCode = true

	err := outputPlan(h.Context, p, opts)
	require.NoError(t, err)
	stdout := h.Stdout.String()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "stdout must be a single JSON document")
	assert.NotContains(t, stdout, "install-time CRD processing disabled")
	assert.Empty(t, h.Stderr.String())
	crds, ok := doc["chart_crds"].([]any)
	require.True(t, ok)
	require.Len(t, crds, 1)
	entry, ok := crds[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "CustomResourceDefinition", entry["kind"])
	assert.Equal(t, "widgets.example.com", entry["name"])
	assert.Equal(t, "skip", entry["lifecycle"])
	assert.Equal(t, false, entry["will_process"])
	assert.NotContains(t, entry, "api_version")
}

func TestRunOnline_DetailedExitCode_ChartCRDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		upgrade   bool
		current   string
		previous  string
		wantErrIs error
	}{
		{name: "fresh install only crds", current: "", wantErrIs: planengine.ErrChangesPresent},
		{name: "upgrade only crds", upgrade: true, current: deploymentV1, previous: deploymentV1},
		{name: "upgrade with resource change", upgrade: true, current: deploymentV2, previous: deploymentV1, wantErrIs: planengine.ErrChangesPresent},
		{name: "fresh with resources and crds", current: deploymentV1, wantErrIs: planengine.ErrChangesPresent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			specPath := filepath.Join(dir, "deployah.yaml")
			writePlanExtras(t, dir, "deployah.yaml", "apiVersion: deployah.dev/v1-alpha.4\nproject: web\n")
			writePlanCRDFile(t, dir, "widget.yaml", planCRDBody)
			result := renderResult(tc.current)
			stub := &stubHelmClient{renderResult: result}
			if tc.upgrade {
				result.IsUpgrade = true
				stub.history = []*v1.Release{releaseAt(3, common.StatusDeployed, tc.previous)}
			} else {
				stub.historyErr = helm.ErrReleaseNotFound
			}
			sess := session.New(
				session.WithSpecPath(specPath),
				session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
					return stub, nil
				}),
			)
			h := nabatctx.New(t, "test")
			h.Context.SetContext(session.WithContext(h.Context.Context(), sess))
			opts := testOptions()
			opts.DetailedExitCode = true
			err := runOnline(h.Context, sess, nil, testManifest(), opts, testResolved(nil))
			if tc.wantErrIs != nil {
				require.ErrorIs(t, err, tc.wantErrIs)
				return
			}
			require.NoError(t, err)
		})
	}
}
