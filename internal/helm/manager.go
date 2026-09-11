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

import "helm.sh/helm/v4/pkg/kube"

// Pin Helm's SSA field manager once per process. Helm reads
// kube.ManagedFieldsManager on apply; if it is empty it uses the
// process basename, which differs between the CLI and tests. init
// runs before NewClient, so parallel constructors only read the
// global.
func init() {
	kube.ManagedFieldsManager = "deployah" //nolint:reassign // Helm field manager is process-global.
}
