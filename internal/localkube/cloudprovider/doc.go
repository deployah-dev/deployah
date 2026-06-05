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

// Package cloudprovider manages the cloud-provider-kind sidecar container
// that enables LoadBalancer, Ingress, and Gateway API in local Kind clusters.
//
// The controller runs the upstream cloud-provider-kind image as a detached
// container (via a [gopherly.dev/currus] Engine) rather than an in-process
// controller. The container connects to the host engine through a bind-mounted
// socket and joins the "kind" network so it can reach cluster nodes directly.
//
// Lifecycle: [Controller.Start] is idempotent — calling it when the container
// is already running returns nil. [Controller.Stop] removes the container.
// [Controller.Attach] starts it (if necessary) then streams its logs to the
// caller until the context is canceled, after which it stops the container.
//
// Coverage: Docker and Podman are fully supported. Containerd (nerdctl/finch)
// returns [ErrUnsupported] because the cloud-provider-kind image requires the
// Docker-compatible engine API; cluster create/delete/kubeconfig continue to
// work on all engines.
package cloudprovider
