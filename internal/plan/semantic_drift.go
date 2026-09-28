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
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	helmHookAnnotation             = "helm.sh/hook"
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"
	kubectlLastAppliedAnnotation   = "kubectl.kubernetes.io/last-applied-configuration"
)

// absenceEquiv matches an empty Previous map at pattern with a Live
// object that omits that key. "*" is one list index. Other empty
// values still differ.
type absenceEquiv struct {
	group   string
	kind    string
	pattern []string
}

// emptyLimitsEquivalences match the empty resources.limits maps
// Kubernetes drops on Deployah Deployments, StatefulSets, and CronJobs.
var emptyLimitsEquivalences = []absenceEquiv{
	{group: "apps", kind: "Deployment", pattern: []string{"spec", "template", "spec", "containers", "*", "resources", "limits"}},
	{group: "apps", kind: "StatefulSet", pattern: []string{"spec", "template", "spec", "containers", "*", "resources", "limits"}},
	{group: "batch", kind: "CronJob", pattern: []string{"spec", "jobTemplate", "spec", "template", "spec", "containers", "*", "resources", "limits"}},
}

type driftBucketKey struct {
	group     string
	kind      string
	namespace string
}

type driftBucket struct {
	key        driftBucketKey
	namespaced bool
	ambiguous  bool
	mappings   []*meta.RESTMapping
}

// observeDrift compares Previous with Live. A fresh install returns
// nothing and does not read live. Previous snapshots stay as stored.
// Modified Live is the projected comparison. Unexpected Live drops
// server bookkeeping and the last-applied annotation. A field path
// may exist only on the comparison copy.
func observeDrift(ctx context.Context, live LiveReader, previous []declaration, header semantic.Header) ([]semantic.DriftChange, error) {
	if header.FreshInstall {
		return []semantic.DriftChange{}, nil
	}
	if header.Release == "" {
		return nil, fmt.Errorf("existing release requires a release name")
	}
	if live == nil {
		return nil, fmt.Errorf("semantic plan for an existing release requires a live reader")
	}

	known := make(map[logicalIdentity]struct{})
	buckets := map[driftBucketKey]*driftBucket{}
	var drift []semantic.DriftChange
	for _, d := range previous {
		key, namespaced := bucketOf(d)
		b := buckets[key]
		if b == nil {
			b = &driftBucket{key: key, namespaced: namespaced}
			buckets[key] = b
		}
		if d.mapping != nil {
			b.mappings = append(b.mappings, d.mapping)
		}
		if d.pairKey != nil {
			b.ambiguous = true
			continue
		}
		known[*d.identity] = struct{}{}
		change, found, err := driftForPrevious(ctx, live, d)
		if err != nil {
			return nil, err
		}
		if found {
			drift = append(drift, change)
		}
	}

	selector := labels.SelectorFromSet(labels.Set{spec.LabelInstance: header.Release})
	keys := make([]driftBucketKey, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b driftBucketKey) int {
		return strings.Compare(a.group+"\x00"+a.kind+"\x00"+a.namespace, b.group+"\x00"+b.kind+"\x00"+b.namespace)
	})
	for _, key := range keys {
		b := buckets[key]
		if b.ambiguous {
			continue
		}
		mapping := representativeMapping(b.mappings)
		if mapping == nil {
			return nil, fmt.Errorf("list live %s %s: missing resource mapping", key.kind, key.namespace)
		}
		ns := ""
		if b.namespaced {
			ns = key.namespace
		}
		items, err := live.List(ctx, mapping, ns, selector)
		if err != nil {
			return nil, fmt.Errorf("list live %s %s/%s: %w", key.kind, key.namespace, key.group, err)
		}
		for i := range items {
			change, keep, unexpectedErr := unexpectedChange(&items[i], b.namespaced, header, known)
			if unexpectedErr != nil {
				return nil, unexpectedErr
			}
			if keep {
				drift = append(drift, change)
			}
		}
	}
	if drift == nil {
		return []semantic.DriftChange{}, nil
	}
	return drift, nil
}

func bucketOf(d declaration) (driftBucketKey, bool) {
	if d.identity != nil {
		return driftBucketKey{group: d.identity.Group, kind: d.identity.Kind, namespace: d.identity.Namespace}, d.namespaced
	}
	return driftBucketKey{group: d.pairKey.Group, kind: d.pairKey.Kind, namespace: d.pairKey.Namespace}, d.namespaced
}

func driftForPrevious(ctx context.Context, live LiveReader, d declaration) (semantic.DriftChange, bool, error) {
	obj, err := live.Get(ctx, d.mapping, d.effectiveNamespace, d.identity.Name)
	if apierrors.IsNotFound(err) {
		return semantic.DriftChange{
			Resource: resourceRef(d),
			Action:   semantic.DriftMissing,
			Previous: snapshotOf(d.obj),
		}, true, nil
	}
	if err != nil {
		return semantic.DriftChange{}, false, fmt.Errorf("read live %s: %w", d.identity, err)
	}
	fields, liveCmp, diffErr := comparePreviousToLive(d, obj)
	if diffErr != nil {
		return semantic.DriftChange{}, false, fmt.Errorf("diff live %s: %w", d.identity, diffErr)
	}
	if len(fields) == 0 {
		return semantic.DriftChange{}, false, nil
	}
	return semantic.DriftChange{
		Resource: resourceRef(d),
		Action:   semantic.DriftModified,
		Previous: snapshotOf(d.obj),
		Live:     &semantic.ResourceSnapshot{Object: liveCmp},
		Fields:   fields,
	}, true, nil
}

func representativeMapping(mappings []*meta.RESTMapping) *meta.RESTMapping {
	var best *meta.RESTMapping
	for _, mapping := range mappings {
		if mapping == nil {
			continue
		}
		if best == nil || version.CompareKubeAwareVersionStrings(mapping.GroupVersionKind.Version, best.GroupVersionKind.Version) > 0 {
			best = mapping
		}
	}
	return best
}

func unexpectedChange(obj *unstructured.Unstructured, namespaced bool, header semantic.Header, known map[logicalIdentity]struct{}) (semantic.DriftChange, bool, error) {
	if !releaseOwned(obj, header.Release, header.Namespace) {
		return semantic.DriftChange{}, false, nil
	}
	id := liveIdentity(obj, namespaced)
	if _, ok := known[id]; ok {
		return semantic.DriftChange{}, false, nil
	}
	if id.Group == "" && id.Kind == "Namespace" && id.Name == header.Namespace {
		return semantic.DriftChange{}, false, nil
	}
	if id.Name == "" {
		return semantic.DriftChange{}, false, fmt.Errorf("unexpected live %s has no name", id.Kind)
	}
	return semantic.DriftChange{
		Resource: liveResourceRef(obj, namespaced),
		Action:   semantic.DriftUnexpected,
		Live:     unexpectedSnapshot(obj),
	}, true, nil
}

func releaseOwned(obj *unstructured.Unstructured, release, releaseNamespace string) bool {
	if obj.GetLabels()[spec.LabelInstance] != release {
		return false
	}
	annotations := obj.GetAnnotations()
	source := annotations[spec.AnnotationSource]
	if source != spec.SourceSpec && source != spec.SourceManifests {
		return false
	}
	if annotations[helmHookAnnotation] != "" {
		return false
	}
	if annotations[helmReleaseNameAnnotation] != release {
		return false
	}
	return annotations[helmReleaseNamespaceAnnotation] == releaseNamespace
}

func liveIdentity(obj *unstructured.Unstructured, namespaced bool) logicalIdentity {
	ns := ""
	if namespaced {
		ns = obj.GetNamespace()
	}
	return logicalIdentity{
		Group:     obj.GroupVersionKind().Group,
		Kind:      obj.GetKind(),
		Namespace: ns,
		Name:      obj.GetName(),
	}
}

func liveResourceRef(obj *unstructured.Unstructured, namespaced bool) semantic.ResourceRef {
	ns := ""
	if namespaced {
		ns = obj.GetNamespace()
	}
	return semantic.ResourceRef{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  ns,
		Name:       obj.GetName(),
	}
}

func unexpectedSnapshot(obj *unstructured.Unstructured) *semantic.ResourceSnapshot {
	cp := obj.DeepCopy()
	if cp.Object == nil {
		return &semantic.ResourceSnapshot{}
	}
	stripBookkeeping(cp.Object)
	annotations := cp.GetAnnotations()
	if _, ok := annotations[kubectlLastAppliedAnnotation]; ok {
		delete(annotations, kubectlLastAppliedAnnotation)
		if len(annotations) == 0 {
			cp.SetAnnotations(nil)
		} else {
			cp.SetAnnotations(annotations)
		}
	}
	return snapshotOf(cp)
}

func stripBookkeeping(obj map[string]any) {
	for _, pointer := range semantic.BookkeepingPointers() {
		deletePointer(obj, pointer)
	}
}

// comparePreviousToLive diffs prev's declared surface against Live.
// prev is left unchanged. The map is the projected Live snapshot and
// still contains apiVersion. The field list does not.
func comparePreviousToLive(prev declaration, live *unstructured.Unstructured) ([]semantic.FieldChange, map[string]any, error) {
	prevCmp := comparisonCopy(prev)
	foldSecretStringData(prevCmp, prev.obj.GetAPIVersion(), prev.obj.GetKind())
	liveMap := map[string]any{}
	if live != nil && live.Object != nil {
		liveMap = live.DeepCopy().Object
	}
	projected := projectValue(prevCmp, liveMap)
	liveCmp, ok := projected.(map[string]any)
	if !ok || liveCmp == nil {
		liveCmp = map[string]any{}
	}
	applyAbsenceEquivalence(prevCmp, liveCmp, prev.obj.GroupVersionKind().GroupKind())
	fields, err := semantic.DiffFields(prevCmp, liveCmp)
	if err != nil {
		return nil, nil, err
	}
	return dropAPIVersion(fields), liveCmp, nil
}

func dropAPIVersion(fields []semantic.FieldChange) []semantic.FieldChange {
	out := make([]semantic.FieldChange, 0, len(fields))
	for _, field := range fields {
		if field.Path == "/apiVersion" {
			continue
		}
		out = append(out, field)
	}
	return out
}

// foldSecretStringData copies a core v1 Secret's stringData into data
// on the comparison copy. stringData wins on the same key. Non-string
// values stay in stringData.
func foldSecretStringData(obj map[string]any, apiVersion, kind string) {
	if kind != "Secret" || (apiVersion != "v1" && apiVersion != "core/v1") {
		return
	}
	raw, ok := obj["stringData"].(map[string]any)
	if !ok {
		return
	}
	data, hadData := obj["data"].(map[string]any)
	if hadData {
		copied := make(map[string]any, len(data))
		maps.Copy(copied, data)
		data = copied
	} else {
		data = map[string]any{}
	}
	left := map[string]any{}
	folded := false
	for k, v := range raw {
		text, isString := v.(string)
		if !isString {
			left[k] = v
			continue
		}
		data[k] = base64.StdEncoding.EncodeToString([]byte(text))
		folded = true
	}
	if hadData || folded {
		obj["data"] = data
	}
	if len(left) == 0 {
		delete(obj, "stringData")
		return
	}
	obj["stringData"] = left
}

// projectValue keeps only what Previous declared in live. An empty
// map declares no keys. Lists stay in order and keep extra Live
// items. A type mismatch keeps the Live value whole.
func projectValue(declared, live any) any {
	switch typed := declared.(type) {
	case map[string]any:
		liveMap, ok := live.(map[string]any)
		if !ok {
			return copyDriftValue(live)
		}
		out := map[string]any{}
		for key, declaredChild := range typed {
			liveChild, exists := liveMap[key]
			if !exists {
				continue
			}
			out[key] = projectValue(declaredChild, liveChild)
		}
		return out
	case []any:
		liveList, ok := live.([]any)
		if !ok {
			return copyDriftValue(live)
		}
		out := make([]any, 0, len(liveList))
		for i := range liveList {
			if i < len(typed) {
				out = append(out, projectValue(typed[i], liveList[i]))
				continue
			}
			out = append(out, copyDriftValue(liveList[i]))
		}
		return out
	default:
		return copyDriftValue(live)
	}
}

func applyAbsenceEquivalence(prev, live map[string]any, gk schema.GroupKind) {
	for _, eq := range emptyLimitsEquivalences {
		if eq.group != gk.Group || eq.kind != gk.Kind {
			continue
		}
		for _, path := range expandPattern(prev, eq.pattern) {
			value, ok := lookupPath(prev, path)
			if !ok || !isEmptyMap(value) {
				continue
			}
			if _, liveOK := lookupPath(live, path); liveOK {
				continue
			}
			deletePath(prev, path)
		}
	}
}

func isEmptyMap(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

func expandPattern(obj map[string]any, pattern []string) [][]string {
	var out [][]string
	var walk func(cur any, index int, acc []string)
	walk = func(cur any, index int, acc []string) {
		if index == len(pattern) {
			out = append(out, slices.Clone(acc))
			return
		}
		token := pattern[index]
		if token != "*" {
			next, ok := mapChild(cur, token)
			if !ok {
				return
			}
			walk(next, index+1, append(acc, token))
			return
		}
		list, ok := cur.([]any)
		if !ok {
			return
		}
		for i := range list {
			walk(list[i], index+1, append(acc, strconv.Itoa(i)))
		}
	}
	walk(obj, 0, nil)
	return out
}

func mapChild(cur any, key string) (any, bool) {
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, false
	}
	next, ok := m[key]
	return next, ok
}

func lookupPath(obj map[string]any, path []string) (any, bool) {
	var cur any = obj
	for _, token := range path {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func deletePath(obj map[string]any, path []string) {
	if len(path) == 0 {
		return
	}
	parentPath := path[:len(path)-1]
	parent, ok := lookupPath(obj, parentPath)
	if !ok {
		return
	}
	token := path[len(path)-1]
	if m, isMap := parent.(map[string]any); isMap {
		delete(m, token)
	}
}

func deletePointer(obj map[string]any, pointer string) {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return
	}
	var tokens []string
	for token := range strings.SplitSeq(pointer[1:], "/") {
		tokens = append(tokens, strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~"))
	}
	deletePath(obj, tokens)
}

func copyDriftValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = copyDriftValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for i := range val {
			out = append(out, copyDriftValue(val[i]))
		}
		return out
	default:
		return v
	}
}
