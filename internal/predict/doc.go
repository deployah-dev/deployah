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

// Package predict answers what Kubernetes resource state Deployah's Helm
// server-side apply (SSA) path would produce, without mutating the cluster.
//
// [Predict] is a library. It is not the deployah plan command and is not
// wired into plan or deploy.
//
// Deployah is SSA-only. There is no client-side apply predictor and no
// apply-method switch. All production PATCH and DELETE calls use server
// dry-run.
//
// Exact Helm-resource prediction requires that the cluster API surface
// already exists at prediction time:
//
//   - the target release namespace already exists for namespaced resources
//   - every Desired and Previous resource group/version/kind is currently
//     discoverable and REST-mappable
//
// A missing namespace ([apierrors.IsNotFound]) or an unmapped kind
// ([meta.IsNoMatchError]) is returned as the native typed error. Those
// errors are not rewritten to [ActionNoOp]. They do not necessarily prove
// that a real Deployah deploy would fail: install sets CreateNamespace, and
// Deployah applies .deployah/crds/ before Helm. Namespace and CRD
// prerequisite prediction are out of scope for this package.
package predict
