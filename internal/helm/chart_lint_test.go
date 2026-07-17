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

package helm

import (
	"io/fs"
	"strings"
	"testing"
)

// nonDeterministicTemplateTokens are Go template / Sprig constructs whose
// output can differ between two renders of the same chart values (e.g. a
// live cluster lookup, or a random/CA-generating function), or that read
// Helm's upgrade-only template context. Any of these breaks the plan/apply
// determinism guarantee (docs/plan-proposal.md section 6): a chart template
// that uses one of them WILL make `deployah deploy` abort every time with
// "rendered manifests changed between plan and apply". This is exactly the
// bug the self-signed TLS template used to have (genCA/genSignedCert +
// lookup) before certs were moved to Go-side materialization.
var nonDeterministicTemplateTokens = []string{
	"lookup",
	".Release.Revision",
	".Release.IsUpgrade",
	"genCA",
	"genSignedCert",
	"genSelfSignedCert",
	"randAlphaNum",
	"randAlpha",
	"randNumeric",
	"randAscii",
	"uuidv4",
	"now",
}

// TestChartTemplatesDeterministic walks the embedded chart and fails if any
// template file references a construct known to make rendering
// non-deterministic across two renders of identical input.
func TestChartTemplatesDeterministic(t *testing.T) {
	t.Parallel()

	err := fs.WalkDir(ChartTemplateFS, "chart", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.Contains(path, "templates/") {
			return nil
		}
		ext := path[strings.LastIndex(path, "."):]
		if ext != ".yaml" && ext != ".tpl" && ext != ".txt" && ext != ".gotmpl" {
			return nil
		}

		data, readErr := ChartTemplateFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(data)

		for _, token := range nonDeterministicTemplateTokens {
			if strings.Contains(content, token) {
				t.Errorf("%s: uses non-deterministic template construct %q; plan/apply verification requires identical output across renders of the same input", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk chart templates: %v", err)
	}
}
