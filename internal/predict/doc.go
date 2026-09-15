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
// Helm [Predict] stays on server-side apply. There is no client-side
// apply predictor and no apply-method switch for Helm resources.
// [Cluster] is a generic dry-run write surface: Apply is SSA with
// caller FieldManager and ForceConflicts, and Create is a Kubernetes
// create. All production writes use server dry-run. Predict does not
// call Create.
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
// prerequisite prediction live in the plan package, which wraps [Cluster]
// for same-deploy fallback. This package stays Helm-strict.
package predict
