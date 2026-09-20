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

import "deployah.dev/deployah/internal/extras"

// StampChartCRDs copies presentation identity from loaded CRD documents
// onto p and sets Helm lifecycle for this invocation. upgrade is Helm
// IsUpgrade, not FreshInstall: failed-only history is still an upgrade.
// skipCRDs is ignored when upgrade is true.
func StampChartCRDs(p *Plan, docs []extras.CRDDoc, upgrade, skipCRDs bool) {
	if p == nil {
		return
	}
	lifecycle := ChartCRDProcess
	willProcess := true
	switch {
	case upgrade:
		lifecycle = ChartCRDUpgrade
		willProcess = false
	case skipCRDs:
		lifecycle = ChartCRDSkip
		willProcess = false
	}
	p.ChartCRDs = make([]ChartCRD, 0, len(docs))
	for _, d := range docs {
		p.ChartCRDs = append(p.ChartCRDs, ChartCRD{
			Source:      extras.CRDDisplayPath(d.Path),
			Index:       d.Index,
			Kind:        d.Kind,
			Name:        d.Name,
			Lifecycle:   lifecycle,
			WillProcess: willProcess,
			YAML:        string(d.YAML),
		})
	}
}
