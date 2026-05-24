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

// Package cli provides shared CLI helpers used across Deployah commands.
//
// It defines exit codes, release view models for table and structured output,
// and rendering utilities that keep command handlers consistent.
//
// # Exit codes
//
// Use [ExitSuccess], [ExitError], and related constants so every command
// reports failures the same way to shells and CI.
package cli
