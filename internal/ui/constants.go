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

package ui

import "time"

// UI and Display
const (
	// ProgressStepCount is the default number of steps in progress tracking
	ProgressStepCount = 4

	// TableMaxWidth is the maximum width for table displays
	TableMaxWidth = 120

	// SpinnerRefreshRate is how often to refresh spinner animations
	SpinnerRefreshRate = 100 * time.Millisecond
)
