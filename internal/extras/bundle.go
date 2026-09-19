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

package extras

import (
	"path/filepath"
	"strings"

	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/client-go/rest"

	"deployah.dev/deployah/internal/spec"
)

// LoadFromSpec loads extras for a deploy/plan of the given spec.
// specPath is the path to deployah.yaml. When cfg is non-nil, live discovery
// is used for scope resolution; otherwise Offline is set so unknown types are
// not rejected. Scope comes from the built-in table and live discovery, not
// from .deployah/crds/ content.
func LoadFromSpec(specPath string, spc *spec.Spec, platform *spec.PlatformConfig, environment, releaseNamespace string, cfg *rest.Config) (*Bundle, error) {
	scope, err := NewDiscoveryResolver(cfg)
	if err != nil {
		return nil, err
	}
	return Load(LoadConfig{
		SpecDir:          filepath.Dir(specPath),
		Project:          spc.Project,
		Environment:      environment,
		DeclaredEnvs:     spec.DeclaredEnvironments(spc.Environments, platform),
		ReleaseNamespace: releaseNamespace,
		Scope:            scope,
		Offline:          cfg == nil,
	})
}

// PostRendererFor returns a Helm post-renderer for the bundle's manifests, or
// a true nil interface when there are none (avoids a typed-nil *PostRenderer).
func (b *Bundle) PostRendererFor() postrenderer.PostRenderer {
	if b == nil || len(b.Manifests) == 0 {
		return nil
	}
	return &PostRenderer{Manifests: b.Manifests}
}

// CRDLifecycleNote describes Helm's install-only processing of the loaded
// .deployah/crds/ source files. upgrade is true for an existing release.
// skipInstall is true when this invocation disables install-time CRD
// processing. The files stay in the chart either way. The note lists
// source filenames; it does not interpret file contents.
func CRDLifecycleNote(files []RawFile, upgrade, skipInstall bool) string {
	if len(files) == 0 {
		return ""
	}
	var header string
	mark := "  "
	switch {
	case upgrade:
		header = "CRD files not processed on upgrade:"
	case skipInstall:
		header = "CRD files in chart (install-time processing disabled):"
	default:
		header = "CRD files to process on install:"
		mark = "  + "
	}
	var b strings.Builder
	b.WriteString(header)
	for i := range files {
		b.WriteByte('\n')
		b.WriteString(mark)
		b.WriteString(crdDisplayPath(files[i].Path))
	}
	return b.String()
}

func crdDisplayPath(path string) string {
	name := filepath.Base(path)
	if name == "" || name == "." || name == "/" {
		name = path
	}
	return filepath.ToSlash(filepath.Join(spec.DeployahConfigDir, spec.CRDsDir, name))
}
