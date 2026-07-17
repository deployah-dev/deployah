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

package plan

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"

	yamlv3 "go.yaml.in/yaml/v3"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ComputeDiff parses previous and current as "---"-separated multi-document
// Kubernetes manifests and returns the resulting [Plan]'s Changes and
// Summary. previous may be the empty string (a fresh install), in which
// case every resource in current shows as an addition. The returned Plan's
// Header is always the zero value; the caller fills it in from what
// [LastSuccessfulRelease] and the render step already know.
func ComputeDiff(previous, current string) (*Plan, error) {
	prevDocs, err := parseResources(previous)
	if err != nil {
		return nil, fmt.Errorf("parsing previous manifest: %w", err)
	}
	currDocs, err := parseResources(current)
	if err != nil {
		return nil, fmt.Errorf("parsing current manifest: %w", err)
	}

	for _, d := range prevDocs {
		normalizeResource(d.node)
	}
	for _, d := range currDocs {
		normalizeResource(d.node)
	}

	prevByKey := indexResources(prevDocs)
	seenInCurrent := make(map[resourceKey]bool, len(currDocs))

	p := &Plan{}

	for _, cd := range currDocs {
		seenInCurrent[cd.key] = true

		pd, existed := prevByKey[cd.key]
		if !existed {
			p.Changes = append(p.Changes, changeFor(ActionAdd, cd.key, nil))
			p.Summary.Add++
			continue
		}

		fields, diffErr := diffResource(pd.node, cd.node)
		if diffErr != nil {
			return nil, fmt.Errorf("diffing %s: %w", cd.key, diffErr)
		}
		if len(fields) == 0 {
			continue // resource rendered identically: not a change
		}
		p.Changes = append(p.Changes, changeFor(ActionChange, cd.key, fields))
		p.Summary.Change++
	}

	for _, pd := range prevDocs {
		if seenInCurrent[pd.key] {
			continue
		}
		p.Changes = append(p.Changes, changeFor(ActionDestroy, pd.key, nil))
		p.Summary.Destroy++
	}

	slices.SortFunc(p.Changes, func(a, b Change) int {
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})

	return p, nil
}

func changeFor(action Action, key resourceKey, fields []FieldDiff) Change {
	return Change{
		Action:     action,
		Kind:       key.Kind,
		APIVersion: key.APIVersion,
		Name:       key.Name,
		Namespace:  key.Namespace,
		Fields:     fields,
	}
}

// diffResource runs dyff on a single matched resource pair (both nodes must
// describe the same Kubernetes object identity) and converts its report
// into [FieldDiff] entries. It returns an empty, non-nil-error slice when
// the two renders of the resource are equivalent.
func diffResource(prev, curr *yamlv3.Node) ([]FieldDiff, error) {
	from := ytbx.InputFile{Documents: []*yamlv3.Node{prev}}
	to := ytbx.InputFile{Documents: []*yamlv3.Node{curr}}

	report, err := dyff.CompareInputFiles(from, to)
	if err != nil {
		return nil, fmt.Errorf("comparing resource: %w", err)
	}

	var fields []FieldDiff
	for _, diff := range report.Diffs {
		path := diff.Path.ToDotStyle()
		segments := pathSegments(diff.Path)
		for _, detail := range diff.Details {
			switch detail.Kind {
			case dyff.ADDITION:
				fields = append(fields, expandMapDetail(FieldAdded, path, segments, detail.To)...)

			case dyff.REMOVAL:
				fields = append(fields, expandMapDetail(FieldRemoved, path, segments, detail.From)...)

			case dyff.MODIFICATION:
				fields = append(fields, FieldDiff{
					Path:       path,
					Segments:   segments,
					ChangeKind: FieldChanged,
					Old:        nodeToString(detail.From),
					New:        nodeToString(detail.To),
				})

			case dyff.ORDERCHANGE:
				// A pure reordering of an already-identical list (e.g. env
				// vars re-sorted by the template engine) has no effect on
				// the applied resource, so it is not a meaningful change to
				// show in a deploy-confirmation preview.
			}
		}
	}

	return fields, nil
}

// HooksChanged reports whether the set of Helm hooks differs between two
// releases (added, removed, or a hook whose manifest content changed). It
// is not part of ComputeDiff because Hooks live outside the "---"-separated
// manifest string ([deployah.dev/deployah/internal/render.RenderResult.Manifest]
// never includes them); the
// caller compares the two hook slices it already has from the previous
// release and the current render.
func HooksChanged(previous, current []*v1.Hook) bool {
	if len(previous) != len(current) {
		return true
	}

	byName := func(hooks []*v1.Hook) map[string]string {
		m := make(map[string]string, len(hooks))
		for _, h := range hooks {
			m[h.Name] = h.Manifest
		}
		return m
	}

	prev, curr := byName(previous), byName(current)
	if len(prev) != len(curr) {
		return true
	}
	for name, manifest := range prev {
		if curr[name] != manifest {
			return true
		}
	}
	return false
}

// expandMapDetail splits a whole-map ADDITION/REMOVAL into one FieldDiff per
// leaf field, so e.g. removing one key from an otherwise-unchanged map
// renders as its own line instead of a flattened block dump. Named list
// entries (e.g. a whole container added/removed) are exempt and stay a
// single dumped block; see writeYAMLValueBlock.
func expandMapDetail(kind FieldChangeKind, path string, segments []PathSegment, node *yamlv3.Node) []FieldDiff {
	isNamedListEntry := len(segments) > 0 && segments[len(segments)-1].ListKey != ""
	if isNamedListEntry || node == nil || node.Kind != yamlv3.MappingNode {
		fd := FieldDiff{Path: path, Segments: segments, ChangeKind: kind}
		if kind == FieldAdded {
			fd.New = nodeToString(node)
		} else {
			fd.Old = nodeToString(node)
		}
		return []FieldDiff{fd}
	}

	var fields []FieldDiff
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		childSegments := append(append([]PathSegment{}, segments...), PathSegment{Name: key, Idx: -1})
		fields = append(fields, expandMapDetail(kind, path+"."+key, childSegments, node.Content[i+1])...)
	}
	return fields
}

// pathSegments converts a dyff/ytbx structured path into [PathSegment]s.
// ytbx.PathElement disambiguates a plain map key from a named list-entry
// identifier as follows (see gonvenience/ytbx path.go): a named list entry
// has both Key (the identifying field, e.g. "name") and Name (the
// identifier value, e.g. "web") set; a plain map key has only Name set;
// an unmatched list entry has only Idx set (>= 0).
func pathSegments(path *ytbx.Path) []PathSegment {
	segments := make([]PathSegment, 0, len(path.PathElements))
	for _, el := range path.PathElements {
		switch {
		case el.Key != "":
			segments = append(segments, PathSegment{Name: el.Name, ListKey: el.Key, Idx: -1})
		case el.Name != "":
			segments = append(segments, PathSegment{Name: el.Name, Idx: -1})
		default:
			segments = append(segments, PathSegment{Idx: el.Idx})
		}
	}
	return segments
}

// nodeToString renders a dyff detail value for display. Scalars render as
// their literal value; mapping and sequence nodes (a whole block added or
// removed, e.g. a new container) render as an indented YAML snippet.
func nodeToString(node *yamlv3.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yamlv3.ScalarNode {
		return node.Value
	}
	out, err := yamlv3.Marshal(node)
	if err != nil {
		return fmt.Sprintf("<error rendering value: %v>", err)
	}
	return strings.TrimRight(string(out), "\n")
}
