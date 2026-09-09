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

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Config holds Kubernetes destination inputs for a [Resolver].
// It describes kubeconfig and override selection only.
type Config struct {
	// KubeconfigPath is an explicit kubeconfig file. When set it takes
	// full precedence over extra paths and KUBECONFIG.
	KubeconfigPath string
	// ContextOverride is an explicit context name, typically --context.
	ContextOverride string
	// NamespaceOverride is an explicit namespace, typically --namespace.
	NamespaceOverride string
	// ExtraKubeconfigPaths are prepended to default kubeconfig precedence
	// when KubeconfigPath is empty. Missing files are tolerated.
	ExtraKubeconfigPaths []string
}

// Resolver owns immutable destination-resolution configuration.
type Resolver struct {
	config Config
}

// NewResolver returns a Resolver that copies config so later mutation of
// ExtraKubeconfigPaths cannot change resolution.
func NewResolver(config Config) *Resolver {
	return &Resolver{
		config: Config{
			KubeconfigPath:       config.KubeconfigPath,
			ContextOverride:      config.ContextOverride,
			NamespaceOverride:    config.NamespaceOverride,
			ExtraKubeconfigPaths: slices.Clone(config.ExtraKubeconfigPaths),
		},
	}
}

// Resolve returns an immutable Target for platformContext.
//
// platformContext is a Kubernetes context name already chosen by the
// caller. Resolve does not load Deployah platform or spec files.
// Kubeconfig read failures do not fail Resolve; [Target.RESTConfig]
// reports them when a connection config is required.
func (r *Resolver) Resolve(platformContext string) *Target {
	loading := snapshotLoading(r.config)
	raw := loadKubeconfig(loading)

	contextName, source := selectContext(r.config.ContextOverride, platformContext, raw)
	namespace := r.config.NamespaceOverride
	if namespace == "" {
		namespace = namespaceFromKubeconfig(raw, contextName)
	}

	return &Target{
		contextName:   contextName,
		contextSource: source,
		namespace:     namespace,
		loading:       loading,
	}
}

func selectContext(override, platformContext string, raw *clientcmdapi.Config) (string, ContextSource) {
	switch {
	case override != "":
		return override, ContextSourceExplicit
	case platformContext != "":
		return platformContext, ContextSourcePlatform
	default:
		if raw != nil {
			return raw.CurrentContext, ContextSourceKubeconfig
		}
		return "", ContextSourceKubeconfig
	}
}

func namespaceFromKubeconfig(raw *clientcmdapi.Config, contextName string) string {
	if raw == nil || contextName == "" {
		return defaultNamespace
	}
	ctx, ok := raw.Contexts[contextName]
	if !ok || ctx == nil || ctx.Namespace == "" {
		return defaultNamespace
	}
	return ctx.Namespace
}
