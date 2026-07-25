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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	memcached "k8s.io/client-go/discovery/cached/memory"
)

// ScopeResolver reports whether a GVK is known and whether it is namespaced.
type ScopeResolver interface {
	// Known reports whether gvk is a built-in kind, declared by an in-repo
	// CRD, or present in live discovery.
	Known(gvk schema.GroupVersionKind) (bool, error)
	// Namespaced reports whether gvk is namespaced. When Known is false and
	// discovery is unavailable, callers may still use this; unknown kinds
	// default to namespaced.
	Namespaced(gvk schema.GroupVersionKind) (bool, error)
}

// builtInScope maps "group/kind" (lowercase; core group is empty so keys look
// like "/configmap") to namespaced. Keys include a small DX allowlist for
// common operator APIs (cert-manager, prometheus-operator) so extras for those
// kinds load offline without embedding their CRDs; arbitrary groups never match
// by Kind alone.
var builtInScope = map[string]bool{
	// core
	"/namespace":             false,
	"/node":                  false,
	"/persistentvolume":      false,
	"/persistentvolumeclaim": true,
	"/pod":                   true,
	"/service":               true,
	"/configmap":             true,
	"/secret":                true,
	"/serviceaccount":        true,
	"/endpoints":             true,
	"/limitrange":            true,
	"/resourcequota":         true,
	"/event":                 true,

	// apps
	"apps/deployment":         true,
	"apps/statefulset":        true,
	"apps/daemonset":          true,
	"apps/replicaset":         true,
	"apps/controllerrevision": true,

	// batch
	"batch/job":     true,
	"batch/cronjob": true,

	// networking.k8s.io
	"networking.k8s.io/ingress":       true,
	"networking.k8s.io/networkpolicy": true,
	"networking.k8s.io/ingressclass":  false,

	// autoscaling
	"autoscaling/horizontalpodautoscaler": true,

	// policy
	"policy/poddisruptionbudget": true,
	"policy/podsecuritypolicy":   false,

	// rbac.authorization.k8s.io
	"rbac.authorization.k8s.io/role":               true,
	"rbac.authorization.k8s.io/rolebinding":        true,
	"rbac.authorization.k8s.io/clusterrole":        false,
	"rbac.authorization.k8s.io/clusterrolebinding": false,

	// storage.k8s.io
	"storage.k8s.io/storageclass":     false,
	"storage.k8s.io/csidriver":        false,
	"storage.k8s.io/csinode":          false,
	"storage.k8s.io/volumeattachment": false,

	// scheduling.k8s.io
	"scheduling.k8s.io/priorityclass": false,

	// apiextensions.k8s.io
	"apiextensions.k8s.io/customresourcedefinition": false,

	// admissionregistration.k8s.io
	"admissionregistration.k8s.io/mutatingwebhookconfiguration":   false,
	"admissionregistration.k8s.io/validatingwebhookconfiguration": false,

	// apiregistration.k8s.io
	"apiregistration.k8s.io/apiservice": false,

	// node.k8s.io
	"node.k8s.io/runtimeclass": false,

	// flowcontrol.apiserver.k8s.io
	"flowcontrol.apiserver.k8s.io/flowschema":                 false,
	"flowcontrol.apiserver.k8s.io/prioritylevelconfiguration": false,

	// coordination.k8s.io
	"coordination.k8s.io/lease": true,

	// discovery.k8s.io
	"discovery.k8s.io/endpointslice": true,

	// cert-manager (DX allowlist)
	"cert-manager.io/certificate":        true,
	"cert-manager.io/issuer":             true,
	"cert-manager.io/clusterissuer":      false,
	"cert-manager.io/certificaterequest": true,
	"acme.cert-manager.io/challenge":     true,
	"acme.cert-manager.io/order":         true,

	// prometheus-operator (DX allowlist)
	"monitoring.coreos.com/prometheusrule":     true,
	"monitoring.coreos.com/servicemonitor":     true,
	"monitoring.coreos.com/podmonitor":         true,
	"monitoring.coreos.com/probe":              true,
	"monitoring.coreos.com/alertmanagerconfig": true,
}

// TableResolver resolves scope from a built-in group/kind table and optional
// CRD-provided scopes. Unknown kinds are not Known; Namespaced defaults
// them to namespaced when called anyway.
type TableResolver struct {
	// CRDScope maps "group/kind" (lowercase) to namespaced, from .deployah/crds.
	CRDScope map[string]bool
}

// Known implements ScopeResolver.
func (r *TableResolver) Known(gvk schema.GroupVersionKind) (bool, error) {
	key := crdScopeKey(gvk.Group, gvk.Kind)
	if r != nil && r.CRDScope != nil {
		if _, ok := r.CRDScope[key]; ok {
			return true, nil
		}
	}
	_, ok := builtInScope[key]
	return ok, nil
}

// Namespaced implements ScopeResolver.
func (r *TableResolver) Namespaced(gvk schema.GroupVersionKind) (bool, error) {
	key := crdScopeKey(gvk.Group, gvk.Kind)
	if r != nil && r.CRDScope != nil {
		if ns, ok := r.CRDScope[key]; ok {
			return ns, nil
		}
	}
	if ns, ok := builtInScope[key]; ok {
		return ns, nil
	}
	return true, nil
}

// DiscoveryResolver wraps a RESTMapper and falls back to TableResolver.
type DiscoveryResolver struct {
	Mapper meta.RESTMapper
	Table  TableResolver
}

// NewDiscoveryResolver builds a ScopeResolver from a rest.Config. When cfg
// is nil, it returns a table-only resolver.
func NewDiscoveryResolver(cfg *rest.Config, crdScope map[string]bool) (ScopeResolver, error) {
	table := TableResolver{CRDScope: crdScope}
	if cfg == nil {
		return &table, nil
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcached.NewMemCacheClient(disco))
	return &DiscoveryResolver{Mapper: mapper, Table: table}, nil
}

// Known implements ScopeResolver.
func (r *DiscoveryResolver) Known(gvk schema.GroupVersionKind) (bool, error) {
	if r.Mapper != nil {
		if _, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err == nil {
			return true, nil
		}
	}
	return r.Table.Known(gvk)
}

// Namespaced implements ScopeResolver.
func (r *DiscoveryResolver) Namespaced(gvk schema.GroupVersionKind) (bool, error) {
	if r.Mapper != nil {
		mapping, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err == nil && mapping != nil {
			return mapping.Scope.Name() == meta.RESTScopeNameNamespace, nil
		}
	}
	return r.Table.Namespaced(gvk)
}

func crdScopeKey(group, kind string) string {
	return strings.ToLower(group) + "/" + strings.ToLower(kind)
}

// scopeFromCRDObjects extracts group/kind -> namespaced from CRD Objects.
func scopeFromCRDObjects(crds []Object) map[string]bool {
	out := make(map[string]bool)
	for i := range crds {
		group, _ := unstructuredNestedString(crds[i].Obj.Object, "spec", "group")
		kind, _ := unstructuredNestedString(crds[i].Obj.Object, "spec", "names", "kind")
		scope, _ := unstructuredNestedString(crds[i].Obj.Object, "spec", "scope")
		if group == "" || kind == "" {
			continue
		}
		out[crdScopeKey(group, kind)] = !strings.EqualFold(scope, "Cluster")
	}
	return out
}

// GroupVersionsFromCRDs returns the set of "group/version" strings declared by
// the given CRD objects (every entry under spec.versions). Used to skip
// required-API checks for APIs this deploy is about to install.
func GroupVersionsFromCRDs(crds []Object) map[string]struct{} {
	out := make(map[string]struct{})
	for i := range crds {
		group, ok := unstructuredNestedString(crds[i].Obj.Object, "spec", "group")
		if !ok || group == "" {
			continue
		}
		versions, hasVersions := unstructuredNestedSlice(crds[i].Obj.Object, "spec", "versions")
		if !hasVersions {
			continue
		}
		for _, v := range versions {
			vm, isMap := v.(map[string]any)
			if !isMap {
				continue
			}
			name, isString := vm["name"].(string)
			if !isString || name == "" {
				continue
			}
			out[group+"/"+name] = struct{}{}
		}
	}
	return out
}

func unstructuredNestedString(obj map[string]any, fields ...string) (string, bool) {
	cur := any(obj)
	for _, f := range fields {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[f]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}

func unstructuredNestedSlice(obj map[string]any, fields ...string) ([]any, bool) {
	cur := any(obj)
	for _, f := range fields {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[f]
		if !ok {
			return nil, false
		}
	}
	s, ok := cur.([]any)
	return s, ok
}
