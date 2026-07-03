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

package localkube

import "time"

const (
	// defaultManagerTimeout is the budget applied to every Manager method
	// when the caller has not set its own deadline.
	defaultManagerTimeout = 5 * time.Minute

	// defaultCreateWaitReady is how long Create waits for all nodes to become Ready.
	defaultCreateWaitReady = 2 * time.Minute

	// stateSubdirKubeconfigs is the XDG StateHome subpath for kubeconfig copies.
	stateSubdirKubeconfigs = "deployah/localkube/kubeconfigs"

	// DefaultIngressIP is the host IP at which the Ingress controller is
	// reachable after cloud-provider-kind maps LoadBalancer ports to the host
	// (via --enable-lb-port-mapping). It matches the default PortMapping
	// ListenAddress used by Kind's extraPortMappings.
	DefaultIngressIP = "127.0.0.1"
)
