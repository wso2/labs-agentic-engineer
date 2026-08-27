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

// Package designspec validates a component design.json against the ONE
// published JSON Schema definition (docs/design/agents-generation-migration.md
// §8): packages/contracts/schemas/component-design.schema.json, mirrored here as
// a vendored embed because go:embed cannot cross the aep-api Go module boundary.
// The agent's FileBundle write-gate (services/agents) and this Go validator both
// key off that single file, so BFF and agent never drift into two hand-kept
// copies.
//
// The validation error codes mirror the agent's checkComponentDesign:
// INVALID_JSON (unparseable) and SCHEMA_VIOLATION (fails the schema, or the
// name != component-directory rule the schema itself cannot express).
package designspec

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

//go:embed component-design.schema.json
var schemaJSON []byte

// Error codes (mirroring the agent's write gate).
const (
	CodeInvalidJSON     = "INVALID_JSON"
	CodeSchemaViolation = "SCHEMA_VIOLATION"
)

// ValidationError carries a stable code + human message for a rejected
// component design.json.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Code + ": " + e.Message }

// componentSchema is the parsed embedded schema, loaded once at init. The schema
// is small and fixed, so a package-level parse is fine.
var componentSchema = jsonschema.MustParse(schemaJSON)

// ValidateComponentDesign checks raw component design.json bytes against the
// embedded schema. Returns nil when valid, or a *ValidationError.
func ValidateComponentDesign(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return &ValidationError{Code: CodeInvalidJSON, Message: "content is not valid JSON: " + err.Error()}
	}
	if msgs := jsonschema.Validate(v, componentSchema); len(msgs) > 0 {
		return &ValidationError{Code: CodeSchemaViolation, Message: msgs[0]}
	}
	return nil
}

// ValidateComponentDesignInDir additionally enforces the rule the schema cannot
// express: the design.json `name` must equal the component directory name
// (mirrors the agent's checkComponentDesign). dirName is the <name> segment of
// specs/design/components/<name>/design.json.
func ValidateComponentDesignInDir(raw []byte, dirName string) error {
	if err := ValidateComponentDesign(raw); err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return &ValidationError{Code: CodeInvalidJSON, Message: err.Error()}
	}
	name, _ := obj["name"].(string)
	if name != dirName {
		return &ValidationError{
			Code:    CodeSchemaViolation,
			Message: fmt.Sprintf("name %q must equal the component directory name %q", name, dirName),
		}
	}
	return nil
}
