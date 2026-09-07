// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package spec

// cell_facts.go — the BFF's read of specs/design/design.cell. The cell is the
// PRIMARY design source: it declares the components the design ships. The
// scaffold engine and the build-tag gate consume these FACTS — component ids
// and types — not the diagram semantics; the TS parser
// (packages/ui/cell-diagram-react) stays the authoritative grammar validator
// for rendering, so this extractor is deliberately permissive: statements it
// does not recognize are skipped, and only the facts it DOES extract are
// validated.

import (
	"fmt"
	"regexp"
	"strings"
)

// CellComponent is one `component` statement's extracted facts.
type CellComponent struct {
	ID   string
	Type string
}

// CellFacts is everything the platform reads from a design.cell.
type CellFacts struct {
	Components []CellComponent
}

// parseCellFacts extracts the platform-relevant facts from design.cell source.
// A leading `---` YAML frontmatter block (the root's sourceSpec carrier — see
// DesignRootFile) is skipped: it is AssembleDesign's business, not a cell fact.
// A MALFORMED block (unterminated `---`) is an error, never scanned through —
// component-looking lines inside it must not become facts the scaffold or the
// build scope act on.
func parseCellFacts(source string) (*CellFacts, error) {
	body, err := stripCellFrontmatter(source)
	if err != nil {
		return nil, fmt.Errorf("design.cell frontmatter: %w", err)
	}
	facts := &CellFacts{}
	for i, rawLine := range strings.Split(body, "\n") {
		line := i + 1
		statement := strings.TrimSpace(rawLine)
		if statement == "" || strings.HasPrefix(statement, "#") || strings.HasPrefix(statement, "//") {
			continue
		}

		if strings.HasPrefix(statement, "component ") {
			component, err := parseCellComponent(statement, line)
			if err != nil {
				return nil, err
			}
			facts.Components = append(facts.Components, component)
			continue
		}
		// Everything else (title, version, externals, edges, unknown lines) is
		// the TS validator's business, not a fact this extractor needs.
	}
	return facts, nil
}

func parseCellComponent(statement string, line int) (CellComponent, error) {
	tokens := tokenizeCellStatement(statement)
	if len(tokens) < 2 {
		return CellComponent{}, fmt.Errorf("design.cell line %d: component statement needs an id", line)
	}
	component := CellComponent{ID: tokens[1]}
	rest := tokens[2:]
	if len(rest) > 0 {
		if rest[0] == "as" {
			// With `as`, the LAST token is the type when two or more follow the
			// label start; a single trailing token is label-only (TS grammar).
			if len(rest) >= 3 {
				component.Type = rest[len(rest)-1]
			}
		} else {
			component.Type = strings.Join(rest, " ")
		}
	}
	return component, nil
}

// stripCellFrontmatter blanks a leading `---` YAML frontmatter block so the
// statements below it keep their line numbers in diagnostics — the same fence
// rule the TS grammar's stripFrontmatter applies. A malformed (unterminated)
// block is the caller's error to surface, never scanned through.
func stripCellFrontmatter(source string) (string, error) {
	_, body, err := SplitFrontmatter(source)
	if err != nil {
		return "", err
	}
	// Preserve line count: replace every frontmatter line with a blank one.
	cut := len(source) - len(body)
	return strings.Repeat("\n", strings.Count(source[:cut], "\n")) + body, nil
}

// tokenizeCellStatement splits on whitespace, keeping double-quoted runs as
// one token (mirrors the TS tokenizer).
var cellTokenPattern = regexp.MustCompile(`"([^"]*)"|(\S+)`)

func tokenizeCellStatement(statement string) []string {
	matches := cellTokenPattern.FindAllStringSubmatch(statement, -1)
	tokens := make([]string, 0, len(matches))
	for _, m := range matches {
		if m[1] != "" || strings.HasPrefix(m[0], `"`) {
			tokens = append(tokens, m[1])
		} else {
			tokens = append(tokens, m[2])
		}
	}
	return tokens
}
