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

// Package prototypespec is the platform's static check of a web-application's
// prototype: its manifest `specs/design/components/<component>/prototype.json`
// (schema version 3, the roles, display states, screens and flows the review
// bar offers) and its screens `prototype.tsx` beside it, a React module over
// @wso2/prototype-kit (ADR-0042).
//
// It is the sibling of securityspec, and for the same reason: the manifest's
// single definition is packages/prototype-kit/schema/prototype-manifest.schema.json,
// generated from the kit's Zod schema (the one the agent's write gate parses
// with) and vendored here as an embed because go:embed cannot cross the
// aep-api module boundary. Agent and save gate therefore judge ONE manifest
// shape.
//
// Beyond the schema, this package owns what a standalone JSON Schema cannot
// express (references.go): one id namespace and every reference resolving. The
// codes, locations and messages are a contract with the kit's
// `referenceFindings`: packages/prototype-kit/test/fixtures/manifest-cases.json
// is the table both sides assert.
//
// The screens get a static floor (source.go): they parse, import only react and
// the kit, and fit the size cap. Drawing them executes generated code, which
// the Go gate never does; the agent's write gate does it in isolation, and the
// console only inside a sandboxed frame.
package prototypespec

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

//go:embed prototype-manifest.schema.json
var schemaJSON []byte

var manifestSchema = jsonschema.MustParse(schemaJSON)

// SchemaVersion is the only manifest version this platform reads.
const SchemaVersion = 3

// Finding codes, identical to the kit's FINDING_CODES for the checks this
// package makes.
const (
	CodeSchemaViolation    = "SCHEMA_VIOLATION"
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION"
	CodeDuplicateID        = "DUPLICATE_ID"
	CodeUnknownReference   = "UNKNOWN_REFERENCE"
	CodeSyntaxError        = "SYNTAX_ERROR"
	CodeForbiddenImport    = "FORBIDDEN_IMPORT"
	CodeSourceTooLarge     = "SOURCE_TOO_LARGE"
)

// ManifestPath is the design-bundle key (relative to specs/design/) of a
// component's prototype manifest.
func ManifestPath(component string) string { return "components/" + component + "/prototype.json" }

// SourcePath is the design-bundle key of a component's prototype source.
func SourcePath(component string) string { return "components/" + component + "/prototype.tsx" }

// Finding is one problem: a stable code, where (a JSON path spelled
// `flows[0].screenIds[1]` in the manifest, `line 12` in the source, `(root)` or
// `(file)` for the whole file) and what to change.
type Finding struct {
	Code     string
	Location string
	Message  string
}

// Text is the finding as one line, led by its place, the way a per-file save
// failure reports it.
func (f Finding) Text() string { return f.Location + ": " + f.Message }

// Manifest is a parsed prototype.json, version 3.
type Manifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	Name          string         `json:"name"`
	EntryScreen   string         `json:"entryScreen"`
	Roles         []Role         `json:"roles"`
	States        []DisplayState `json:"states"`
	Screens       []Screen       `json:"screens"`
	Flows         []Flow         `json:"flows"`
}

// Role is a role the reviewer can view the prototype as.
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DisplayState is a named presentation (default, empty, failed, …) a reviewer
// can switch to.
type DisplayState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Screen is one page of the application: its id, its name and who reaches it.
type Screen struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	RoleIDs []string `json:"roleIds"`
}

// Flow is a named path through the application, walked as one role.
type Flow struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	RoleID    string   `json:"roleId"`
	ScreenIDs []string `json:"screenIds"`
}

// ParseManifest validates raw prototype.json bytes and returns the manifest, or
// the findings that refuse it. It runs the kit's stages and stops at the first
// that fails: JSON, the version, the embedded schema, then the references.
func ParseManifest(raw []byte) (*Manifest, []Finding) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, []Finding{{
			Code:     CodeSchemaViolation,
			Location: "(root)",
			Message:  "prototype.json is not valid JSON: " + err.Error(),
		}}
	}
	if f := unsupportedVersion(v); f != nil {
		return nil, []Finding{*f}
	}
	// The interpreter stops at the first failure, so one finding: the kit lists
	// every violation, and the codes agree.
	if msgs := jsonschema.Validate(v, manifestSchema); len(msgs) > 0 {
		return nil, []Finding{{Code: CodeSchemaViolation, Location: "(root)", Message: msgs[0]}}
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, []Finding{{Code: CodeSchemaViolation, Location: "(root)", Message: err.Error()}}
	}
	if findings := referenceFindings(&manifest); len(findings) > 0 {
		return nil, findings
	}
	return &manifest, nil
}

// unsupportedVersion refuses a document that SAYS it is another version in one
// finding, before the schema reports a const mismatch plus whatever else that
// version does differently.
func unsupportedVersion(v any) *Finding {
	doc, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	version, present := doc["schemaVersion"]
	if !present {
		return nil
	}
	if n, ok := version.(float64); ok && n == SchemaVersion {
		return nil
	}
	return &Finding{
		Code:     CodeUnsupportedVersion,
		Location: "schemaVersion",
		Message:  fmt.Sprintf("schemaVersion %s is not supported; write schemaVersion %d", quote(version), SchemaVersion),
	}
}

// quote spells v as JSON, the way the kit's messages do (JSON.stringify):
// quotes around a string, no HTML escaping.
func quote(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return string(bytes.TrimRight(b.Bytes(), "\n"))
}
