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

// Command gendocs generates the Deployah CLI reference as Markdown, one file per
// command, under docs/cli. It wraps Cobra's Markdown generator and post-processes
// the output so the files pass the repository's markdownlint rules: fenced code
// blocks get a language (MD040), tabs outside code become spaces (MD010), and
// repeated blank lines are collapsed (MD012).
//
// It is the source of truth for the command reference. Run it with
// `nix run .#gen-docs` (or `go generate ./...`) after changing any command, flag,
// or description.
package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"deployah.dev/deployah/internal/cmd"
)

// outDir is the output directory, relative to the repository root.
const outDir = "docs/cli"

var multipleBlankLines = regexp.MustCompile(`\n{3,}`)

func main() {
	root := cmd.NewApp().UnsafeRoot()

	// Disable the "Auto generated ... on <date>" footer on every command so the
	// output is reproducible and the CI drift check stays stable.
	disableAutoGenTag(root)

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		log.Fatalf("gendocs: create %s: %v", outDir, err)
	}
	if err := genTree(root); err != nil {
		log.Fatalf("gendocs: %v", err)
	}
}

// genTree writes a Markdown file for cmd and every visible descendant, mirroring
// cobra/doc.GenMarkdownTree but routing each page through lintClean first.
func genTree(c *cobra.Command) error {
	for _, sub := range c.Commands() {
		if !sub.IsAvailableCommand() || sub.IsAdditionalHelpTopicCommand() {
			continue
		}
		if err := genTree(sub); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	// The identity link handler keeps the relative "<command>.md" links between
	// pages, the same as GenMarkdownTree.
	if err := doc.GenMarkdownCustom(c, &buf, func(s string) string { return s }); err != nil {
		return err
	}

	name := strings.ReplaceAll(c.CommandPath(), " ", "_") + ".md"
	path := filepath.Join(outDir, name)
	// #nosec G304 -- path is built from the static command tree under a constant dir.
	return os.WriteFile(path, []byte(lintClean(buf.String())), 0o600)
}

// lintClean rewrites Cobra's Markdown so it satisfies the repository's
// markdownlint config.
func lintClean(s string) string {
	var b strings.Builder
	inCode := false
	for line := range strings.SplitSeq(s, "\n") {
		if line == "```" {
			if inCode {
				b.WriteString("```\n")
			} else {
				// MD040: fenced code blocks must declare a language.
				b.WriteString("```text\n")
			}
			inCode = !inCode
			continue
		}
		if !inCode {
			// MD010: no hard tabs outside code blocks (the "SEE ALSO" lines).
			line = strings.ReplaceAll(line, "\t", " ")
			line = strings.TrimRight(line, " ")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// MD012: collapse runs of blank lines, and end with a single newline (MD047).
	out := multipleBlankLines.ReplaceAllString(b.String(), "\n\n")
	return strings.TrimRight(out, "\n") + "\n"
}

// disableAutoGenTag turns off the timestamp footer for a command and all of its
// descendants.
func disableAutoGenTag(c *cobra.Command) {
	c.DisableAutoGenTag = true
	for _, sub := range c.Commands() {
		disableAutoGenTag(sub)
	}
}
