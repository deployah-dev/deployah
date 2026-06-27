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
	"fmt"
	"time"
)

// ParseDuration converts a deployah duration string into whole seconds.
//
// It delegates to [time.ParseDuration], so it accepts the same syntax
// (e.g. "10s", "2m", "1h"). The JSON Schema pattern on health.alive fields
// already constrains values to positive integer seconds, minutes, or hours,
// so sub-second units and float values are rejected before they reach this
// function.
//
// Zero and negative durations are rejected.
func ParseDuration(s string) (int, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be a positive value", s)
	}
	return int(d.Seconds()), nil
}
