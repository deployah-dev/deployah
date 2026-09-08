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
// See the License for the specific language governing the License.

package spec

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// EnvIdentity is the canonical identity for an environment name from
// [NormalizeEnv].
//
// Original is the exact user-requested instance (review/pr-123).
// MapKey is the logical environment used for manifest lookup and
// [LabelEnvironment] (review).
// DeploymentID is a collision-resistant, Helm-safe encoding of Original.
// It is not a reconstruction of Original and must not be reversed.
type EnvIdentity struct {
	// Original is the requested instance, for example review/pr-123.
	Original string
	// MapKey is the logical environment and [LabelEnvironment] value.
	MapKey string
	// DeploymentID encodes Original so review/pr-123 and review-pr-123
	// never share an identity. Wildcard instances use "--" as the separator.
	DeploymentID string
}

const (
	// HelmReleaseNameMax is Helm's maximum release name length.
	HelmReleaseNameMax = 53
	// helmReleaseHashLen is the hex digest appended when the full name
	// must be truncated (4 bytes, 8 hex characters).
	helmReleaseHashLen = 8
	// wildcardIDSep encodes "/" in DeploymentID. Logical names and instance
	// ids cannot contain this sequence, so the mapping stays injective.
	wildcardIDSep = "--"
)

// helmReleaseNamePattern is Helm's release-name regex, matching
// chartutil.ValidateReleaseName.
var helmReleaseNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// envInstanceIDPattern is the concrete wildcard suffix (review/<id>).
// One DNS-1123 label: lowercase letters, digits, and single dashes.
var envInstanceIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// NormalizeEnv returns the canonical identity for an environment name.
// It handles the slash-prefix pattern used by wildcard environments
// (review/pr-42 has MapKey "review"). Call [ValidateRequestedEnv] before
// this for user input; NormalizeEnv does not validate.
func NormalizeEnv(name string) EnvIdentity {
	mapKey, suffix, isWildcard := strings.Cut(name, "/")
	deploymentID := name
	if isWildcard {
		deploymentID = mapKey + wildcardIDSep + suffix
	}
	return EnvIdentity{
		Original:     name,
		MapKey:       mapKey,
		DeploymentID: deploymentID,
	}
}

// IsWildcard reports whether Original is a concrete instance of MapKey
// (review/pr-123), not the logical environment itself.
func (e EnvIdentity) IsWildcard() bool {
	return e.Original != e.MapKey
}

// ReleaseName is the Helm release name for project and this environment.
// The full identity is length-checked: at most [HelmReleaseNameMax]
// characters, Helm-safe, with a stable hash suffix when truncated.
func (e EnvIdentity) ReleaseName(project string) string {
	raw := project
	if e.DeploymentID != "" {
		if raw != "" {
			raw += "-" + e.DeploymentID
		} else {
			raw = e.DeploymentID
		}
	}
	if helmReleaseOK(raw) {
		return raw
	}
	return hashTruncateHelmName(raw, project+"\x00"+e.Original)
}

// ValidateRequestedEnv validates a user-requested environment, including
// concrete wildcard instances such as review/pr-123. Logical names use
// [ValidateEnvName]. Nested suffixes (review/foo/bar) are not supported.
func ValidateRequestedEnv(name string) error {
	if name == "" {
		return fmt.Errorf("environment name cannot be empty")
	}
	if strings.Count(name, "/") == 0 {
		return ValidateEnvName(name)
	}
	if strings.Count(name, "/") != 1 {
		return fmt.Errorf("environment %q is invalid: wildcard instances use exactly one '/' (for example review/pr-123)", name)
	}
	mapKey, suffix, _ := strings.Cut(name, "/")
	if err := ValidateEnvName(mapKey); err != nil {
		return fmt.Errorf("environment %q: %w", name, err)
	}
	if suffix == "" {
		return fmt.Errorf("environment %q is invalid: missing instance id after '/'", name)
	}
	if strings.Contains(suffix, wildcardIDSep) {
		return fmt.Errorf("environment %q is invalid: instance id cannot contain %q", name, wildcardIDSep)
	}
	if !envInstanceIDPattern.MatchString(suffix) {
		return fmt.Errorf("environment %q is invalid: instance id must be a lowercase DNS label (letters, digits, and dashes)", name)
	}
	if len(suffix) > MaxEnvironmentNameLength {
		return fmt.Errorf("environment %q is invalid: instance id must be at most %d characters", name, MaxEnvironmentNameLength)
	}
	return nil
}

func helmReleaseOK(name string) bool {
	return name != "" && len(name) <= HelmReleaseNameMax && helmReleaseNamePattern.MatchString(name)
}

func hashTruncateHelmName(raw, hashKey string) string {
	sum := sha256.Sum256([]byte(hashKey))
	digest := fmt.Sprintf("%08x", sum[:4])
	budget := HelmReleaseNameMax - 1 - helmReleaseHashLen
	prefix := raw
	if len(prefix) > budget {
		prefix = prefix[:budget]
	}
	prefix = strings.Trim(prefix, "-.")
	if prefix == "" || !helmReleaseNamePattern.MatchString(prefix) {
		// Prefix may start mid-token after truncation; keep a safe lead.
		prefix = sanitizeHelmPrefix(prefix)
	}
	name := prefix + "-" + digest
	if helmReleaseOK(name) {
		return name
	}
	return "r-" + digest
}

func sanitizeHelmPrefix(prefix string) string {
	var b strings.Builder
	b.Grow(len(prefix))
	lastDash := false
	for i := range len(prefix) {
		c := prefix[i]
		ok := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if ok {
			b.WriteByte(c)
			lastDash = false
			continue
		}
		if c == '-' && b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "r"
	}
	return s
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
