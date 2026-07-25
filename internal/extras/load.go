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
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/spec"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// crdAPIVersion is the only CustomResourceDefinition apiVersion Deployah
// accepts. apiextensions.k8s.io/v1beta1 was removed in Kubernetes 1.22.
const crdAPIVersion = "apiextensions.k8s.io/v1"

// Bundle holds the deploy-ready extras for one environment.
type Bundle struct {
	// Manifests are objects from .deployah/manifests/ for the selected environment.
	Manifests []Object
	// CRDs are CustomResourceDefinition objects from .deployah/crds/.
	CRDs []Object
}

// LoadConfig configures [Load].
type LoadConfig struct {
	// SpecDir is the directory containing deployah.yaml (and .deployah/).
	SpecDir string
	// Project is the project name for identity annotations/labels.
	Project string
	// Environment is the runtime environment (e.g. review/pr-123).
	Environment string
	// DeclaredEnvs are the registry keys used to validate manifests/<env>/ dirs.
	DeclaredEnvs []string
	// ReleaseNamespace fills empty metadata.namespace on namespaced objects.
	ReleaseNamespace string
	// Scope resolves namespaced vs cluster-scoped. Required.
	Scope ScopeResolver
	// Offline is true when cluster discovery is unavailable (plan --offline
	// or missing rest config). Unknown types are then allowed; scope defaults
	// to namespaced unless an in-repo CRD declares otherwise.
	Offline bool
}

// Load reads .deployah/manifests and .deployah/crds under SpecDir, validates
// them, merges Deployah identity metadata, and returns a deploy-ready Bundle.
// Missing directories yield an empty Bundle with a nil error.
func Load(cfg LoadConfig) (*Bundle, error) {
	if cfg.Scope == nil {
		return nil, errors.New("extras: ScopeResolver is required")
	}
	if cfg.Project == "" {
		return nil, errors.New("extras: Project is required")
	}

	root := filepath.Join(cfg.SpecDir, spec.DeployahConfigDir)
	manifestsRoot := filepath.Join(root, spec.ManifestsDir)
	crdsRoot := filepath.Join(root, spec.CRDsDir)

	envKey, _ := spec.MatchEnvKey(cfg.Environment, cfg.DeclaredEnvs)
	envSafe := spec.NormalizeEnv(cfg.Environment).K8sSafe

	manifestFiles, err := listManifestFiles(manifestsRoot, cfg.DeclaredEnvs, envKey)
	if err != nil {
		return nil, err
	}
	crdFiles, err := listCRDFiles(crdsRoot)
	if err != nil {
		return nil, err
	}

	var manifests []Object
	for _, path := range manifestFiles {
		objs, loadErr := loadFile(path, true)
		if loadErr != nil {
			return nil, loadErr
		}
		manifests = append(manifests, objs...)
	}

	var crds []Object
	for _, path := range crdFiles {
		objs, loadErr := loadFile(path, false)
		if loadErr != nil {
			return nil, loadErr
		}
		for i := range objs {
			if objs[i].Obj.GetKind() != "CustomResourceDefinition" {
				return nil, fmt.Errorf("%s: only CustomResourceDefinition objects are allowed under .deployah/crds/ (found %s)", objs[i].Path, objs[i].Obj.GetKind())
			}
			if av := objs[i].Obj.GetAPIVersion(); av != crdAPIVersion {
				return nil, fmt.Errorf("%s: CustomResourceDefinition must use apiVersion %s (found %s)", objs[i].Path, crdAPIVersion, av)
			}
			crds = append(crds, objs[i])
		}
	}

	crdScope := scopeFromCRDObjects(crds)
	scope := withCRDScope(cfg.Scope, crdScope)

	for i := range manifests {
		gvk := manifests[i].GVK()
		known, knownErr := scope.Known(gvk)
		if knownErr != nil {
			return nil, fmt.Errorf("%s: resolve type: %w", manifests[i].Path, knownErr)
		}
		if !known && !cfg.Offline {
			return nil, fmt.Errorf("%s: unknown type %s; add its CRD under .deployah/crds/ or install it on the cluster first", manifests[i].Path, gvk.String())
		}
		namespaced, scopeErr := scope.Namespaced(gvk)
		if scopeErr != nil {
			return nil, fmt.Errorf("%s: resolve scope: %w", manifests[i].Path, scopeErr)
		}
		if mergeErr := mergeIdentity(&manifests[i], cfg.Project, envSafe, spec.SourceManifests, cfg.ReleaseNamespace, namespaced); mergeErr != nil {
			return nil, mergeErr
		}
	}
	for i := range crds {
		if mergeErr := mergeIdentity(&crds[i], cfg.Project, "", spec.SourceCRDs, "", false); mergeErr != nil {
			return nil, mergeErr
		}
	}

	if dupErr := checkDuplicateIdentities(manifests); dupErr != nil {
		return nil, dupErr
	}
	if dupErr := checkDuplicateIdentities(crds); dupErr != nil {
		return nil, dupErr
	}

	return &Bundle{Manifests: manifests, CRDs: crds}, nil
}

func checkDuplicateIdentities(objs []Object) error {
	seen := make(map[string]string, len(objs))
	for i := range objs {
		id := objs[i].Identity()
		if prev, ok := seen[id.Key()]; ok {
			return fmt.Errorf("duplicate object %s in %s and %s", id, prev, objs[i].Path)
		}
		seen[id.Key()] = objs[i].Path
	}
	return nil
}

// withCRDScope returns a resolver that prefers CRD-derived scopes.
func withCRDScope(base ScopeResolver, crdScope map[string]bool) ScopeResolver {
	if len(crdScope) == 0 {
		return base
	}
	switch r := base.(type) {
	case *TableResolver:
		merged := make(map[string]bool, len(r.CRDScope)+len(crdScope))
		maps.Copy(merged, r.CRDScope)
		maps.Copy(merged, crdScope)
		return &TableResolver{CRDScope: merged}
	case *DiscoveryResolver:
		merged := make(map[string]bool, len(r.Table.CRDScope)+len(crdScope))
		maps.Copy(merged, r.Table.CRDScope)
		maps.Copy(merged, crdScope)
		return &DiscoveryResolver{Mapper: r.Mapper, Table: TableResolver{CRDScope: merged}}
	default:
		return &chainedScope{crd: &TableResolver{CRDScope: crdScope}, next: base}
	}
}

type chainedScope struct {
	crd  *TableResolver
	next ScopeResolver
}

func (c *chainedScope) Known(gvk schema.GroupVersionKind) (bool, error) {
	if known, err := c.crd.Known(gvk); err != nil || known {
		return known, err
	}
	return c.next.Known(gvk)
}

func (c *chainedScope) Namespaced(gvk schema.GroupVersionKind) (bool, error) {
	if c.crd.CRDScope != nil {
		if ns, ok := c.crd.CRDScope[crdScopeKey(gvk.Group, gvk.Kind)]; ok {
			return ns, nil
		}
	}
	return c.next.Namespaced(gvk)
}

func listManifestFiles(root string, declaredEnvs []string, envKey string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}

	var files []string
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(root, name)
		if e.IsDir() {
			if !slices.Contains(declaredEnvs, name) {
				return nil, fmt.Errorf("%s: unknown environment directory %q (must be a declared environment key)", path, name)
			}
			if envKey == "" || name != envKey {
				continue
			}
			envFiles, listErr := listYAMLFiles(path)
			if listErr != nil {
				return nil, listErr
			}
			files = append(files, envFiles...)
			continue
		}
		keep, fileErr := classifyFile(path, name)
		if fileErr != nil {
			return nil, fileErr
		}
		if keep {
			files = append(files, path)
		}
	}
	return files, nil
}

func listCRDFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			return nil, fmt.Errorf("%s: subdirectories are not allowed under .deployah/crds/", filepath.Join(root, e.Name()))
		}
	}
	return listYAMLFiles(root)
}

func listYAMLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.IsDir() {
			return nil, fmt.Errorf("%s: nested directories are not allowed", path)
		}
		keep, fileErr := classifyFile(path, name)
		if fileErr != nil {
			return nil, fileErr
		}
		if keep {
			files = append(files, path)
		}
	}
	return files, nil
}

// classifyFile returns whether the file should be loaded. Dotfiles and
// README/markdown are skipped; other non-YAML files fail.
func classifyFile(path, name string) (bool, error) {
	if strings.HasPrefix(name, ".") {
		return false, nil
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "readme") || strings.HasSuffix(lower, ".md") {
		return false, nil
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".yaml" || ext == ".yml" {
		return true, nil
	}
	return false, fmt.Errorf("%s: unsupported file (only .yaml/.yml are loaded; remove it or rename)", path)
}

// loadFile parses a multi-doc YAML file into Objects. When rejectCRD is true,
// a CustomResourceDefinition document is an error.
func loadFile(path string, rejectCRD bool) ([]Object, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path from extras dir listing under SpecDir
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var out []Object
	doc := 0
	for {
		var obj unstructured.Unstructured
		if decodeErr := decoder.Decode(&obj); decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s: parse YAML: %w", path, decodeErr)
		}
		doc++
		if len(obj.Object) == 0 {
			continue
		}
		apiVersion := obj.GetAPIVersion()
		kind := obj.GetKind()
		name := obj.GetName()
		if apiVersion == "" || kind == "" || name == "" {
			return nil, fmt.Errorf("%s: document %d missing required apiVersion, kind, or metadata.name", path, doc)
		}
		if rejectCRD && kind == "CustomResourceDefinition" {
			return nil, fmt.Errorf("%s: CustomResourceDefinition belongs in .deployah/crds/, not .deployah/manifests/", path)
		}
		raw, marshalErr := sigsyamlMarshal(&obj)
		if marshalErr != nil {
			return nil, fmt.Errorf("%s: %w", path, marshalErr)
		}
		out = append(out, Object{Path: path, Raw: raw, Obj: obj.DeepCopy()})
	}
	return out, nil
}

func sigsyamlMarshal(obj *unstructured.Unstructured) ([]byte, error) {
	o := &Object{Obj: obj}
	return o.MarshalYAML()
}
