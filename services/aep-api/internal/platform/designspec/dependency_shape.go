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

// dependency_shape.go — the dependency.json / sdk.json shape rules the JSON
// schema cannot say, the save gate's twin of the agent's zod gate
// (packages/agent-stream/src/dependency-design-schema.ts
// `checkDependencyDesign`): name equals directory, the resource block's name
// equals it too, suggestions and a provider never coexist, config keys follow
// a chosen provider, the contract path fits its type. The two must agree, or a
// file the agent writes could never be tagged. The platform-only rules (`ref`,
// `consumptionInstructions`, `accepted` are the platform's to write) are the
// agent's write-time concern: at save the file is its own record, whoever
// wrote it.

package designspec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// shapeProblem is one shape-rule violation.
type shapeProblem struct {
	code    string
	message string
}

// CheckDependencyFile checks a dependency's definition
// (specs/design/dependencies/<name>/dependency.json) or SDK manifest
// (…/sdk.json) against the shape rules. Nil when path is neither or the
// content passes.
func CheckDependencyFile(path, content string) *ValidationError {
	if sdkManifestRe.MatchString(path) {
		if p := validateSdkManifest(content); p != nil {
			return &ValidationError{Code: p.code, Message: path + ": " + p.message}
		}
		return nil
	}
	m := dependencyDesignRe.FindStringSubmatch(path)
	if m == nil {
		return nil
	}
	if p := validateDependencyDesign(content, m[1]); p != nil {
		return &ValidationError{Code: p.code, Message: path + ": " + p.message}
	}
	return nil
}

var (
	// dependencyDesignRe is DEPENDENCY_DESIGN_JSON_RE; sdkManifestRe is SDK_MANIFEST_JSON_RE.
	dependencyDesignRe = regexp.MustCompile(`^specs/design/dependencies/([^/]+)/dependency\.json$`)
	sdkManifestRe      = regexp.MustCompile(`^specs/design/dependencies/([^/]+)/sdk\.json$`)
	sha256HexRe        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	packageRefRe       = regexp.MustCompile(`^[a-z][a-z0-9-]*:.+`)

	dependencyStyles         = map[string]bool{"rest-api": true, "graphql": true, "sdk": true}
	dependencyDefinitionKeys = map[string]bool{"name": true, "resource": true, "provenance": true, "suggestions": true}
	resourceKeys             = map[string]bool{"ref": true, "name": true, "description": true, "provider": true, "config": true, "contract": true, "consumptionInstructions": true, "provenance": true}
	contractKeys             = map[string]bool{"type": true, "path": true, "origin": true, "accepted": true}
	contractTypes            = map[string]bool{"openapi": true, "graphql": true, "sdk": true}
	contractOrigins          = map[string]bool{"registry": true, "provider": true, "derived": true, "assumed": true}
	provenanceKeys           = map[string]bool{"sourceUrl": true, "registry": true, "sha256": true, "readOn": true}
	assumptionKeys           = map[string]bool{"by": true, "at": true, "note": true}
	suggestionKeys           = map[string]bool{"name": true, "style": true, "description": true}
	configKeyKeys            = map[string]bool{"key": true, "secret": true, "description": true, "defaultValue": true}
	sdkManifestKeys          = map[string]bool{"packages": true, "docsUrl": true, "calls": true, "derived": true, "assumed": true}
	contractFilesByType      = map[string][]string{"openapi": {"openapi.yaml", "openapi.yml", "openapi.json"}, "graphql": {"schema.graphql", "schema.graphqls"}, "sdk": {"sdk.json"}}
	// retiredDefinitionKeys are the previous flat shape's top-level fields.
	// Naming one is the one mistake a model trained on the old shape makes,
	// so it gets its own message.
	retiredDefinitionKeys = map[string]bool{"source": true, "provider": true, "style": true, "contract": true, "sdk": true, "config": true, "assumed": true, "candidates": true, "description": true}
)

func validateDependencyDesign(content, dirName string) *shapeProblem {
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &shapeProblem{code: CodeInvalidJSON, message: "content is not valid JSON: " + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "must be an object"}
	}
	for k := range obj {
		if !dependencyDefinitionKeys[k] {
			if retiredDefinitionKeys[k] {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`%q is not a top-level field any more — the resource's own fields (name, description, provider, config, contract) live in the "resource" block; style is not stored (the contract's type says it); "source" is the presence of "resource.ref"; the acceptance record is "resource.contract.accepted".`, k)}
			}
			return &shapeProblem{code: CodeSchemaViolation, message: "unknown property " + k}
		}
	}
	name, ok := obj["name"].(string)
	if !ok || name == "" {
		return &shapeProblem{code: CodeSchemaViolation, message: "name: must be a non-empty string"}
	}
	if name != dirName {
		return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("\"name\" must equal the dependency directory (%q), got %q.", dirName, name)}
	}
	resV, hasRes := obj["resource"]
	if !hasRes {
		return &shapeProblem{code: CodeSchemaViolation, message: `"resource" is required — the block that says what the thing is (name, description; provider, config and contract once a service is chosen).`}
	}
	res, ok := resV.(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "resource: must be an object"}
	}
	for k := range res {
		if !resourceKeys[k] {
			return &shapeProblem{code: CodeSchemaViolation, message: "resource: unknown property " + k}
		}
	}
	for _, f := range []string{"ref", "name", "description", "provider", "consumptionInstructions"} {
		if v, present := res[f]; present {
			s, ok := v.(string)
			if !ok || (f != "description" && f != "consumptionInstructions" && s == "") {
				return &shapeProblem{code: CodeSchemaViolation, message: "resource." + f + ": must be a non-empty string"}
			}
		}
	}
	resName, _ := res["name"].(string)
	if resName == "" {
		return &shapeProblem{code: CodeSchemaViolation, message: "resource.name: must be a non-empty string"}
	}
	if resName != name {
		return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`"resource.name" must equal the dependency name (%q), got %q.`, name, resName)}
	}
	ref, _ := res["ref"].(string)
	if ref != "" && ref != name {
		return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`"resource.ref" must equal the dependency name (%q) — a registered resource is always used under its own name; got %q.`, name, ref)}
	}
	if p := validateProvenance(obj["provenance"], "provenance"); p != nil {
		return p
	}
	if p := validateProvenance(res["provenance"], "resource.provenance"); p != nil {
		return p
	}
	// zod's `.optional()` admits an absent field, never an explicit null; the
	// fold must refuse the same nulls or a write would fold here that the
	// agent's own gate had rejected.
	for _, f := range []string{"provenance", "suggestions"} {
		if v, present := obj[f]; present && v == nil {
			return &shapeProblem{code: CodeSchemaViolation, message: f + ": must not be null — omit the field instead"}
		}
	}
	for _, f := range []string{"provenance", "contract", "config"} {
		if v, present := res[f]; present && v == nil {
			return &shapeProblem{code: CodeSchemaViolation, message: "resource." + f + ": must not be null — omit the field instead"}
		}
	}
	suggestions, hasSuggestions := obj["suggestions"]
	if hasSuggestions {
		if p := validateSuggestions(suggestions); p != nil {
			return p
		}
	}
	provider, _ := res["provider"].(string)
	if cfg, present := res["config"]; present {
		if p := validateConfigKeys(cfg); p != nil {
			return p
		}
		if list, ok := cfg.([]any); ok && len(list) > 0 && provider == "" && ref == "" {
			return &shapeProblem{code: CodeSchemaViolation, message: `"resource.config" is derived from the chosen service and is written only once "resource.provider" is set — leave it out until the user has chosen; the resolve flow derives the keys.`}
		}
	}
	contractV, hasContract := res["contract"]
	if hasSuggestions && provider != "" {
		return &shapeProblem{code: CodeSchemaViolation, message: `"suggestions" and "resource.provider" never coexist — once the user chose a service, REMOVE suggestions and set the provider; keep suggestions only while no service is chosen.`}
	}
	if hasSuggestions && (hasContract || ref != "") {
		return &shapeProblem{code: CodeSchemaViolation, message: `while "suggestions" are open, "resource.contract" and "resource.ref" stay unset — they describe the chosen service, and none is chosen yet.`}
	}
	if hasContract {
		if p := validateContract(contractV); p != nil {
			return p
		}
	}
	return nil
}

// validateContract checks a project contract object: `{ type, path, origin?,
// accepted? }` with the path a bare file name in the dependency directory
// that fits the type, and no URL form at all.
func validateContract(v any) *shapeProblem {
	obj, ok := v.(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "resource.contract: must be an object"}
	}
	for k := range obj {
		if !contractKeys[k] {
			if k == "url" {
				return &shapeProblem{code: CodeSchemaViolation, message: `resource.contract has no "url" form — a contract is a FILE in this directory ({ "type", "path" }); an internet address belongs in "provenance.sourceUrl" and the document itself is copied beside this file.`}
			}
			return &shapeProblem{code: CodeSchemaViolation, message: "resource.contract: unknown property " + k}
		}
	}
	t, _ := obj["type"].(string)
	if !contractTypes[t] {
		return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`resource.contract.type: %q is not an allowed value (openapi, graphql, sdk)`, obj["type"])}
	}
	path, _ := obj["path"].(string)
	if path == "" {
		return &shapeProblem{code: CodeSchemaViolation, message: "resource.contract.path: must be a non-empty string"}
	}
	if strings.Contains(path, "/") {
		return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`resource.contract.path is a file name in this directory (e.g. "openapi.yaml"), not a path — got %q.`, path)}
	}
	if allowed := contractFilesByType[t]; !containsString(allowed, path) {
		return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`resource.contract.path for type %q must be one of %s, got %q.`, t, quoteAll(allowed), path)}
	}
	if ov, present := obj["origin"]; present {
		if o, ok := ov.(string); !ok || !contractOrigins[o] {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`resource.contract.origin: %q is not an allowed value (registry, provider, derived, assumed)`, ov)}
		}
	}
	if v, present := obj["accepted"]; present && v == nil {
		return &shapeProblem{code: CodeSchemaViolation, message: "resource.contract.accepted: must not be null — omit the field instead"}
	}
	if p := validateAssumption(obj["accepted"], "resource.contract.accepted"); p != nil {
		return p
	}
	return nil
}

func validateProvenance(v any, where string) *shapeProblem {
	if v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: where + ": must be an object"}
	}
	for k := range obj {
		if !provenanceKeys[k] {
			if k == "fetchedAt" || k == "sliced" {
				return &shapeProblem{code: CodeSchemaViolation, message: where + ": " + k + ` is retired — write "readOn" for the instant the source was read; a contract is a whole document, never a slice.`}
			}
			return &shapeProblem{code: CodeSchemaViolation, message: where + ": unknown property " + k}
		}
	}
	for _, f := range []string{"sourceUrl", "registry", "sha256", "readOn"} {
		if fv, present := obj[f]; present {
			if _, ok := fv.(string); !ok {
				return &shapeProblem{code: CodeSchemaViolation, message: where + "." + f + ": must be a string"}
			}
		}
	}
	if s, present := obj["sha256"].(string); present && !sha256HexRe.MatchString(s) {
		return &shapeProblem{code: CodeSchemaViolation, message: where + ".sha256: a lower-case hex SHA-256"}
	}
	if r, present := obj["registry"].(string); present && (strings.Contains(r, "://") || strings.HasPrefix(r, "/")) {
		return &shapeProblem{code: CodeSchemaViolation, message: where + `.registry: the org-resource-docs path ("<name>/<file>"), never a URL`}
	}
	return nil
}

func validateAssumption(v any, where string) *shapeProblem {
	if v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: where + ": must be an object"}
	}
	for k := range obj {
		if !assumptionKeys[k] {
			return &shapeProblem{code: CodeSchemaViolation, message: where + ": unknown property " + k}
		}
	}
	for _, f := range []string{"by", "at"} {
		s, ok := obj[f].(string)
		if !ok || s == "" {
			return &shapeProblem{code: CodeSchemaViolation, message: where + "." + f + ": must be a non-empty string"}
		}
	}
	if nv, present := obj["note"]; present {
		if _, ok := nv.(string); !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: where + ".note: must be a string"}
		}
	}
	return nil
}

func validateSuggestions(v any) *shapeProblem {
	list, ok := v.([]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "suggestions: must be an array"}
	}
	for i, c := range list {
		obj, ok := c.(map[string]any)
		if !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("suggestions[%d]: must be an object", i)}
		}
		for k := range obj {
			if !suggestionKeys[k] {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("suggestions[%d]: unknown property %s", i, k)}
			}
		}
		if n, ok := obj["name"].(string); !ok || n == "" {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("suggestions[%d].name: must be a non-empty string", i)}
		}
		if sv, present := obj["style"]; present {
			if s, ok := sv.(string); !ok || !dependencyStyles[s] {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("suggestions[%d].style: %q is not an allowed value", i, sv)}
			}
		}
		if dv, present := obj["description"]; present {
			if _, ok := dv.(string); !ok {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("suggestions[%d].description: must be a string", i)}
			}
		}
	}
	return nil
}

func validateConfigKeys(v any) *shapeProblem {
	list, ok := v.([]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "config: must be an array"}
	}
	for i, c := range list {
		obj, ok := c.(map[string]any)
		if !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("config[%d]: must be an object", i)}
		}
		for k := range obj {
			if !configKeyKeys[k] {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("config[%d]: unknown property %s", i, k)}
			}
		}
		key, ok := obj["key"].(string)
		if !ok || key == "" {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("config[%d].key: must be a non-empty string", i)}
		}
		secret := false
		if v, present := obj["secret"]; present {
			b, ok := v.(bool)
			if !ok {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("config[%d].secret: must be a boolean", i)}
			}
			secret = b
		}
		for _, f := range []string{"description", "defaultValue"} {
			if v, present := obj[f]; present {
				if _, ok := v.(string); !ok {
					return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("config[%d].%s: must be a string", i, f)}
				}
			}
		}
		if _, hasDefault := obj["defaultValue"]; hasDefault && secret {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("config key %q is secret and cannot carry a defaultValue.", key)}
		}
	}
	return nil
}

func validateSdkManifest(content string) *shapeProblem {
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &shapeProblem{code: CodeInvalidJSON, message: "content is not valid JSON: " + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "must be an object"}
	}
	for k := range obj {
		if !sdkManifestKeys[k] {
			return &shapeProblem{code: CodeSchemaViolation, message: "unknown property " + k}
		}
	}
	packages, ok := obj["packages"].(map[string]any)
	if !ok {
		return &shapeProblem{code: CodeSchemaViolation, message: "packages: must be an object"}
	}
	if len(packages) == 0 {
		return &shapeProblem{code: CodeSchemaViolation, message: `"packages" needs at least one language → package entry (e.g. "typescript": "npm:stripe@^14").`}
	}
	for lang, pv := range packages {
		pkg, ok := pv.(string)
		if !ok || pkg == "" {
			return &shapeProblem{code: CodeSchemaViolation, message: "packages." + lang + ": must be a non-empty string"}
		}
		if lang != strings.ToLower(lang) {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("language keys are lower-case (%q), got %q.", strings.ToLower(lang), lang)}
		}
		if !packageRefRe.MatchString(pkg) {
			return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf(`package for %q must be ecosystem-prefixed ("npm:…", "go:…", "pypi:…", "ballerina:…"), got %q.`, lang, pkg)}
		}
	}
	if dv, present := obj["docsUrl"]; present {
		if _, ok := dv.(string); !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: "docsUrl: must be a string"}
		}
	}
	if av, present := obj["assumed"]; present {
		if _, ok := av.(bool); !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: "assumed: must be a boolean"}
		}
	}
	if dv, present := obj["derived"]; present {
		if _, ok := dv.(bool); !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: "derived: must be a boolean"}
		}
	}
	if cv, present := obj["calls"]; present {
		list, ok := cv.([]any)
		if !ok {
			return &shapeProblem{code: CodeSchemaViolation, message: "calls: must be an array"}
		}
		for i, c := range list {
			if s, ok := c.(string); !ok || s == "" {
				return &shapeProblem{code: CodeSchemaViolation, message: fmt.Sprintf("calls[%d]: must be a non-empty string", i)}
			}
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func quoteAll(list []string) string {
	q := make([]string, 0, len(list))
	for _, x := range list {
		q = append(q, fmt.Sprintf("%q", x))
	}
	return strings.Join(q, ", ")
}
