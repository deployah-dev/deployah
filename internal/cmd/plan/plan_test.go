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
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/view"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"
	"deployah.dev/deployah/internal/testing/nabatctx"

	planengine "deployah.dev/deployah/internal/plan"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// planKubeconfig resolves a REST config without contacting a cluster.
const planKubeconfig = `apiVersion: v1
kind: Config
current-context: test-context
clusters:
- name: test-cluster
  cluster:
    server: https://example.com:6443
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
users:
- name: test-user
  user:
    token: fake-token
`

// stubHelmClient implements [session.HelmClient] for plan command tests.
// RenderManifestsWithPrep is the only render path the command may call.
type stubHelmClient struct {
	reachableErr error

	result    *render.RenderResult
	prep      helm.ReleasePrep
	renderErr error

	calls           int
	gotCRDs         []extras.RawFile
	gotPostRenderer postrenderer.PostRenderer
}

func (s *stubHelmClient) IsReachable() error { return s.reachableErr }

func (s *stubHelmClient) RenderManifestsWithPrep(_ context.Context, _ *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.RawFile) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	s.calls++
	s.gotCRDs = crds
	s.gotPostRenderer = postRenderer
	if s.renderErr != nil {
		return nil, helm.ReleasePrep{}, nil, s.renderErr
	}
	return s.result, s.prep, func() {}, nil
}

func (s *stubHelmClient) RenderManifests(context.Context, *spec.ResolvedSpec, postrenderer.PostRenderer, []extras.RawFile) (*render.RenderResult, func(), error) {
	panic("unexpected RenderManifests call")
}

func (s *stubHelmClient) GetReleaseHistory(context.Context, string, string) ([]*v1.Release, error) {
	panic("unexpected GetReleaseHistory call")
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

type fakeRESTMapper struct{}

func (fakeRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}
	scope := meta.RESTScopeNamespace
	if gk.Kind == "Namespace" || gk.Kind == "CustomResourceDefinition" {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gk.Group, Version: version, Resource: strings.ToLower(gk.Kind) + "s"},
		GroupVersionKind: gk.WithVersion(version),
		Scope:            scope,
	}, nil
}

type fakeLive struct {
	gets  int
	lists int
	objs  []*unstructured.Unstructured
}

func (f *fakeLive) Get(_ context.Context, mapping *meta.RESTMapping, _, name string) (*unstructured.Unstructured, error) {
	f.gets++
	for _, obj := range f.objs {
		if obj.GetName() == name && obj.GetKind() == mapping.GroupVersionKind.Kind {
			return obj.DeepCopy(), nil
		}
	}
	gr := schema.GroupResource{Group: mapping.GroupVersionKind.Group, Resource: mapping.Resource.Resource}
	return nil, apierrors.NewNotFound(gr, name)
}

func (f *fakeLive) List(context.Context, *meta.RESTMapping, string, labels.Selector) ([]unstructured.Unstructured, error) {
	f.lists++
	return nil, nil
}

var _ planengine.LiveReader = (*fakeLive)(nil)

type recordedReaders struct {
	host  string
	calls int
	err   error
	live  planengine.LiveReader
}

func (r *recordedReaders) build(cfg *rest.Config) (planengine.RESTMapper, planengine.LiveReader, error) {
	r.calls++
	if cfg != nil {
		r.host = cfg.Host
	}
	if r.err != nil {
		return nil, nil, r.err
	}
	live := r.live
	if live == nil {
		live = &fakeLive{}
	}
	return fakeRESTMapper{}, live, nil
}

func testResolved(m *spec.Spec) *spec.ResolvedSpec {
	if m == nil {
		m = &spec.Spec{Project: "web", APIVersion: spec.CurrentManifestVersion}
	}
	return &spec.ResolvedSpec{Spec: m, Env: spec.NormalizeEnv("production")}
}

func installResult(manifest string) *render.RenderResult {
	return &render.RenderResult{
		ReleaseName: "web-production",
		Namespace:   "default",
		Manifest:    manifest,
		Revision:    1,
	}
}

func upgradeResult(manifest string, revision int) *render.RenderResult {
	result := installResult(manifest)
	result.IsUpgrade = true
	result.Revision = revision
	return result
}

func installPrep() helm.ReleasePrep {
	return helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1}
}

func upgradePrep(manifest string, version int) helm.ReleasePrep {
	rel := &v1.Release{
		Name:      "web-production",
		Namespace: "default",
		Version:   version,
		Manifest:  manifest,
	}
	return helm.ReleasePrep{
		Operation:    helm.OperationUpgrade,
		Current:      rel,
		Newest:       rel,
		NextRevision: version + 1,
	}
}

func mustObject(t *testing.T, manifest string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{}
	require.NoError(t, yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096).Decode(obj))
	return obj
}

func writeSpecDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte("apiVersion: deployah.dev/v1-alpha.4\nproject: web\n"), 0o600))
	return dir
}

func writePlanExtras(t *testing.T, dir, relative, content string) {
	t.Helper()
	path := filepath.Join(dir, relative)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

const planCRDBody = "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"

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

const deploymentLive = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 9
  template:
    spec:
      containers:
        - name: web
          image: myapp:v1.2
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

func migrateHook() *v1.Hook {
	return &v1.Hook{
		Name:   "migrate",
		Kind:   "Job",
		Weight: 1,
		Events: []v1.HookEvent{v1.HookPreInstall},
		Manifest: "apiVersion: batch/v1\nkind: Job\nmetadata:\n  name: migrate\n  namespace: default\n  labels:\n    " +
			spec.LabelTask + ": migrate\n    " + spec.LabelComponent + ": migrate\nspec:\n  template:\n    spec:\n      containers:\n      - name: job\n        image: busybox\n",
	}
}

func taskResolved() *spec.ResolvedSpec {
	resolved := testResolved(nil)
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	return resolved
}

type planFixture struct {
	dir        string
	stub       *stubHelmClient
	opts       *Options
	resolved   *spec.ResolvedSpec
	live       *fakeLive
	readers    *recordedReaders
	kubeconfig string
}

func (f planFixture) run(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	if f.dir == "" {
		f.dir = writeSpecDir(t)
	}
	if f.opts == nil {
		f.opts = &Options{Environment: "production", OutputFormat: outputFormatHuman}
	}
	if f.resolved == nil {
		f.resolved = testResolved(nil)
	}
	if f.readers == nil {
		f.readers = &recordedReaders{live: f.live}
	} else if f.readers.live == nil {
		f.readers.live = f.live
	}
	kube := f.kubeconfig
	if kube == "" {
		kube = filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, os.WriteFile(kube, []byte(planKubeconfig), 0o600))
	}
	sess := session.New(
		session.WithSpecPath(filepath.Join(f.dir, "deployah.yaml")),
		session.WithKubeconfig(kube),
		session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
			return f.stub, nil
		}),
	)
	h := nabatctx.New(t, "test")
	err = executePlan(h.Context, sess, nil, &spec.Spec{Project: "web", APIVersion: spec.CurrentManifestVersion}, f.opts, f.resolved, f.readers.build)
	return h.Stdout.String(), h.Stderr.String(), err
}

func TestExecutePlan_Human(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		stub        *stubHelmClient
		live        *fakeLive
		contains    []string
		notContains []string
	}{
		{
			name: "fresh install",
			stub: &stubHelmClient{
				result: installResult(deploymentV1 + "---\n" + configMap),
				prep:   installPrep(),
			},
			contains: []string{
				`+ create apps/v1/Deployment "web"`,
				`+ create v1/ConfigMap "web-config"`,
				"Resources: 2 create, 0 update, 0 delete",
			},
		},
		{
			name: "image bump",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV2, 8),
				prep:   upgradePrep(deploymentV1, 7),
			},
			live:     &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			contains: []string{`~ update apps/v1/Deployment "web"`, "myapp:v1.2", "myapp:v1.3"},
		},
		{
			name: "resource added and removed",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV1+"---\n"+configMap, 4),
				prep:   upgradePrep(deploymentV1+"---\n"+secretV1, 3),
			},
			live: &fakeLive{objs: []*unstructured.Unstructured{
				mustObject(t, deploymentV1),
				mustObject(t, secretV1),
			}},
			contains: []string{
				`+ create v1/ConfigMap "web-config"`,
				`- delete v1/Secret "web-secret"`,
			},
		},
		{
			name: "no changes",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV1, 5),
				prep:   upgradePrep(deploymentV1, 4),
			},
			live:     &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			contains: []string{"Resources: 0 create, 0 update, 0 delete"},
		},
		{
			name: "masked secret hides values",
			stub: &stubHelmClient{
				result: installResult(secretV1),
				prep:   installPrep(),
			},
			contains:    []string{"(redacted)"},
			notContains: []string{"b2xk"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := (planFixture{stub: tt.stub, live: tt.live}).run(t)
			require.NoError(t, err)
			assert.NotContains(t, stdout, "\x1b")
			for _, s := range tt.contains {
				assert.Contains(t, stdout, s)
			}
			for _, s := range tt.notContains {
				assert.NotContains(t, stdout, s)
			}
		})
	}
}

func TestExecutePlan_Tasks(t *testing.T) {
	t.Parallel()
	result := installResult("")
	result.Hooks = []*v1.Hook{migrateHook()}
	stdout, _, err := (planFixture{
		stub:     &stubHelmClient{result: result, prep: installPrep()},
		resolved: taskResolved(),
	}).run(t)
	require.NoError(t, err)
	assert.NotContains(t, stdout, "\x1b")
	assert.Contains(t, stdout, "Tasks")
	assert.Contains(t, stdout, "preDeploy")
}

func TestExecutePlan_FreshInstallDoesNotReadLive(t *testing.T) {
	t.Parallel()
	live := &fakeLive{}
	stdout, _, err := (planFixture{
		stub: &stubHelmClient{
			result: installResult(deploymentV1),
			prep:   installPrep(),
		},
		live: live,
		opts: &Options{Environment: "production", OutputFormat: outputFormatJSON},
	}).run(t)
	require.NoError(t, err)
	assert.Zero(t, live.gets)
	assert.Zero(t, live.lists)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Empty(t, doc["drift"])
}

func TestExecutePlan_DriftOnExistingRelease(t *testing.T) {
	t.Parallel()
	live := &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentLive)}}
	stdout, _, err := (planFixture{
		stub: &stubHelmClient{
			result: upgradeResult(deploymentV2, 8),
			prep:   upgradePrep(deploymentV1, 7),
		},
		live: live,
	}).run(t)
	require.NoError(t, err)
	assert.Positive(t, live.gets)
	assert.Contains(t, stdout, "Drift")
	assert.Contains(t, stdout, "Drift: ")
}

func TestExecutePlan_DriftOnExistingRelease_JSON(t *testing.T) {
	t.Parallel()
	live := &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentLive)}}
	stdout, _, err := (planFixture{
		stub: &stubHelmClient{
			result: upgradeResult(deploymentV2, 8),
			prep:   upgradePrep(deploymentV1, 7),
		},
		live: live,
		opts: &Options{Environment: "production", OutputFormat: outputFormatJSON},
	}).run(t)
	require.NoError(t, err)
	assert.Positive(t, live.gets)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	drift, ok := doc["drift"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, drift)
}

func TestExecutePlan_DriftOnly(t *testing.T) {
	t.Parallel()
	stdout, _, err := (planFixture{
		stub: &stubHelmClient{
			result: upgradeResult(deploymentV1, 5),
			prep:   upgradePrep(deploymentV1, 4),
		},
		live: &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentLive)}},
	}).run(t)
	require.NoError(t, err)
	assert.Contains(t, stdout, "Drift")
}

func TestExecutePlan_DriftOnly_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, err := (planFixture{
		stub: &stubHelmClient{
			result: upgradeResult(deploymentV1, 5),
			prep:   upgradePrep(deploymentV1, 4),
		},
		live: &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentLive)}},
		opts: &Options{Environment: "production", OutputFormat: outputFormatJSON, DetailedExitCode: true},
	}).run(t)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	assert.Equal(t, "none", doc["helmAction"])
	assert.Empty(t, doc["changes"])
	drift, ok := doc["drift"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, drift)
}

func TestExecutePlan_JSONIsWriterBytes(t *testing.T) {
	t.Parallel()
	stub := &stubHelmClient{
		result: installResult(deploymentV1),
		prep:   installPrep(),
	}
	live := &fakeLive{}
	dir := writeSpecDir(t)
	readers := &recordedReaders{live: live}
	stdout, _, err := (planFixture{
		dir:     dir,
		stub:    stub,
		live:    live,
		readers: readers,
		opts:    &Options{Environment: "production", OutputFormat: outputFormatJSON},
	}).run(t)
	require.NoError(t, err)
	assert.NotContains(t, stdout, "\x1b")
	assert.Equal(t, "https://example.com:6443", readers.host)

	wantPlan, _, cleanup, buildErr := planengine.BuildSemanticPlan(t.Context(), stub, fakeRESTMapper{}, live, planengine.SemanticBuildInput{
		ClusterContext: "test-context",
		Resolved:       testResolved(nil),
		SkipCRDs:       false,
	})
	t.Cleanup(cleanup)
	require.NoError(t, buildErr)
	var want bytes.Buffer
	require.NoError(t, view.WriteJSON(&want, wantPlan, view.Options{}))
	assert.Equal(t, want.String(), stdout)

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc))
	var extra any
	assert.ErrorIs(t, dec.Decode(&extra), io.EOF)
	for _, key := range []string{"schema", "header", "helmAction", "changes", "drift", "tasks", "chartCRDs", "summary"} {
		assert.Contains(t, doc, key)
	}
	assert.NotContains(t, doc, "chart_crds")
	assert.NotContains(t, doc, "first_install_note")
}

func TestExecutePlan_ChartCRDsHuman(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		result   *render.RenderResult
		prep     helm.ReleasePrep
		live     *fakeLive
		contains string
	}{
		{
			name:     "fresh install",
			result:   installResult(deploymentV1),
			prep:     installPrep(),
			contains: "lifecycle: process (Helm install will process this chart CRD)",
		},
		{
			name:     "upgrade",
			result:   upgradeResult(deploymentV2, 4),
			prep:     upgradePrep(deploymentV1, 3),
			live:     &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			contains: "lifecycle: upgrade (Helm upgrade does not process chart CRDs)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeSpecDir(t)
			writePlanExtras(t, dir, ".deployah/crds/widget.yaml", planCRDBody)
			stdout, _, err := (planFixture{
				dir:  dir,
				stub: &stubHelmClient{result: tc.result, prep: tc.prep},
				live: tc.live,
			}).run(t)
			require.NoError(t, err)
			assert.Contains(t, stdout, tc.contains)
			resources := strings.Index(stdout, "Resources")
			crds := strings.Index(stdout, "Chart CRDs")
			summary := strings.Index(stdout, "Summary")
			assert.GreaterOrEqual(t, resources, 0)
			assert.Greater(t, crds, resources)
			assert.Greater(t, summary, crds)
			assert.NotContains(t, stdout, "+ CustomResourceDefinition")
		})
	}
}

func TestExecutePlan_ChartCRDsJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		result      *render.RenderResult
		prep        helm.ReleasePrep
		live        *fakeLive
		wantLife    string
		wantProcess bool
	}{
		{
			name:        "fresh install",
			result:      installResult(deploymentV1),
			prep:        installPrep(),
			wantLife:    "process",
			wantProcess: true,
		},
		{
			name:     "upgrade",
			result:   upgradeResult(deploymentV1, 4),
			prep:     upgradePrep(deploymentV1, 3),
			live:     &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			wantLife: "upgrade",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeSpecDir(t)
			writePlanExtras(t, dir, ".deployah/crds/widget.yaml", planCRDBody)
			stdout, _, err := (planFixture{
				dir:  dir,
				stub: &stubHelmClient{result: tc.result, prep: tc.prep},
				live: tc.live,
				opts: &Options{Environment: "production", OutputFormat: outputFormatJSON},
			}).run(t)
			require.NoError(t, err)
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
			crds, ok := doc["chartCRDs"].([]any)
			require.True(t, ok)
			require.Len(t, crds, 1)
			entry, ok := crds[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "CustomResourceDefinition", entry["kind"])
			assert.Equal(t, "widgets.example.com", entry["name"])
			assert.Equal(t, tc.wantLife, entry["lifecycle"])
			assert.Equal(t, tc.wantProcess, entry["willProcess"])
		})
	}
}

func TestExecutePlan_ForwardsCRDsAndPostRendererOnce(t *testing.T) {
	t.Parallel()
	dir := writeSpecDir(t)
	writePlanExtras(t, dir, ".deployah/crds/widget.yaml", planCRDBody)
	writePlanExtras(t, dir, ".deployah/manifests/extra.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: extra\ndata:\n  key: value\n")
	// A closed port makes scope discovery fail fast and fall back to the
	// built-in table. example.com:6443 would wait out a dial timeout.
	kube := filepath.Join(t.TempDir(), "kubeconfig")
	closed := strings.Replace(planKubeconfig, "https://example.com:6443", "https://127.0.0.1:1", 1)
	require.NoError(t, os.WriteFile(kube, []byte(closed), 0o600))
	stub := &stubHelmClient{result: installResult(deploymentV1), prep: installPrep()}
	_, _, err := (planFixture{dir: dir, stub: stub, kubeconfig: kube}).run(t)
	require.NoError(t, err)
	assert.Equal(t, 1, stub.calls)
	require.Len(t, stub.gotCRDs, 1)
	assert.NotNil(t, stub.gotPostRenderer)
}

func TestExecutePlan_ShowSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		format      string
		reveal      bool
		contains    []string
		notContains []string
	}{
		{
			name:        "human default",
			format:      outputFormatHuman,
			contains:    []string{"(redacted)"},
			notContains: []string{"b2xk"},
		},
		{
			name:     "human reveal",
			format:   outputFormatHuman,
			reveal:   true,
			contains: []string{"b2xk"},
		},
		{
			name:        "json default",
			format:      outputFormatJSON,
			contains:    []string{"(redacted)"},
			notContains: []string{"b2xk", "\x1b"},
		},
		{
			name:        "json reveal",
			format:      outputFormatJSON,
			reveal:      true,
			contains:    []string{"b2xk"},
			notContains: []string{"\x1b"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, err := (planFixture{
				stub: &stubHelmClient{result: installResult(secretV1), prep: installPrep()},
				opts: &Options{Environment: "production", OutputFormat: tc.format, ShowSecrets: tc.reveal},
			}).run(t)
			require.NoError(t, err)
			if tc.format == outputFormatJSON {
				require.NoError(t, json.Unmarshal([]byte(stdout), &map[string]any{}))
			}
			for _, s := range tc.contains {
				assert.Contains(t, stdout, s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, stdout, s)
			}
		})
	}

	hidden, _, err := (planFixture{
		stub: &stubHelmClient{result: installResult(secretV1), prep: installPrep()},
		opts: &Options{Environment: "production", OutputFormat: outputFormatJSON},
	}).run(t)
	require.NoError(t, err)
	shown, _, err := (planFixture{
		stub: &stubHelmClient{result: installResult(secretV1), prep: installPrep()},
		opts: &Options{Environment: "production", OutputFormat: outputFormatJSON, ShowSecrets: true},
	}).run(t)
	require.NoError(t, err)
	assert.JSONEq(t, hidden, strings.ReplaceAll(shown, "b2xk", "(redacted)"))
}

func TestShowSecrets_PresentationOnly(t *testing.T) {
	t.Parallel()
	stub := &stubHelmClient{result: installResult(secretV1), prep: installPrep()}
	p, _, cleanup, err := planengine.BuildSemanticPlan(t.Context(), stub, fakeRESTMapper{}, nil, planengine.SemanticBuildInput{
		ClusterContext: "test-context",
		Resolved:       testResolved(nil),
	})
	t.Cleanup(cleanup)
	require.NoError(t, err)
	before, err := json.Marshal(p)
	require.NoError(t, err)

	var hidden, shown bytes.Buffer
	require.NoError(t, view.WriteHuman(&hidden, p, view.Options{}))
	require.NoError(t, view.WriteHuman(&shown, p, view.Options{ShowSecrets: true}))
	require.NoError(t, view.WriteJSON(&hidden, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&shown, p, view.Options{ShowSecrets: true}))

	after, err := json.Marshal(p)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.NotContains(t, hidden.String(), "b2xk")
	assert.Contains(t, shown.String(), "b2xk")
}

func TestExecutePlan_DetailedExitCode(t *testing.T) {
	t.Parallel()
	taskResult := installResult("")
	taskResult.Hooks = []*v1.Hook{migrateHook()}
	tests := []struct {
		name      string
		stub      *stubHelmClient
		live      *fakeLive
		resolved  *spec.ResolvedSpec
		dirCRD    bool
		format    string
		wantErr   error
		wantPlain string
		contains  []string
		off       bool
	}{
		{
			name: "resource change",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV2, 8),
				prep:   upgradePrep(deploymentV1, 7),
			},
			live:     &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			wantErr:  ErrChangesPresent,
			contains: []string{`~ update apps/v1/Deployment "web"`, "Summary"},
		},
		{
			name:     "task only",
			stub:     &stubHelmClient{result: taskResult, prep: installPrep()},
			resolved: taskResolved(),
			wantErr:  ErrChangesPresent,
		},
		{
			name:    "chart crd process",
			stub:    &stubHelmClient{result: installResult(""), prep: installPrep()},
			dirCRD:  true,
			wantErr: ErrChangesPresent,
		},
		{
			name: "no changes",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV1, 5),
				prep:   upgradePrep(deploymentV1, 4),
			},
			live: &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
		},
		{
			name: "drift only",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV1, 5),
				prep:   upgradePrep(deploymentV1, 4),
			},
			live: &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentLive)}},
		},
		{
			name: "chart crd upgrade",
			stub: &stubHelmClient{
				result: upgradeResult("", 2),
				prep:   upgradePrep("", 1),
			},
			dirCRD: true,
			live:   &fakeLive{},
		},
		{
			name: "render error",
			stub: &stubHelmClient{
				result:    installResult(deploymentV1),
				prep:      installPrep(),
				renderErr: errors.New("render broke"),
			},
			wantPlain: "render manifests",
		},
		{
			name:   "json resource change",
			format: outputFormatJSON,
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV2, 8),
				prep:   upgradePrep(deploymentV1, 7),
			},
			live:    &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			wantErr: ErrChangesPresent,
		},
		{
			name: "flag off ignores effects",
			stub: &stubHelmClient{
				result: upgradeResult(deploymentV2, 8),
				prep:   upgradePrep(deploymentV1, 7),
			},
			live: &fakeLive{objs: []*unstructured.Unstructured{mustObject(t, deploymentV1)}},
			off:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := ""
			if tc.dirCRD {
				dir = writeSpecDir(t)
				writePlanExtras(t, dir, ".deployah/crds/widget.yaml", planCRDBody)
			}
			format := tc.format
			if format == "" {
				format = outputFormatHuman
			}
			stdout, _, err := (planFixture{
				dir:      dir,
				stub:     tc.stub,
				live:     tc.live,
				resolved: tc.resolved,
				opts:     &Options{Environment: "production", OutputFormat: format, DetailedExitCode: !tc.off},
			}).run(t)
			if tc.wantPlain != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tc.wantPlain)
				assert.NotErrorIs(t, err, ErrChangesPresent)
				return
			}
			assert.NotEmpty(t, stdout)
			for _, s := range tc.contains {
				assert.Contains(t, stdout, s)
			}
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if format == outputFormatJSON {
				require.NoError(t, json.Unmarshal([]byte(stdout), &map[string]any{}))
			}
		})
	}
}

func TestExecutePlan_ReaderError(t *testing.T) {
	t.Parallel()
	readers := &recordedReaders{err: errors.New("mapper down")}
	_, _, err := (planFixture{
		stub:    &stubHelmClient{result: installResult(deploymentV1), prep: installPrep()},
		readers: readers,
	}).run(t)
	require.Error(t, err)
	assert.ErrorContains(t, err, "cluster readers")
	assert.ErrorContains(t, err, "mapper down")
	assert.NotErrorIs(t, err, ErrChangesPresent)
}

func TestExecutePlan_MissingKubeconfig(t *testing.T) {
	t.Parallel()
	_, _, err := (planFixture{
		stub:       &stubHelmClient{result: installResult(deploymentV1), prep: installPrep()},
		kubeconfig: filepath.Join(t.TempDir(), "missing"),
	}).run(t)
	require.Error(t, err)
	assert.ErrorContains(t, err, "kubernetes config")
}

func TestExecutePlan_LoadExtrasError(t *testing.T) {
	t.Parallel()
	dir := writeSpecDir(t)
	writePlanExtras(t, dir, ".deployah/manifests/bad.yaml", "not: [valid")
	_, _, err := (planFixture{
		dir:  dir,
		stub: &stubHelmClient{result: installResult(deploymentV1), prep: installPrep()},
	}).run(t)
	require.Error(t, err)
	assert.ErrorContains(t, err, "load extras")
}
