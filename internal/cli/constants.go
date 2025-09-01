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

package cli

// Exit Codes
const (
	// ExitSuccess indicates successful program execution
	ExitSuccess = 0

	// ExitError is the exit code used when the application encounters an error
	ExitError = 1

	// ExitTimedOut is the exit code used when the application times out
	ExitTimedOut = 124
)

// Output Formats
const (
	// OutputFormatTable formats output as a table
	OutputFormatTable = "table"

	// OutputFormatJSON formats output as JSON
	OutputFormatJSON = "json"
)

// OutputFormats contains all valid output formats
var OutputFormats = []string{OutputFormatTable, OutputFormatJSON}
