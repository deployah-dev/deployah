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

package view

// Role is the presentation intent of one Human output line. The zero
// value is not a role. [WriteHuman] does not pass it to [Styler].
type Role int

const (
	// RoleTitle marks a section or structural title, such as the plan
	// heading, Resources, Tasks, a task phase, Drift, or Summary.
	RoleTitle Role = iota + 1
	// RolePrimary marks ordinary body text with no diff meaning, such
	// as header metadata and summary counts.
	RolePrimary
	// RoleDiffAdded marks content introduced on the after side of a
	// diff. Create headings and bodies, inserted diff lines, unexpected
	// drift, and task create headings use it.
	RoleDiffAdded
	// RoleDiffRemoved marks content present on the before side and
	// absent from the after side. Delete headings and bodies, removed
	// diff lines, missing drift, and task delete headings use it.
	RoleDiffRemoved
	// RoleDiffModified marks the heading of an object that changed.
	// Lines inside an update body use [RoleDiffAdded], [RoleDiffRemoved],
	// and [RoleDiffContext].
	RoleDiffModified
	// RoleDiffContext marks an unchanged line kept around a changed
	// region.
	RoleDiffContext
)

// Styler decorates one Human output line for a [Role].
//
// line is one complete output line, including indentation and any diff
// marker. It has no trailing newline and contains no tab characters.
// Style returns the decorated line and must keep it on one line.
// [WriteHuman] calls Style in order, on the caller's goroutine.
type Styler interface {
	Style(role Role, line string) string
}
