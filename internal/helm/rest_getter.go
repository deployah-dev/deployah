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

package helm

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/kubeenv"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	diskcached "k8s.io/client-go/discovery/cached/disk"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	helmBurstLimit    = 100
	helmQPS           = float32(0)
	helmUserAgent     = "Helm/4.3"
	discoveryCacheTTL = 6 * time.Hour
	discoveryBurst    = 0
	discoveryQPS      = float32(0)
)

var (
	_ genericclioptions.RESTClientGetter = (*restClientGetter)(nil)
	_ clientcmd.ClientConfig             = unconfiguredClientConfig{}

	overlyCautiousIllegalFileCharacters = regexp.MustCompile(`[^(\w/.)]`)
)

// ErrDestinationNotConfigured is returned when a Helm client has no
// Kubernetes destination and an operation needs one.
var ErrDestinationNotConfigured = errors.New("kubernetes destination is not configured")

// NewRESTClientGetter returns a Helm REST client getter backed by cc.
// A nil cc yields an unconfigured getter: online Kubernetes access fails
// with [ErrDestinationNotConfigured] and does not select an ambient
// kubeconfig or in-cluster destination.
func NewRESTClientGetter(cc clientcmd.ClientConfig) genericclioptions.RESTClientGetter {
	if cc == nil {
		cc = unconfiguredClientConfig{}
	}
	return &restClientGetter{cc: cc}
}

type restClientGetter struct {
	cc clientcmd.ClientConfig
}

func (g *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	cfg, err := g.cc.ClientConfig()
	if err != nil {
		return nil, err
	}
	return applyHelmRESTRuntime(cfg), nil
}

func (g *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return g.cc
}

func (g *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	config, err := g.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	config.Burst = discoveryBurst
	config.QPS = discoveryQPS

	cacheDir := defaultKubeCacheDir()
	httpCacheDir := filepath.Join(cacheDir, "http")
	discoveryCacheDir := computeDiscoverCacheDir(filepath.Join(cacheDir, "discovery"), config.Host)
	return diskcached.NewCachedDiscoveryClientForConfig(config, discoveryCacheDir, httpCacheDir, discoveryCacheTTL)
}

func (g *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	discoveryClient, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(discoveryClient)
	return restmapper.NewShortcutExpander(mapper, discoveryClient, func(string) {}), nil
}

func applyHelmRESTRuntime(cfg *rest.Config) *rest.Config {
	cfg.Burst = helmBurstLimit
	cfg.QPS = helmQPS
	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &kubeenv.RetryingRoundTripper{Wrapped: rt}
	})
	cfg.UserAgent = helmUserAgent
	return cfg
}

func defaultKubeCacheDir() string {
	if kcd := os.Getenv("KUBECACHEDIR"); kcd != "" {
		return kcd
	}
	return filepath.Join(homedir.HomeDir(), ".kube", "cache")
}

func computeDiscoverCacheDir(parentDir, host string) string {
	schemelessHost := strings.Replace(strings.Replace(host, "https://", "", 1), "http://", "", 1)
	safeHost := overlyCautiousIllegalFileCharacters.ReplaceAllString(schemelessHost, "_")
	return filepath.Join(parentDir, safeHost)
}

type unconfiguredClientConfig struct{}

func (unconfiguredClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, ErrDestinationNotConfigured
}

func (unconfiguredClientConfig) ClientConfig() (*rest.Config, error) {
	return nil, ErrDestinationNotConfigured
}

func (unconfiguredClientConfig) Namespace() (string, bool, error) {
	return "default", false, nil
}

func (unconfiguredClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return &clientcmd.ClientConfigLoadingRules{}
}
