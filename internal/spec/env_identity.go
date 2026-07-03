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

package spec

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
)

// EnvIdentity is the canonical identity for an environment name. A single
// NormalizeEnv call is the only entry point. All subsystems (manifest lookup,
// platform lookup, component.Environments filter, release name generation,
// cache keys, and explain output) use fields from the same EnvIdentity.
type EnvIdentity struct {
	// Original is the raw environment name as supplied by the caller.
	Original string
	// MapKey is the key used for map lookups: split on the first "/" and take
	// the prefix. For "review/pr-123" MapKey is "review". For "production" it
	// equals Original.
	MapKey string
	// K8sSafe is a Kubernetes-safe version of Original: "/" replaced with "-",
	// truncated to 53 characters. When truncation changes the string a 4-char
	// hex hash of Original is appended (after truncating to 49 chars) to keep
	// uniqueness. Safe for Helm release name suffixes and label values.
	K8sSafe string
}

const k8sMaxLen = 53

// NormalizeEnv returns the canonical identity for an environment name.
// It handles the slash-prefix pattern used by wildcard environments (e.g.
// "review/pr-42" has MapKey "review").
func NormalizeEnv(name string) EnvIdentity {
	mapKey, _, _ := strings.Cut(name, "/")

	k8sSafe := strings.ReplaceAll(name, "/", "-")
	if len(k8sSafe) > k8sMaxLen {
		hash := sha256.Sum256([]byte(name))
		suffix := fmt.Sprintf("%04x", hash[:2])
		k8sSafe = k8sSafe[:k8sMaxLen-5] + "-" + suffix
	}

	return EnvIdentity{
		Original: name,
		MapKey:   mapKey,
		K8sSafe:  k8sSafe,
	}
}

// matchEnvKey returns the matching map key for candidate within mapKeys.
// It first tries an exact match; on miss it splits candidate on the first "/"
// and retries with the prefix. Returns the matched key and true on success.
//
// This is the universal matcher for manifest environment lookup, platform
// environment lookup, and component.Environments filter. All three use
// exact-then-prefix semantics.
//
// MatchEnvKey is the exported alias used in tests and by the resolve command.
func matchEnvKey(candidate string, mapKeys []string) (string, bool) {
	if slices.Contains(mapKeys, candidate) {
		return candidate, true
	}
	if prefix, _, ok := strings.Cut(candidate, "/"); ok {
		if slices.Contains(mapKeys, prefix) {
			return prefix, true
		}
	}
	return "", false
}

// MatchEnvKey is the exported form of matchEnvKey.
var MatchEnvKey = matchEnvKey
