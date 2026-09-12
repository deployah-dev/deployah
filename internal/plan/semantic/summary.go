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

// Summary counts [ResourceChange] values by action. [New] always sets it
// from [Summarize]; callers cannot supply an independent summary.
type Summary struct {
	Create   int
	Update   int
	Delete   int
	Recreate int
}

// Total returns the number of resource changes.
func (s Summary) Total() int {
	return s.Create + s.Update + s.Delete + s.Recreate
}

// Summarize counts changes by action. It is the only summary calculation.
func Summarize(changes []ResourceChange) Summary {
	var s Summary
	for _, c := range changes {
		switch c.Action {
		case Create:
			s.Create++
		case Update:
			s.Update++
		case Delete:
			s.Delete++
		case Recreate:
			s.Recreate++
		}
	}
	return s
}
