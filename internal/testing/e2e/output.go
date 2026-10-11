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

package e2e

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const outputExcerptLimit = 2000

// CheckOutput reports whether stdout, stderr, and exitCode satisfy exp.
func CheckOutput(stdout, stderr string, exitCode int, exp *PreparedOutput) error {
	// A nil exp means exit code 0. Extra text is fine. Every pattern must match. There is no retry.
	want := 0
	if exp != nil {
		want = exp.ExitCode
	}
	var problems []string
	if exitCode != want {
		problems = append(problems, fmt.Sprintf("exit %d, want %d", exitCode, want))
	}
	if exp != nil {
		problems = append(problems, missingPatterns("stdout", exp.Stdout, exp.StdoutPat, stdout)...)
		problems = append(problems, missingPatterns("stderr", exp.Stderr, exp.StderrPat, stderr)...)
	}
	if len(problems) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(strings.Join(problems, "; "))
	if stdout != "" {
		fmt.Fprintf(&b, "\nstdout:\n%s", excerpt(stdout))
	}
	if stderr != "" {
		fmt.Fprintf(&b, "\nstderr:\n%s", excerpt(stderr))
	}
	return errors.New(b.String())
}

func missingPatterns(stream string, compiled []*regexp.Regexp, patterns []string, text string) []string {
	var missing []string
	for i, re := range compiled {
		if re.MatchString(text) {
			continue
		}
		pattern := re.String()
		if i < len(patterns) {
			pattern = patterns[i]
		}
		missing = append(missing, fmt.Sprintf("%s missing pattern %q", stream, pattern))
	}
	return missing
}

func excerpt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= outputExcerptLimit {
		return s
	}
	return s[:outputExcerptLimit] + "\n..."
}
