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
	"slices"

	"k8s.io/client-go/tools/clientcmd"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// loadingSnapshot is the client-go loading configuration captured at
// Resolve time. RESTConfig rebuilds rules from this snapshot so a later
// KUBECONFIG or HOME change cannot retarget an already-resolved Target.
type loadingSnapshot struct {
	explicitPath string
	precedence   []string
}

func snapshotLoading(cfg Config) loadingSnapshot {
	if cfg.KubeconfigPath != "" {
		return loadingSnapshot{explicitPath: cfg.KubeconfigPath}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	extra := slices.Clone(cfg.ExtraKubeconfigPaths)
	prec := make([]string, 0, len(extra)+len(rules.Precedence))
	prec = append(prec, extra...)
	prec = append(prec, rules.Precedence...)
	return loadingSnapshot{precedence: prec}
}

func (s loadingSnapshot) rules() *clientcmd.ClientConfigLoadingRules {
	rules := &clientcmd.ClientConfigLoadingRules{}
	if s.explicitPath != "" {
		rules.ExplicitPath = s.explicitPath
		return rules
	}
	rules.Precedence = slices.Clone(s.precedence)
	return rules
}

func loadKubeconfig(s loadingSnapshot) *clientcmdapi.Config {
	cfg, err := s.rules().Load()
	if err != nil {
		return nil
	}
	return cfg
}
