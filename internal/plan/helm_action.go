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

package plan

import (
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
)

func deriveHelmAction(op helm.Operation, resourcesChanged bool, hookTasks []semantic.TaskPlan) semantic.HelmAction {
	if op == helm.OperationInstall {
		return semantic.HelmInstall
	}
	if resourcesChanged {
		return semantic.HelmUpgrade
	}
	for _, t := range hookTasks {
		if t.Phase == semantic.TaskSchedule {
			continue
		}
		if t.Action != semantic.TaskUnchanged {
			return semantic.HelmUpgrade
		}
	}
	return semantic.HelmNone
}
