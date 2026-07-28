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

// Package e2e holds Deployah's end-to-end suite, which drives the CLI
// in-process against a live Kind cluster.
//
// The tests are gated behind the "e2e" build tag and need a container engine;
// run them with `nix run .#test-e2e`. This file intentionally carries no build
// tag so `go list ./...` and golangci-lint can resolve the package without it.
package e2e
