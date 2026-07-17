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
	"fmt"
	"io"
	"strconv"
	"strings"

	"nabat.dev/theme"
)

// yamlTreeNode is one node in the nested tree ModeYAML builds from a
// Change's Fields, reconstructing the real manifest shape (map nesting and
// named list entries) instead of dyff's flattened dot path.
type yamlTreeNode struct {
	children map[string]*yamlTreeNode
	order    []string // insertion order of children, for stable output
	// listKey is non-empty when this node is one entry of a named-entry
	// list (e.g. a container matched by its "name" field): it holds the
	// identifying field name, and the node's own key (as stored in the
	// parent's children map) is the identifier value.
	listKey string
	// field is set when a FieldDiff's path ends exactly at this node.
	field *FieldDiff
}

func newYAMLTreeNode() *yamlTreeNode {
	return &yamlTreeNode{children: make(map[string]*yamlTreeNode)}
}

// buildYAMLTree merges every field's Segments into one tree.
func buildYAMLTree(fields []FieldDiff) *yamlTreeNode {
	root := newYAMLTreeNode()
	for i := range fields {
		insertYAMLField(root, fieldSegments(&fields[i]), &fields[i])
	}
	return root
}

// fieldSegments returns f.Segments, falling back to splitting f.Path into
// plain map-key segments when Segments wasn't populated (e.g. a Change
// built by hand rather than via [ComputeDiff]/dyff) -- without this,
// distinct hand-built fields would collide into one tree node.
func fieldSegments(f *FieldDiff) []PathSegment {
	if len(f.Segments) > 0 || f.Path == "" {
		return f.Segments
	}
	parts := strings.Split(f.Path, ".")
	segments := make([]PathSegment, 0, len(parts))
	for _, part := range parts {
		segments = append(segments, PathSegment{Name: part, Idx: -1})
	}
	return segments
}

func insertYAMLField(node *yamlTreeNode, segments []PathSegment, field *FieldDiff) {
	if len(segments) == 0 {
		// Truly empty path (no Segments AND no Path): attach directly so
		// writeYAMLTree can still print it instead of silently dropping it.
		node.field = field
		return
	}

	seg := segments[0]
	key := seg.Name
	if seg.ListKey == "" && seg.Name == "" {
		key = strconv.Itoa(seg.Idx)
	}

	child, ok := node.children[key]
	if !ok {
		child = newYAMLTreeNode()
		node.children[key] = child
		node.order = append(node.order, key)
	}
	if seg.ListKey != "" {
		child.listKey = seg.ListKey
	}
	insertYAMLField(child, segments[1:], field)
}

// yamlLeafStyle controls how a leaf's old/new values render as text, so
// the same tree structure serves both the ordinary diff ("X -> Y") and the
// drift section ("expected X, live Y").
type yamlLeafStyle int

const (
	yamlLeafStyleChange yamlLeafStyle = iota
	yamlLeafStyleDrift
)

// yamlTreeBaseIndent matches the 4-space ("    ") indent every other mode
// uses for a Change's top-level fields.
const yamlTreeBaseIndent = 2

// writeYAMLTree renders fields as a nested YAML-shaped tree.
func writeYAMLTree(w io.Writer, fields []FieldDiff, opts TextOptions, style yamlLeafStyle) error {
	root := buildYAMLTree(fields)
	if root.field != nil {
		if err := writeYAMLLeaf(w, yamlTreeBaseIndent, "(root)", *root.field, opts, style); err != nil {
			return err
		}
	}
	return writeYAMLChildren(w, root, yamlTreeBaseIndent, opts, style)
}

func writeYAMLChildren(w io.Writer, node *yamlTreeNode, indent int, opts TextOptions, style yamlLeafStyle) error {
	for _, key := range node.order {
		if err := writeYAMLNode(w, key, node.children[key], indent, opts, style); err != nil {
			return err
		}
	}
	return nil
}

func writeYAMLNode(w io.Writer, key string, node *yamlTreeNode, indent int, opts TextOptions, style yamlLeafStyle) error {
	pad := strings.Repeat("  ", indent)

	if node.listKey != "" {
		// A named list entry: "- <listKey>: <name>", then either its
		// remaining changed fields (a scalar field inside an existing
		// item changed) or, when the whole item was added/removed as one
		// unit, its content dumped directly underneath.
		if _, err := fmt.Fprintf(w, "%s- %s: %s\n", pad, node.listKey, key); err != nil {
			return err
		}
		childIndent := indent + 1
		if node.field != nil {
			return writeYAMLValueBlock(w, childIndent, *node.field, opts, style)
		}
		return writeYAMLChildren(w, node, childIndent, opts, style)
	}

	if node.field != nil && len(node.order) == 0 {
		return writeYAMLLeaf(w, indent, key, *node.field, opts, style)
	}

	if _, err := fmt.Fprintf(w, "%s%s:\n", pad, key); err != nil {
		return err
	}
	return writeYAMLChildren(w, node, indent+1, opts, style)
}

// writeYAMLValueBlock dumps a whole item's added/removed/changed content
// directly under its "- <listKey>: <name>" line, for a diff that reports
// an entire named list entry at once (e.g. a whole new container) rather
// than per-field.
func writeYAMLValueBlock(w io.Writer, indent int, f FieldDiff, opts TextOptions, style yamlLeafStyle) error {
	pad := strings.Repeat("  ", indent)
	if f.Masked && !opts.ShowSecrets {
		line := opts.Theme.Style(theme.TextMuted).Render(fmt.Sprintf("%s(masked) %s", pad, f.ChangeKind))
		_, err := fmt.Fprintln(w, line)
		return err
	}
	token := fieldToken(f.ChangeKind)
	if style == yamlLeafStyleDrift {
		token = theme.StatusWarning
	}
	render := opts.Theme.Style(token).Render
	switch f.ChangeKind {
	case FieldAdded:
		_, err := fmt.Fprintln(w, render(indentBlock(f.New, pad)))
		return err
	case FieldRemoved:
		_, err := fmt.Fprintln(w, render(indentBlock(f.Old, pad)))
		return err
	default:
		if _, err := fmt.Fprintln(w, render(indentBlock(f.Old, pad+"- "))); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, render(indentBlock(f.New, pad+"+ ")))
		return err
	}
}

// writeYAMLLeaf renders one leaf field: "key: old -> new" (or the drift/
// masked/added/removed variant), falling back to an indented block when
// the value itself is multi-line (a whole map/list replaced as a scalar
// field's value, e.g. a ConfigMap's whole data entry).
func writeYAMLLeaf(w io.Writer, indent int, key string, f FieldDiff, opts TextOptions, style yamlLeafStyle) error {
	pad := strings.Repeat("  ", indent)

	if f.Masked && !opts.ShowSecrets {
		line := opts.Theme.Style(theme.TextMuted).Render(fmt.Sprintf("%s%s: (masked) %s", pad, key, f.ChangeKind))
		_, err := fmt.Fprintln(w, line)
		return err
	}

	if strings.Contains(f.Old, "\n") || strings.Contains(f.New, "\n") {
		if _, err := fmt.Fprintf(w, "%s%s:\n", pad, key); err != nil {
			return err
		}
		return writeYAMLValueBlock(w, indent+1, f, opts, style)
	}

	token := fieldToken(f.ChangeKind)
	if style == yamlLeafStyleDrift {
		token = theme.StatusWarning
	}
	render := opts.Theme.Style(token).Render

	if style == yamlLeafStyleDrift {
		switch f.ChangeKind {
		case FieldAdded:
			_, err := fmt.Fprintln(w, render(fmt.Sprintf("%s%s: (only on cluster) %s", pad, key, f.New)))
			return err
		case FieldRemoved:
			_, err := fmt.Fprintln(w, render(fmt.Sprintf("%s%s: (missing on cluster, expected %s)", pad, key, f.Old)))
			return err
		default:
			_, err := fmt.Fprintln(w, render(fmt.Sprintf("%s%s: expected %s, live %s", pad, key, f.Old, f.New)))
			return err
		}
	}

	switch f.ChangeKind {
	case FieldAdded:
		_, err := fmt.Fprintln(w, render(fmt.Sprintf("%s%s: (added) %s", pad, key, f.New)))
		return err
	case FieldRemoved:
		_, err := fmt.Fprintln(w, render(fmt.Sprintf("%s%s: (removed) %s", pad, key, f.Old)))
		return err
	default:
		_, err := fmt.Fprintln(w, render(fmt.Sprintf("%s%s: %s -> %s", pad, key, f.Old, f.New)))
		return err
	}
}
