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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	memcached "k8s.io/client-go/discovery/cached/memory"
	sigsyaml "sigs.k8s.io/yaml"
)

// FieldManager is the field manager name every drift dry-run PATCH is sent
// with. It matches the field manager Deployah's real applies use, so a
// prediction reflects Deployah's own ownership, not a foreign manager's.
const FieldManager = "deployah"

// forceOwnership is passed as [metav1.PatchOptions.Force] on every dry-run
// apply. Without it, a field another controller already owns (e.g. an HPA
// driving spec.replicas) would make the dry-run fail with a conflict.
var forceOwnership = true

// Predictor predicts a resource's post-apply state via a server-side apply
// dry-run and fetches its current live state. [Client] is the production
// implementation; tests substitute a stub to exercise [ComputeDrift]'s
// subtraction logic without a cluster.
type Predictor interface {
	// Predict returns the predicted and live YAML for the single resource
	// described by resourceYAML. live is "" with a nil error when the
	// resource does not exist yet (not a failure).
	Predict(ctx context.Context, resourceYAML string) (predicted, live string, err error)
}

// Client is the production [Predictor], talking to a real Kubernetes API
// server through a dynamic client and a discovery-backed REST mapper.
type Client struct {
	dynamicClient dynamic.Interface
	mapper        meta.RESTMapper
}

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

// newClient is the shared constructor behind [NewClient] (real clusters)
// and tests (a fake dynamic client paired with a static REST mapper).
func newClient(dyn dynamic.Interface, mapper meta.RESTMapper) *Client {
	return &Client{dynamicClient: dyn, mapper: mapper}
}

// Predict implements [Predictor].
func (c *Client) Predict(ctx context.Context, resourceYAML string) (predicted, live string, err error) {
	obj := &unstructured.Unstructured{}
	if decodeErr := sigsyaml.Unmarshal([]byte(resourceYAML), &obj.Object); decodeErr != nil {
		return "", "", fmt.Errorf("decode resource: %w", decodeErr)
	}
	gvk := obj.GroupVersionKind()

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", "", fmt.Errorf("resolve resource mapping for %s %s: %w", gvk, obj.GetName(), err)
	}

	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ri = c.dynamicClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		ri = c.dynamicClient.Resource(mapping.Resource)
	}

	predictedObj, err := ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, []byte(resourceYAML), metav1.PatchOptions{
		DryRun:       []string{metav1.DryRunAll},
		FieldManager: FieldManager,
		Force:        &forceOwnership,
	})
	if err != nil {
		return "", "", fmt.Errorf("predict %s %q: %w", gvk.Kind, obj.GetName(), err)
	}

	liveObj, err := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		predicted, err = toYAML(predictedObj)
		if err != nil {
			return "", "", err
		}
		return predicted, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("fetch live state of %s %q: %w", gvk.Kind, obj.GetName(), err)
	}

	predicted, err = toYAML(predictedObj)
	if err != nil {
		return "", "", err
	}
	live, err = toYAML(liveObj)
	if err != nil {
		return "", "", err
	}
	return predicted, live, nil
}

func toYAML(obj *unstructured.Unstructured) (string, error) {
	b, err := sigsyaml.Marshal(obj.Object)
	if err != nil {
		return "", fmt.Errorf("encode resource to YAML: %w", err)
	}
	return string(b), nil
}
