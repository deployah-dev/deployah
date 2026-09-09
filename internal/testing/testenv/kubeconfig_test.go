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

package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

func TestRunWithIsolatedKubeconfig_RedirectsClientGoDefaults(t *testing.T) {
	t.Parallel()

	home := os.Getenv("HOME")
	require.NotEmpty(t, home)
	assert.True(t, strings.HasPrefix(filepath.Base(home), "deployah-test-home-"))

	_, kubeconfigSet := os.LookupEnv("KUBECONFIG")
	assert.False(t, kubeconfigSet)

	wantHomeFile := filepath.Join(home, clientcmd.RecommendedHomeDir, clientcmd.RecommendedFileName)
	assert.Equal(t, wantHomeFile, clientcmd.RecommendedHomeFile)
	assert.Equal(t, filepath.Join(home, clientcmd.RecommendedHomeDir), clientcmd.RecommendedConfigDir)
	assert.Equal(t, filepath.Join(clientcmd.RecommendedConfigDir, clientcmd.RecommendedSchemaName), clientcmd.RecommendedSchemaFile)

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	assert.Equal(t, []string{clientcmd.RecommendedHomeFile}, rules.Precedence)
	assert.False(t, rules.WarnIfAllMissing)
	require.NotEmpty(t, rules.MigrationRules)
	for dest := range rules.MigrationRules {
		rel, err := filepath.Rel(home, dest)
		require.NoError(t, err)
		assert.False(t, strings.HasPrefix(rel, ".."))
	}
}
