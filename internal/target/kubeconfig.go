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

package target

import (
	"maps"
	"slices"

	"k8s.io/client-go/tools/clientcmd"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// loadingSnapshot is the client-go loading configuration captured at
// Resolve time. RESTConfig rebuilds rules from this snapshot so a later
// KUBECONFIG or HOME change cannot retarget an already-resolved Target.
type loadingSnapshot struct {
	rules clientcmd.ClientConfigLoadingRules
}

func snapshotLoading(cfg Config) loadingSnapshot {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if cfg.KubeconfigPath != "" {
		rules.ExplicitPath = cfg.KubeconfigPath
	} else if len(cfg.ExtraKubeconfigPaths) > 0 {
		rules.Precedence = append(slices.Clone(cfg.ExtraKubeconfigPaths), rules.Precedence...)
	}
	return loadingSnapshot{rules: cloneLoadingRules(rules)}
}

func (s loadingSnapshot) clientConfigLoadingRules() *clientcmd.ClientConfigLoadingRules {
	cloned := cloneLoadingRules(&s.rules)
	return &cloned
}

func cloneLoadingRules(rules *clientcmd.ClientConfigLoadingRules) clientcmd.ClientConfigLoadingRules {
	cloned := *rules
	cloned.Precedence = slices.Clone(rules.Precedence)
	cloned.MigrationRules = maps.Clone(rules.MigrationRules)
	return cloned
}

func loadKubeconfig(s loadingSnapshot) *clientcmdapi.Config {
	cfg, err := s.clientConfigLoadingRules().Load()
	if err != nil {
		return nil
	}
	return cfg
}
