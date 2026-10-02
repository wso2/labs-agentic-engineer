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

// Moved from services/aep-api/internal/spec/design_scaffold.go and
// cell_facts.go (the facts the scaffold reads); the aep-api copies are
// deleted in phase 4.

package files

// The scaffold engine: the cell is the primary design source, so the moment
// a save lands specs/design/design.cell, the platform derives a design.json
// SKELETON for every deployable component the cell declares that has none
// yet, in the same commit. The agent then only ENRICHES (language, stories,
// dependencies, description, skillsPinned); it never authors the mechanical
// fields. Skeletons satisfy the design write-gate outright, so a
// scaffolded-but-unenriched component is valid on disk and the build-tag gate
// is what demands enrichment.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DesignCellPath is the project-level cell file the scaffold keys off.
const DesignCellPath = "specs/design/design.cell"

// deployableCellTypes maps a cell node type to the design.json component type
// it scaffolds. Nodes typed otherwise (database, cache, identity-server, …)
// are dependencies of components, never component directories.
var deployableCellTypes = map[string]string{
	"service":         "service",
	"web-application": "web-application",
	"web-app":         "web-application",
	"worker":          "worker",
	"scheduled-task":  "scheduled-task",
}

// scaffoldLanguageSentinel is what the scaffold writes for `language`: a
// non-empty value (the schema demands one) that the build-tag gate REFUSES.
// Language is a judgment call the platform never makes.
const scaffoldLanguageSentinel = "TBD"

// scaffoldExposureByType is the safe mechanical default per type; enrichable.
var scaffoldExposureByType = map[string]string{
	"service":         "intranet",
	"web-application": "internet",
	"worker":          "intranet",
	"scheduled-task":  "intranet",
}

//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func componentDesignPath(id string) string {
	return "specs/design/components/" + id + "/design.json"
}

// scaffoldFromCell derives the missing design.json skeletons for a
// design.cell source. `exists` answers whether a repo path is already present
// (committed tree or the same batch). A cell that fails fact extraction
// scaffolds nothing: the TS grammar validator owns surfacing the error.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func scaffoldFromCell(cellSource string, exists func(path string) bool) map[string]string {
	components, err := cellComponents(cellSource)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, c := range components {
		componentType, deployable := deployableCellTypes[strings.ToLower(strings.TrimSpace(c.Type))]
		if !deployable || c.ID == "" {
			continue
		}
		path := componentDesignPath(c.ID)
		if exists(path) {
			continue
		}
		out[path] = renderScaffold(c.ID, componentType)
	}
	return out
}

// renderScaffold emits a skeleton that passes the strict design write-gate.
// Key order is stable (marshal of an ordered struct) so scaffolds are
// byte-deterministic. `stories` is deliberately absent: the agent authors it
// during enrichment.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func renderScaffold(id, componentType string) string {
	skeleton := struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		Version      string `json:"version"`
		Language     string `json:"language"`
		Buildpack    string `json:"buildpack"`
		AppPath      string `json:"appPath"`
		Entrypoint   string `json:"entrypoint"`
		Exposure     string `json:"exposure"`
		Dependencies []any  `json:"dependencies"`
		Description  string `json:"description"`
	}{
		Name:         id,
		Type:         componentType,
		Version:      "0.1.0",
		Language:     scaffoldLanguageSentinel,
		Buildpack:    "docker",
		AppPath:      id,
		Entrypoint:   "deployment/" + componentType,
		Exposure:     scaffoldExposureByType[componentType],
		Dependencies: []any{},
		Description:  "Scaffolded from design.cell — enrich with this component's responsibility, language (org Tech stack default first), the PRD stories it serves, dependencies, and pinned skills.",
	}
	b, _ := json.MarshalIndent(skeleton, "", "  ")
	return string(b) + "\n"
}

// sortedPaths returns a path map's keys in stable order (deterministic commit
// contents and result metas).
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func sortedPaths(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// cellComponent is one `component` statement of a design.cell.
type cellComponent struct {
	ID   string
	Type string
}

// cellComponents extracts the `component` statements of a design.cell. The
// extractor is deliberately permissive (the TS parser is the authoritative
// grammar validator): statements it does not recognize are skipped. A leading
// `---` frontmatter block is skipped; an unterminated one is an error, never
// scanned through, so component-looking lines inside it never become facts.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func cellComponents(source string) ([]cellComponent, error) {
	body, err := cellBody(source)
	if err != nil {
		return nil, fmt.Errorf("design.cell frontmatter: %w", err)
	}
	// Line numbers in diagnostics count from the top of the file.
	offset := strings.Count(source[:len(source)-len(body)], "\n")
	var out []cellComponent
	for i, rawLine := range strings.Split(body, "\n") {
		statement := strings.TrimSpace(rawLine)
		if statement == "" || strings.HasPrefix(statement, "#") || strings.HasPrefix(statement, "//") {
			continue
		}
		if strings.HasPrefix(statement, "component ") {
			c, err := parseCellComponent(statement, offset+i+1)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func parseCellComponent(statement string, line int) (cellComponent, error) {
	tokens := tokenizeCellStatement(statement)
	if len(tokens) < 2 {
		return cellComponent{}, fmt.Errorf("design.cell line %d: component statement needs an id", line)
	}
	c := cellComponent{ID: tokens[1]}
	rest := tokens[2:]
	if len(rest) > 0 {
		if rest[0] == "as" {
			// With `as`, the LAST token is the type when two or more follow
			// the label start; a single trailing token is label-only.
			if len(rest) >= 3 {
				c.Type = rest[len(rest)-1]
			}
		} else {
			c.Type = strings.Join(rest, " ")
		}
	}
	return c, nil
}

// cellBody returns the suffix of source after a leading `---` YAML
// frontmatter block, the same fence rule as the TS grammar's
// stripFrontmatter. An optional UTF-8 BOM and leading whitespace are allowed
// before the fence.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func cellBody(source string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimLeft(source, " \t\r\n"), "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return source, nil
	}
	rest := strings.TrimLeft(trimmed[3:], " \t")
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return source, nil
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", errors.New("unterminated --- block")
	}
	after := strings.TrimPrefix(rest[end+len("\n---"):], "\r")
	return strings.TrimPrefix(after, "\n"), nil
}

// cellTokenPattern splits on whitespace, keeping double-quoted runs as one
// token (mirrors the TS tokenizer).
var cellTokenPattern = regexp.MustCompile(`"([^"]*)"|(\S+)`)

//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
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
