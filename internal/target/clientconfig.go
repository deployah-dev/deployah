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
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var _ clientcmd.ClientConfig = (*clientConfig)(nil)

// ClientConfig returns a lazy Kubernetes client configuration for this
// destination.
//
// The returned value does not read kubeconfig files until RawConfig or
// ClientConfig is called. Namespace returns the Target namespace without
// loading. When Context is non-empty, that context is pinned. Namespace is
// always pinned to [Target.Namespace]. Loading rules come from the Resolve
// snapshot. It does not use in-cluster configuration.
func (t *Target) ClientConfig() clientcmd.ClientConfig {
	if t == nil {
		return &clientConfig{nilTarget: true, namespace: defaultNamespace}
	}
	return &clientConfig{
		loading:   t.loading,
		context:   t.contextName,
		namespace: t.Namespace(),
	}
}

// clientConfig is a Target-owned [clientcmd.ClientConfig] that loads
// snapshotted rules on demand and pins context and namespace.
type clientConfig struct {
	loading   loadingSnapshot
	context   string
	namespace string
	nilTarget bool
}

func (c *clientConfig) overrides() *clientcmd.ConfigOverrides {
	ns := c.namespace
	if ns == "" {
		ns = defaultNamespace
	}
	overrides := &clientcmd.ConfigOverrides{
		Context: clientcmdapi.Context{Namespace: ns},
	}
	if c.context != "" {
		overrides.CurrentContext = c.context
	}
	return overrides
}

func (c *clientConfig) direct() (clientcmd.OverridingClientConfig, error) {
	if c == nil || c.nilTarget {
		return nil, fmt.Errorf("failed to build kubernetes config: target is nil")
	}
	rules := c.loading.clientConfigLoadingRules()
	raw, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to build kubernetes config: %w", err)
	}
	return clientcmd.NewNonInteractiveClientConfig(*raw, "", c.overrides(), rules), nil
}

func (c *clientConfig) RawConfig() (clientcmdapi.Config, error) {
	cc, err := c.direct()
	if err != nil {
		return clientcmdapi.Config{}, err
	}
	merged, err := cc.MergedRawConfig()
	if err != nil {
		return clientcmdapi.Config{}, fmt.Errorf("failed to build kubernetes config: %w", err)
	}
	return merged, nil
}

func (c *clientConfig) ClientConfig() (*rest.Config, error) {
	cc, err := c.direct()
	if err != nil {
		return nil, err
	}
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build kubernetes config: %w", err)
	}
	return cfg, nil
}

func (c *clientConfig) Namespace() (string, bool, error) {
	ns := defaultNamespace
	if c != nil && c.namespace != "" {
		ns = c.namespace
	}
	return ns, true, nil
}

func (c *clientConfig) ConfigAccess() clientcmd.ConfigAccess {
	if c == nil {
		return &clientcmd.ClientConfigLoadingRules{}
	}
	return c.loading.clientConfigLoadingRules()
}
