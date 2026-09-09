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

package cmdopts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/session"
)

func TestWarnContextFallback_ExplicitContext(t *testing.T) {
	t.Parallel()

	c, _, errOut := nabatContextWithErr(t)
	cluster := mustTarget(t, session.New(session.WithKubeContext("cli-context")), "")
	WarnContextFallback(c, cluster, "production")
	assert.Empty(t, errOut.String())
}

func TestWarnContextFallback_PlatformContext(t *testing.T) {
	t.Parallel()

	platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	require.NoError(t, os.WriteFile(platformPath, []byte(`apiVersion: platform/v1-alpha.3
environments:
  production:
    context: platform-context
    domains:
      main:
        baseDomain: example.com
`), 0o600))

	c, _, errOut := nabatContextWithErr(t)
	cluster := mustTarget(t, session.New(session.WithPlatformFile(platformPath)), "production")
	WarnContextFallback(c, cluster, "production")
	assert.Empty(t, errOut.String())
}

func TestWarnContextFallback_KubeconfigCurrentContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(minimalKubeconfig), 0o600))

	c, _, errOut := nabatContextWithErr(t)
	cluster := mustTarget(t, session.New(session.WithKubeconfig(path)), "production")
	WarnContextFallback(c, cluster, "production")

	got := errOut.String()
	assert.Contains(t, got, `environment "production" has no context in the platform file and no --context was given`)
	assert.Contains(t, got, `the current kubeconfig context "test-context"`)
}

func TestWarnContextFallback_EmptyCurrentContext(t *testing.T) {
	t.Parallel()

	c, _, errOut := nabatContextWithErr(t)
	cluster := mustTarget(t, session.New(
		session.WithKubeconfig(filepath.Join(t.TempDir(), "missing-kubeconfig")),
	), "")
	WarnContextFallback(c, cluster, "")

	got := errOut.String()
	assert.Contains(t, got, "no context is configured (platform file or --context)")
	assert.Contains(t, got, "the kubeconfig's current context")
}

func mustTarget(t *testing.T, sess *session.Session, env string) *session.Cluster {
	t.Helper()
	cluster, err := sess.Target(t.Context(), env)
	require.NoError(t, err)
	return cluster
}

func nabatContextWithErr(t *testing.T) (*nabat.Context, *nabat.App, *bytes.Buffer) {
	t.Helper()
	io, _, _, errOut := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	return nabattest.Context(t, app), app, errOut
}

const minimalKubeconfig = `apiVersion: v1
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
