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

package semantic

import "fmt"

// DiagnosticSeverity is the severity of a [Diagnostic]. The zero value is
// invalid.
type DiagnosticSeverity int

const (
	// DiagnosticWarning is a non-fatal plan note.
	DiagnosticWarning DiagnosticSeverity = iota + 1
)

func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticWarning:
		return "warning"
	default:
		return fmt.Sprintf("DiagnosticSeverity(%d)", int(s))
	}
}

func (s DiagnosticSeverity) valid() bool {
	return s == DiagnosticWarning
}

// DiagnosticCategory classifies a [Diagnostic]. The zero value is invalid.
type DiagnosticCategory int

const (
	// CategoryPredictionLimitation marks a Stage B prediction that is not
	// exact.
	CategoryPredictionLimitation DiagnosticCategory = iota + 1
)

func (c DiagnosticCategory) String() string {
	switch c {
	case CategoryPredictionLimitation:
		return "prediction_limitation"
	default:
		return fmt.Sprintf("DiagnosticCategory(%d)", int(c))
	}
}

func (c DiagnosticCategory) valid() bool {
	return c == CategoryPredictionLimitation
}

// Diagnostic is semantic information attached to a successfully built
// [Plan]. It is not a substitute for a prediction error.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Category DiagnosticCategory
	Message  string
	Resource *ResourceRef
}
