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

import (
	"fmt"
	"slices"

	"deployah.dev/deployah/internal/plan/semantic"
)

func prepareRender(p semantic.Plan, opts Options) (semantic.Plan, error) {
	if err := validateRenderable(p); err != nil {
		return semantic.Plan{}, err
	}
	out := copyPlan(p)
	if !opts.ShowSecrets {
		redactPlan(&out)
	}
	return out, nil
}

func validateRenderable(p semantic.Plan) error {
	if err := requireEnum("completeness", p.Completeness.String(),
		semantic.CompletenessComplete.String(), semantic.CompletenessPartial.String()); err != nil {
		return err
	}
	if err := requireEnum("helmAction", p.HelmAction.String(),
		semantic.HelmNone.String(), semantic.HelmInstall.String(), semantic.HelmUpgrade.String()); err != nil {
		return err
	}
	for i, c := range p.Changes {
		if err := requireEnum(fmt.Sprintf("change %d action", i), c.Action.String(),
			semantic.Create.String(), semantic.Update.String(),
			semantic.Delete.String(), semantic.Replace.String()); err != nil {
			return err
		}
		if err := requireEnum(fmt.Sprintf("change %d origin", i), c.Origin.Kind.String(),
			semantic.OriginHelm.String(), semantic.OriginCRD.String(), semantic.OriginNamespace.String()); err != nil {
			return err
		}
		if c.Apply.Write != nil {
			if err := requireEnum(fmt.Sprintf("change %d write method", i), c.Apply.Write.Method.String(),
				semantic.WriteCreate.String(), semantic.WriteServerSide.String()); err != nil {
				return err
			}
		}
		if c.Apply.Delete != nil {
			if err := requireEnum(fmt.Sprintf("change %d delete propagation", i), c.Apply.Delete.Propagation.String(),
				semantic.PropagationBackground.String()); err != nil {
				return err
			}
		}
		for j, f := range c.Fields {
			if err := requireEnum(fmt.Sprintf("change %d field %d op", i, j), f.Op.String(),
				semantic.FieldAdd.String(), semantic.FieldRemove.String(), semantic.FieldReplace.String()); err != nil {
				return err
			}
		}
	}
	owned := make(map[string]string)
	changeKeys := make(map[string]int, len(p.Changes))
	for i, c := range p.Changes {
		key := refKey(c.Resource)
		if _, exists := changeKeys[key]; exists {
			changeKeys[key] = -1
			continue
		}
		changeKeys[key] = i
	}
	for i, t := range p.Tasks {
		if err := requireEnum(fmt.Sprintf("task %d phase", i), t.Phase.String(),
			semantic.TaskPreDeploy.String(), semantic.TaskPostDeploy.String(), semantic.TaskSchedule.String()); err != nil {
			return err
		}
		if err := requireEnum(fmt.Sprintf("task %d action", i), t.Action.String(),
			semantic.TaskUnchanged.String(), semantic.TaskCreate.String(),
			semantic.TaskUpdate.String(), semantic.TaskDelete.String()); err != nil {
			return err
		}
		if t.Phase == semantic.TaskSchedule {
			if len(t.Definitions) > 0 {
				return fmt.Errorf("task %s: schedule must not have hook definitions", t.Name)
			}
			if t.WillRun {
				return fmt.Errorf("task %s: schedule must not will run", t.Name)
			}
			for _, ref := range t.Resources {
				key := refKey(ref)
				idx, ok := changeKeys[key]
				if !ok {
					return fmt.Errorf("task %s: dangling resource %s", t.Name, ref)
				}
				if idx < 0 {
					return fmt.Errorf("task %s: resource %s matches more than one change", t.Name, ref)
				}
				if prev, taken := owned[key]; taken {
					return fmt.Errorf("task %s: resource %s already referenced by task %s", t.Name, ref, prev)
				}
				owned[key] = t.Name
			}
		}
		if (t.Phase == semantic.TaskPreDeploy || t.Phase == semantic.TaskPostDeploy) && len(t.Resources) > 0 {
			return fmt.Errorf("task %s: %s must not reference resource changes", t.Name, t.Phase)
		}
		if t.Action == semantic.TaskDelete && t.WillRun {
			return fmt.Errorf("task %s: delete must not will run", t.Name)
		}
		for j, d := range t.Definitions {
			if err := requireEnum(fmt.Sprintf("task %d definition %d action", i, j), d.Action.String(),
				semantic.Create.String(), semantic.Update.String(), semantic.Delete.String()); err != nil {
				return err
			}
			for k, f := range d.Fields {
				if err := requireEnum(fmt.Sprintf("task %d definition %d field %d op", i, j, k), f.Op.String(),
					semantic.FieldAdd.String(), semantic.FieldRemove.String(), semantic.FieldReplace.String()); err != nil {
					return err
				}
			}
		}
	}
	for i, d := range p.Diagnostics {
		if err := requireEnum(fmt.Sprintf("diagnostic %d severity", i), d.Severity.String(),
			semantic.DiagnosticWarning.String()); err != nil {
			return err
		}
		if err := requireEnum(fmt.Sprintf("diagnostic %d category", i), d.Category.String(),
			semantic.CategoryPredictionLimitation.String()); err != nil {
			return err
		}
	}
	return nil
}

func requireEnum(name, got string, allowed ...string) error {
	if slices.Contains(allowed, got) {
		return nil
	}
	return fmt.Errorf("invalid %s %s", name, got)
}
