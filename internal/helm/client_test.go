// Copyright 2026 The Deployah Authors
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

package helm

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kubefake "helm.sh/helm/v4/pkg/kube/fake"
)

func TestNewClient_RejectsInvalidStorageDriver(t *testing.T) {
	t.Parallel()

	c, err := NewClient(WithStorageDriver("sql"))
	require.Error(t, err)
	assert.Nil(t, c)
	assert.ErrorContains(t, err, "invalid storage driver 'sql'")
}

func TestNewClient_TimeoutAndNamespaceReachHelmActions(t *testing.T) {
	t.Parallel()

	c, err := NewClient(
		WithStorageDriver("memory"),
		WithTimeout(90*time.Second),
		WithNamespace("apps"),
	)
	require.NoError(t, err)
	install := c.newInstallAction("shop-production", nil, nil, false)
	upgrade := c.newUpgradeAction(nil, nil)
	assert.Equal(t, 90*time.Second, install.Timeout)
	assert.Equal(t, 90*time.Second, upgrade.Timeout)
	assert.Equal(t, "apps", install.Namespace)
	assert.Equal(t, "apps", upgrade.Namespace)

	defaults, err := NewClient(WithStorageDriver("memory"), WithNamespace(""))
	require.NoError(t, err)
	assert.Equal(t, "default", defaults.Namespace())
	assert.Equal(t, 5*time.Minute, defaults.newInstallAction("shop-production", nil, nil, false).Timeout)
}

func TestNamespace_EmptyAndNilUseDefault(t *testing.T) {
	t.Parallel()

	var c *Client
	assert.Equal(t, "default", c.Namespace())
	assert.Equal(t, "default", (&Client{}).Namespace())
	assert.Equal(t, "apps", (&Client{namespace: "apps"}).Namespace())
}

func TestIsReachable(t *testing.T) {
	t.Parallel()

	c, cfg, _, _ := memoryHelmApp(t, "reach-app")
	require.NoError(t, c.IsReachable())

	cause := errors.New("api server unreachable")
	failing := &kubefake.FailingKubeClient{ConnectionError: cause}
	failing.Out = io.Discard
	cfg.KubeClient = failing

	err := c.IsReachable()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrClusterUnreachable)
	assert.ErrorIs(t, err, cause)
}
