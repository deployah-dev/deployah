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
	for i, c := range p.Changes {
		if err := requireEnum(fmt.Sprintf("change %d action", i), c.Action.String(),
			semantic.Create.String(), semantic.Update.String(),
			semantic.Delete.String(), semantic.Recreate.String()); err != nil {
			return err
		}
		if err := requireEnum(fmt.Sprintf("change %d origin", i), c.Origin.Kind.String(),
			semantic.OriginHelm.String()); err != nil {
			return err
		}
		if c.Apply.Write != nil {
			if err := requireEnum(fmt.Sprintf("change %d write method", i), c.Apply.Write.Method.String(),
				semantic.WriteServerSide.String()); err != nil {
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
