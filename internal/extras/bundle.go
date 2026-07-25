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

	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/client-go/rest"

	"deployah.dev/deployah/internal/spec"
)

// LoadFromSpec loads extras for a deploy/plan of the given spec.
// specPath is the path to deployah.yaml. When cfg is non-nil, live discovery
// is used for scope resolution; otherwise Offline is set so unknown types are
// not rejected (scope still comes from the built-in table and in-repo CRDs).
func LoadFromSpec(specPath string, spc *spec.Spec, platform *spec.PlatformConfig, environment, releaseNamespace string, cfg *rest.Config) (*Bundle, error) {
	scope, err := NewDiscoveryResolver(cfg, nil)
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
