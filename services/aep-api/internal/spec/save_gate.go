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

import (
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/designspec"
	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// The hard save gate (docs/design/agents-generation-migration.md §8). Save is
// the ONE point where nothing malformed may acquire a tag — the tag is the only
// state downstream (tasks, OC provisioning) trusts. Author-time (agent write
// gate) and files:apply soft warnings are advisory; this is the enforcement.

// FileValidationError is one per-file save-gate failure. Code mirrors the
// agent's write-gate codes (designspec.CodeInvalidJSON / CodeSchemaViolation)
// plus the layout/OpenAPI codes below, so the console renders one vocabulary.
type FileValidationError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DesignValidationError is the aggregate hard-gate rejection for a design save:
// one or more component design.json / OpenAPI / layout violations. The handler
// renders it as a 422 with per-file detail; nothing is tagged.
type DesignValidationError struct {
	Files []FileValidationError
}

func (e *DesignValidationError) Error() string {
	if len(e.Files) == 0 {
		return "design validation failed"
	}
	parts := make([]string, 0, len(e.Files))
	for _, f := range e.Files {
		parts = append(parts, fmt.Sprintf("%s: %s: %s", f.Path, f.Code, f.Message))
	}
	return "design validation failed: " + strings.Join(parts, "; ")
}

// Save-gate error codes not owned by designspec.
const (
	// codeInvalidOpenAPI — a component openapi.yaml does not parse as YAML.
	codeInvalidOpenAPI = "INVALID_OPENAPI"
	// codeInvalidFrontmatter — the root design.cell frontmatter block does
	// not parse.
	codeInvalidFrontmatter = "INVALID_FRONTMATTER"
)

// validateDesignBundle is the design hard gate (§8). It runs on the HEAD design
// bundle (keys relative to specs/design/) before a tag is cut:
//
//   - layout: the root design.cell must exist (a bundle with no root can't be
//     assembled into a design);
//   - frontmatter: the root design.cell frontmatter must parse (surfaced by
//     AssembleDesign);
//   - component design.json: every present design.json validates against the
//     single published schema + the name==dir rule (designspec — the same
//     definition the agent's write gate uses);
//   - security.json, when present: the security design validates against the
//     same published schema and referential rules the agent's write gate
//     applies;
//   - OpenAPI: every present component openapi.yaml/yml must parse.
//
// A missing root is ErrArtifactPathInvalid (400). Any other failure aggregates
// into a *DesignValidationError (422) carrying per-file detail. Nothing
// malformed acquires a tag.
func validateDesignBundle(files map[string]string) error {
	if strings.TrimSpace(files[designRootFile]) == "" {
		return fmt.Errorf("%w: %s/%s missing — generate design before saving",
			ErrArtifactPathInvalid, DesignDir, designRootFile)
	}

	var verrs []FileValidationError

	// Root design.cell frontmatter parseability + layout shape.
	if _, err := AssembleDesign(files); err != nil {
		verrs = append(verrs, FileValidationError{
			Path: designRootFile, Code: codeInvalidFrontmatter, Message: err.Error(),
		})
	}

	// Component design.json schema + name==dir (validate every one present).
	for _, name := range ComponentNamesIn(files) {
		key := componentDirPrefix + name + "/design.json"
		content, ok := files[key]
		if !ok {
			continue
		}
		if err := designspec.ValidateComponentDesignInDir([]byte(content), name); err != nil {
			var ve *designspec.ValidationError
			if errors.As(err, &ve) {
				verrs = append(verrs, FileValidationError{Path: key, Code: ve.Code, Message: ve.Message})
			} else {
				verrs = append(verrs, FileValidationError{Path: key, Code: designspec.CodeSchemaViolation, Message: err.Error()})
			}
		}
	}

	// The security.json document, when present: schema + the referential rules
	// (every test user's role declared, coldStartRole declared or null). Same
	// single definition the agent's write gate uses, so a document that passes
	// one gate passes the other.
	if raw, ok := files[securityspec.BundleKey]; ok && strings.TrimSpace(raw) != "" {
		if _, err := securityspec.Parse([]byte(raw)); err != nil {
			var ve *securityspec.ValidationError
			code := securityspec.CodeSchemaViolation
			msg := err.Error()
			if errors.As(err, &ve) {
				code, msg = ve.Code, ve.Message
			}
			verrs = append(verrs, FileValidationError{Path: securityspec.BundleKey, Code: code, Message: msg})
		}
	}

	// OpenAPI parseability (component openapi.yaml/.yml).
	for rel, content := range files {
		if !strings.HasSuffix(rel, "/openapi.yaml") && !strings.HasSuffix(rel, "/openapi.yml") {
			continue
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		if _, err := NormalizeOpenAPIYAML(content); err != nil {
			verrs = append(verrs, FileValidationError{Path: rel, Code: codeInvalidOpenAPI, Message: err.Error()})
		}
	}

	if len(verrs) > 0 {
		return &DesignValidationError{Files: verrs}
	}
	return nil
}
