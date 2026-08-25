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

// Package e2e drives the Deployah CLI in-process against a live Kind cluster.
//
// TestE2EFixtures runs every scenarios/*/e2e.yaml. TestCRDLifecycle mutates
// CRD files between CLI calls. The suite uses the "e2e" build tag and needs
// a container engine (`nix run .#test-e2e`).
// This file has no build tag so `go list ./...` and golangci-lint can
// resolve the package without the tag.
package e2e
