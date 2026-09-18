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

	v1 "helm.sh/helm/v4/pkg/release/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// Bundle holds the deploy-ready extras for one environment.
type Bundle struct {
	// Manifests are objects from .deployah/manifests/ for the selected environment.
	Manifests []Object
	// CRDs are one entry per source file under .deployah/crds/. Bytes are
	// the exact file contents; they are never parsed or rewritten for Helm.
	CRDs []RawFile
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

// Load reads .deployah/manifests and .deployah/crds under SpecDir and
// returns a deploy-ready Bundle. It validates extra manifests and merges
// Deployah identity metadata into them. Chart CRDs are loaded as opaque
// source files. Missing directories yield an empty Bundle with a nil error.
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
	env := spec.NormalizeEnv(cfg.Environment)
	envLabel := env.MapKey
	instance := ""
	original := ""
	if cfg.Environment != "" {
		instance = env.ReleaseName(cfg.Project)
		original = env.Original
	}

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

	var crds []RawFile
	for _, path := range crdFiles {
		raw, readErr := os.ReadFile(path) // #nosec G304 -- path from extras dir listing under SpecDir
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", path, readErr)
		}
		crds = append(crds, RawFile{Path: path, Raw: raw})
	}

	crdScope := scopeFromCRDFiles(crds)
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
		if mergeErr := mergeIdentity(&manifests[i], cfg.Project, envLabel, instance, original, spec.SourceManifests, cfg.ReleaseNamespace, namespaced); mergeErr != nil {
			return nil, mergeErr
		}
	}

	if dupErr := checkDuplicateIdentities(manifests); dupErr != nil {
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

// scopeFromCRDFiles extracts group/kind -> namespaced from raw CRD source
// files. Inspection is read-only: parse failures skip that file and never
// fail Load, and nothing is written back into the source bytes.
func scopeFromCRDFiles(files []RawFile) map[string]bool {
	out := make(map[string]bool)
	for i := range files {
		inspectCRDScope(files[i].Raw, out)
	}
	return out
}

func inspectCRDScope(raw []byte, out map[string]bool) {
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	for {
		var obj map[string]any
		if err := decoder.Decode(&obj); err != nil {
			return
		}
		if len(obj) == 0 {
			continue
		}
		group, _ := unstructuredNestedString(obj, "spec", "group")
		kind, _ := unstructuredNestedString(obj, "spec", "names", "kind")
		scope, _ := unstructuredNestedString(obj, "spec", "scope")
		if group == "" || kind == "" {
			continue
		}
		out[crdScopeKey(group, kind)] = !strings.EqualFold(scope, "Cluster")
	}
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

func rejectHelmHookAnnotations(path string, obj *unstructured.Unstructured) error {
	anns := obj.GetAnnotations()
	for _, key := range []string{v1.HookAnnotation, v1.HookWeightAnnotation, v1.HookDeleteAnnotation} {
		if _, ok := anns[key]; ok {
			return fmt.Errorf("%s: Helm hook annotation %s is not supported on custom manifests; use a Deployah task for deploy hooks", path, key)
		}
	}
	return nil
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
		if rejectCRD {
			if hookErr := rejectHelmHookAnnotations(path, &obj); hookErr != nil {
				return nil, hookErr
			}
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
