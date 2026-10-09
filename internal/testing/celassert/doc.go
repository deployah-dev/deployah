// Copyright 2026 The Deployah Authors
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

// Package celassert evaluates Boolean Common Expression Language (CEL)
// assertions against JSON-like values.
//
// Programs are test infrastructure. Production deployment code must not
// import this package. The standard CEL library is the only language
// surface, so an expression cannot read files, run commands, or change
// Kubernetes.
//
// Simple Kubernetes subset checks stay with DiffSubset in the parent
// testing package. celassert does not replace that matcher.
//
// Compile an expression once and call [Program.Eval] for each input.
package celassert
