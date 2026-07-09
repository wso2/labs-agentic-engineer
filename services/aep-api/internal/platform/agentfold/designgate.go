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

package agentfold

// designgate.go — the component design.json write-gate, an EXACT
// accept/reject port of packages/agent-stream/src/component-design-schema.ts
// checkComponentDesign (the zod gate the TS FileBundle runs on every write).
//
// Deliberately NOT delegated to internal/platform/designspec: for the fold,
// parity with the agent's gate wins — a write the agent applied must fold
// here too, or the manifest check fails a healthy turn. The historic
// divergence (zod accepted unknown CONNECTION properties while the published
// component-design.schema.json pins `additionalProperties: false`) was
// RECONCILED in Phase 5: the zod gate is strict now, and this port matches.
// Messages are designspec-flavored; message text is log-only.

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// componentDesignRe is COMPONENT_DESIGN_JSON_RE.
var componentDesignRe = regexp.MustCompile(`^specs/design/components/([^/]+)/design\.json$`)

type designProblem struct {
	code    ErrCode
	message string
}

var (
	designStringFields  = []string{"name", "type", "version", "language", "buildpack", "appPath", "entrypoint", "description"}
	exposureValues      = map[string]bool{"internet": true, "intranet": true}
	dependencyKinds     = map[string]bool{"component": true, "org-service": true, "external": true, "platform-resource": true}
	dependencyKnownKeys = map[string]bool{
		"kind": true, "name": true, "description": true, "needsSpec": true,
		"specPath": true, "specUrl": true, "config": true, "resourceType": true,
		"parameters": true, "candidates": true,
	}
	designKnownKeys = map[string]bool{
		"name": true, "type": true, "version": true, "language": true,
		"buildpack": true, "appPath": true, "entrypoint": true,
		"exposure": true, "dependencies": true, "description": true,
		"exposesAPI": true, "componentAgentInstructions": true,
	}
)

// validateComponentDesign mirrors the zod componentDesignSchema.safeParse
// (dependencies[] — the kind-discriminated successor to the legacy connections[])
// plus the name-equals-directory rule. Only the FIRST problem is reported (the
// accept/reject outcome is what parity needs). It is deliberately AT MOST as
// strict as the TS zod gate (which the FileBundle runs first), so a zod-passing
// write always folds here: the top-level shape + each dependency's kind/name and
// strict-key set are enforced; the optional platform-owned blocks (exposesAPI /
// componentAgentInstructions) are type-checked only.
func validateComponentDesign(content, dirName string) *designProblem {
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &designProblem{code: ErrInvalidJSON, message: "content is not valid JSON: " + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "must be an object"}
	}
	// z.strictObject: unknown TOP-LEVEL properties reject.
	for k := range obj {
		if !designKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "unknown property " + k}
		}
	}
	for _, field := range designStringFields {
		s, ok := obj[field].(string)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: field + ": must be a string"}
		}
		if s == "" {
			return &designProblem{code: ErrSchemaViolation, message: field + ": must be at least 1 characters"}
		}
	}
	exposure, ok := obj["exposure"].(string)
	if !ok || !exposureValues[exposure] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("exposure: %q is not an allowed value", obj["exposure"])}
	}
	deps, ok := obj["dependencies"].([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "dependencies: must be an array"}
	}
	for i, d := range deps {
		if p := validateDependency(i, d); p != nil {
			return p
		}
	}
	if name := obj["name"].(string); name != dirName {
		return &designProblem{
			code:    ErrSchemaViolation,
			message: fmt.Sprintf("name %q must equal the component directory name %q", name, dirName),
		}
	}
	return nil
}

// validateDependency mirrors the zod dependencySchema.strictObject: a
// kind-discriminated edge whose kind + name are required and whose keys are a
// closed set (unknown keys — notably the read-time-computed status/reason, which
// the agent must NEVER author — reject).
func validateDependency(i int, d any) *designProblem {
	dep, ok := d.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d]: must be an object", i)}
	}
	for k := range dep {
		if !dependencyKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d]: unknown property %s", i, k)}
		}
	}
	kind, ok := dep["kind"].(string)
	if !ok || !dependencyKinds[kind] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].kind: %q is not an allowed value", i, dep["kind"])}
	}
	name, ok := dep["name"].(string)
	if !ok || name == "" {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("dependencies[%d].name: must be a non-empty string", i)}
	}
	return nil
}
