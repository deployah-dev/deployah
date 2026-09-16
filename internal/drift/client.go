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

package drift

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	memcached "k8s.io/client-go/discovery/cached/memory"
	sigsyaml "sigs.k8s.io/yaml"
)

// Client is the production [LiveReader], talking to a real Kubernetes API
// server through a dynamic client and a discovery-backed REST mapper.
type Client struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
}

var _ LiveReader = (*Client)(nil)

// NewClient builds a drift [Client] targeting the cluster described by cfg.
func NewClient(cfg *rest.Config) (*Client, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcached.NewMemCacheClient(disco))
	return newClient(dyn, mapper), nil
}

func newClient(dyn dynamic.Interface, mapper meta.RESTMapper) *Client {
	return &Client{dynamicClient: dyn, mapper: mapper}
}

// Live implements [LiveReader]. It GETs the object and does not dry-run
// apply.
func (c *Client) Live(ctx context.Context, resourceYAML string) (live string, err error) {
	obj := &unstructured.Unstructured{}
	if decodeErr := sigsyaml.Unmarshal([]byte(resourceYAML), &obj.Object); decodeErr != nil {
		return "", fmt.Errorf("decode resource: %w", decodeErr)
	}
	gvk := obj.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", fmt.Errorf("resolve resource mapping for %s %s: %w", gvk, obj.GetName(), err)
	}

	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ri = c.dynamicClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		ri = c.dynamicClient.Resource(mapping.Resource)
	}

	liveObj, err := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("fetch live state of %s %q: %w", gvk.Kind, obj.GetName(), err)
	}
	return toYAML(liveObj)
}

func toYAML(obj *unstructured.Unstructured) (string, error) {
	b, err := sigsyaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("encode resource to YAML: %w", err)
	}
	return string(b), nil
}
