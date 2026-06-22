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

package k8s

import (
	"k8s.io/client-go/kubernetes"
)

// Client wraps Kubernetes operations for Deployah resources.
type Client struct {
	k8sClient kubernetes.Interface
	namespace string
}

// NewClient creates a new Kubernetes client for Deployah operations.
func NewClient(k8sClient kubernetes.Interface, namespace string) *Client {
	return &Client{
		k8sClient: k8sClient,
		namespace: namespace,
	}
}

// GetKubernetesClient returns the underlying Kubernetes client.
func (c *Client) GetKubernetesClient() kubernetes.Interface {
	return c.k8sClient
}

// GetNamespace returns the namespace this client operates in.
func (c *Client) GetNamespace() string {
	return c.namespace
}
