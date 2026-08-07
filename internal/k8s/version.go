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

package k8s

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"
)

// MinStatefulMajor and MinStatefulMinor are the Kubernetes version floor
// for kind: stateful with persistence (RWOP GA at 1.29, PVC retention
// policy GA at 1.32). Identity-only stateful components do not require it.
const (
	MinStatefulMajor = 1
	MinStatefulMinor = 32
)

// CheckMinimumVersion probes the cluster server version and returns an
// error when it is below major.minor. reason is included in the error.
func CheckMinimumVersion(client kubernetes.Interface, major, minor int, reason string) error {
	info, err := client.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("discover cluster version: %w", err)
	}

	gotMajor, majorErr := parseVersionPart(info.Major)
	if majorErr != nil {
		return fmt.Errorf("parse cluster major version %q: %w", info.Major, majorErr)
	}
	gotMinor, minorErr := parseVersionPart(info.Minor)
	if minorErr != nil {
		return fmt.Errorf("parse cluster minor version %q: %w", info.Minor, minorErr)
	}

	if gotMajor < major || (gotMajor == major && gotMinor < minor) {
		return fmt.Errorf(
			"cluster Kubernetes version %d.%d is below required %d.%d (%s)",
			gotMajor, gotMinor, major, minor, reason,
		)
	}
	return nil
}

// parseVersionPart accepts "1", "32", "32+", and "32.0" style discovery
// strings and returns the leading integer.
func parseVersionPart(s string) (int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "+")
	if i := strings.IndexAny(s, ".-"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return 0, fmt.Errorf("empty version part")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n, nil
}
