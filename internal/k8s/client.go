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

// Package k8s provides functions to interact with Kubernetes.
package k8s

import (
	"context"
	"fmt"

	"github.com/deployah-dev/deployah/internal/runtime"
)

// Client wraps Kubernetes operations for Deployah resources
type Client struct {
	k8sClient runtime.KubernetesClient
	namespace string
}

// NewClient creates a new Kubernetes client for Deployah operations
func NewClient(k8sClient runtime.KubernetesClient, namespace string) *Client {
	return &Client{
		k8sClient: k8sClient,
		namespace: namespace,
	}
}

// NewClientFromRuntime creates a new client from a runtime instance
func NewClientFromRuntime(ctx context.Context, rt *runtime.Runtime) (*Client, error) {
	k8sClient, err := rt.Kubernetes()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	return NewClient(k8sClient, rt.Namespace()), nil
}

// GetKubernetesClient returns the underlying Kubernetes client
func (c *Client) GetKubernetesClient() runtime.KubernetesClient {
	return c.k8sClient
}

// GetNamespace returns the namespace this client operates in
func (c *Client) GetNamespace() string {
	return c.namespace
}
