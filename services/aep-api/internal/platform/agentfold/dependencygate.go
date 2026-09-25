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

// dependencygate.go — the dependency.json / sdk.json write-gate, an EXACT
// port of the agent's zod gate (packages/agent-stream/src/dependency-design-
// schema.ts `checkDependencyDesign`). The two must agree: a write the agent's
// bundle accepts must fold here, and one it rejects must reject here, or the
// agent would self-correct against one rule and the fold would enforce
// another. Kept as a hand-written mirror (like designgate.go) rather than a
// JSON-schema check because half the rules are shape rules the schema cannot
// say — name equals directory, the resource block's name equals it too,
// suggestions and a provider never coexist, config keys follow a chosen
// provider, the contract path fits its type, a copy (`ref`) and the
// organization's instructions are the platform's to write, `accepted` is
// echoed but never authored.

package agentfold

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

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

// CheckDependencyFileForSave is the save-time gate's view of the same rules
// (spec/save_gate.go): the shape checks the JSON schema cannot express, over a
// file already on the way to the repo. The platform-only rules (`ref`,
// `consumptionInstructions`, `accepted`) do not apply — at save the record is
// the file's own, whoever wrote it — so the file stands in as its own prior.
// Returns ("", "") when the path is not a dependency file or the content
// passes.
func CheckDependencyFileForSave(path, content string) (code, message string) {
	c, m := checkDependencyDesignGuard(path, content, &content)
	return string(c), m
}

// preservePlatformFields is the fold's twin of the TS bundle's: the fields
// only the platform writes on a dependency's definition — the organization's
// `consumptionInstructions` (while the write keeps the copy's `ref`) and the
// contract's `accepted` record — ride through every agent write of the file,
// put back from the prior content when the write leaves them out. Any other
// path, an unparseable write, or nothing on file returns content unchanged.
func preservePlatformFields(path, content string, prior *string) string {
	if prior == nil || dependencyDesignRe.FindStringSubmatch(path) == nil {
		return content
	}
	var before, next map[string]any
	if json.Unmarshal([]byte(*prior), &before) != nil || json.Unmarshal([]byte(content), &next) != nil || next == nil {
		return content
	}
	beforeRes := priorResource(before)
	nextRes, _ := next["resource"].(map[string]any)
	if beforeRes == nil || nextRes == nil {
		return content
	}
	changed := false
	// The organization's instructions ride through a write that leaves them
	// out — but only while the write still names the copy (`ref`). A write
	// that drops the ref is the agent replacing the copy with a resource the
	// project defines (Select a provider on a name the organization never
	// registered, or Reconsider), and an inline resource never carries the
	// organization's instructions; putting them back would re-create the copy
	// the agent just gave up. The ref itself is therefore never put back.
	if _, keepsRef := nextRes["ref"]; keepsRef {
		if was, had := beforeRes["consumptionInstructions"]; had && was != nil {
			if _, present := nextRes["consumptionInstructions"]; !present {
				nextRes["consumptionInstructions"] = was
				changed = true
			}
		}
	}
	if beforeC, ok := beforeRes["contract"].(map[string]any); ok {
		if was, had := beforeC["accepted"]; had && was != nil {
			if nextC, ok := nextRes["contract"].(map[string]any); ok {
				if _, present := nextC["accepted"]; !present {
					nextC["accepted"] = was
					changed = true
				}
			}
		}
	}
	if !changed {
		return content
	}
	next["resource"] = nextRes
	out, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return content
	}
	if strings.HasSuffix(content, "\n") {
		return string(out) + "\n"
	}
	return string(out)
}

func checkDependencyDesignGuard(path, content string, prior *string) (ErrCode, string) {
	if m := sdkManifestRe.FindStringSubmatch(path); m != nil {
		if p := validateSdkManifest(content); p != nil {
			return p.code, path + ": " + p.message
		}
		return "", ""
	}
	m := dependencyDesignRe.FindStringSubmatch(path)
	if m == nil {
		return "", ""
	}
	if p := validateDependencyDesign(content, m[1], prior); p != nil {
		return p.code, path + ": " + p.message
	}
	return "", ""
}

func validateDependencyDesign(content, dirName string, prior *string) *designProblem {
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &designProblem{code: ErrInvalidJSON, message: "content is not valid JSON: " + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "must be an object"}
	}
	for k := range obj {
		if !dependencyDefinitionKeys[k] {
			if retiredDefinitionKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`%q is not a top-level field any more — the resource's own fields (name, description, provider, config, contract) live in the "resource" block; style is not stored (the contract's type says it); "source" is the presence of "resource.ref"; the acceptance record is "resource.contract.accepted".`, k)}
			}
			return &designProblem{code: ErrSchemaViolation, message: "unknown property " + k}
		}
	}
	name, ok := obj["name"].(string)
	if !ok || name == "" {
		return &designProblem{code: ErrSchemaViolation, message: "name: must be a non-empty string"}
	}
	if name != dirName {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("\"name\" must equal the dependency directory (%q), got %q.", dirName, name)}
	}
	resV, hasRes := obj["resource"]
	if !hasRes {
		return &designProblem{code: ErrSchemaViolation, message: `"resource" is required — the block that says what the thing is (name, description; provider, config and contract once a service is chosen).`}
	}
	res, ok := resV.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "resource: must be an object"}
	}
	for k := range res {
		if !resourceKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "resource: unknown property " + k}
		}
	}
	for _, f := range []string{"ref", "name", "description", "provider", "consumptionInstructions"} {
		if v, present := res[f]; present {
			s, ok := v.(string)
			if !ok || (f != "description" && f != "consumptionInstructions" && s == "") {
				return &designProblem{code: ErrSchemaViolation, message: "resource." + f + ": must be a non-empty string"}
			}
		}
	}
	resName, _ := res["name"].(string)
	if resName == "" {
		return &designProblem{code: ErrSchemaViolation, message: "resource.name: must be a non-empty string"}
	}
	if resName != name {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`"resource.name" must equal the dependency name (%q), got %q.`, name, resName)}
	}
	ref, _ := res["ref"].(string)
	if ref != "" && ref != name {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`"resource.ref" must equal the dependency name (%q) — a registered resource is always used under its own name; got %q.`, name, ref)}
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
			return &designProblem{code: ErrSchemaViolation, message: f + ": must not be null — omit the field instead"}
		}
	}
	for _, f := range []string{"provenance", "contract", "config"} {
		if v, present := res[f]; present && v == nil {
			return &designProblem{code: ErrSchemaViolation, message: "resource." + f + ": must not be null — omit the field instead"}
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
			return &designProblem{code: ErrSchemaViolation, message: `"resource.config" is derived from the chosen service and is written only once "resource.provider" is set — leave it out until the user has chosen; the resolve flow derives the keys.`}
		}
	}
	contractV, hasContract := res["contract"]
	if hasSuggestions && provider != "" {
		return &designProblem{code: ErrSchemaViolation, message: `"suggestions" and "resource.provider" never coexist — once the user chose a service, REMOVE suggestions and set the provider; keep suggestions only while no service is chosen.`}
	}
	if hasSuggestions && (hasContract || ref != "") {
		return &designProblem{code: ErrSchemaViolation, message: `while "suggestions" are open, "resource.contract" and "resource.ref" stay unset — they describe the chosen service, and none is chosen yet.`}
	}
	if hasContract {
		if p := validateContract(contractV); p != nil {
			return p
		}
	}
	if p := platformFieldsAuthored(res, prior); p != nil {
		return p
	}
	return nil
}

// validateContract checks a project contract object: `{ type, path, origin?,
// accepted? }` with the path a bare file name in the dependency directory
// that fits the type, and no URL form at all.
func validateContract(v any) *designProblem {
	obj, ok := v.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "resource.contract: must be an object"}
	}
	for k := range obj {
		if !contractKeys[k] {
			if k == "url" {
				return &designProblem{code: ErrSchemaViolation, message: `resource.contract has no "url" form — a contract is a FILE in this directory ({ "type", "path" }); an internet address belongs in "provenance.sourceUrl" and the document itself is copied beside this file.`}
			}
			return &designProblem{code: ErrSchemaViolation, message: "resource.contract: unknown property " + k}
		}
	}
	t, _ := obj["type"].(string)
	if !contractTypes[t] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`resource.contract.type: %q is not an allowed value (openapi, graphql, sdk)`, obj["type"])}
	}
	path, _ := obj["path"].(string)
	if path == "" {
		return &designProblem{code: ErrSchemaViolation, message: "resource.contract.path: must be a non-empty string"}
	}
	if strings.Contains(path, "/") {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`resource.contract.path is a file name in this directory (e.g. "openapi.yaml"), not a path — got %q.`, path)}
	}
	if allowed := contractFilesByType[t]; !containsString(allowed, path) {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`resource.contract.path for type %q must be one of %s, got %q.`, t, quoteAll(allowed), path)}
	}
	if ov, present := obj["origin"]; present {
		if o, ok := ov.(string); !ok || !contractOrigins[o] {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`resource.contract.origin: %q is not an allowed value (registry, provider, derived, assumed)`, ov)}
		}
	}
	if v, present := obj["accepted"]; present && v == nil {
		return &designProblem{code: ErrSchemaViolation, message: "resource.contract.accepted: must not be null — omit the field instead"}
	}
	if p := validateAssumption(obj["accepted"], "resource.contract.accepted"); p != nil {
		return p
	}
	return nil
}

// platformFieldsAuthored reports the first platform-only field the write
// introduces or alters relative to the prior file:
// `resource.consumptionInstructions` (the organization's, landed by the
// platform's copy) and `resource.contract.accepted` (the user's permission
// record). `resource.ref` is NOT one: the agent writes it as the stub that
// asks for the copy. Omitting a field the file has is allowed — the preserve
// step puts it back.
func platformFieldsAuthored(res map[string]any, prior *string) *designProblem {
	var beforeRes map[string]any
	if prior != nil {
		var before map[string]any
		if json.Unmarshal([]byte(*prior), &before) == nil {
			beforeRes, _ = before["resource"].(map[string]any)
		}
	}
	if next, present := res["consumptionInstructions"]; present {
		var was any
		if beforeRes != nil {
			was = beforeRes["consumptionInstructions"]
		}
		if prior == nil || canonicalJSON(was) != canonicalJSON(next) {
			return &designProblem{code: ErrSchemaViolation, message: `"resource.consumptionInstructions" is the organization's, copied by the platform when a registered resource is used here — write the stub { "name", "resource": { "ref", "name" } } and the platform fills the block at save. Leave the field exactly as the file already has it (or omit it).`}
		}
	}
	nextC, _ := res["contract"].(map[string]any)
	if nextC == nil {
		return nil
	}
	next, present := nextC["accepted"]
	if !present {
		return nil
	}
	var was any
	if beforeC, ok := beforeRes["contract"].(map[string]any); ok {
		was = beforeC["accepted"]
	}
	if prior == nil || canonicalJSON(was) != canonicalJSON(next) {
		return &designProblem{code: ErrSchemaViolation, message: `"resource.contract.accepted" is the user's permission record — it is written when the user accepts your proposal from the dependency's definition in the spec view, never by you. Leave the field exactly as the file already has it (or omit it), and ask the user to accept the assumption instead.`}
	}
	return nil
}

func validateProvenance(v any, where string) *designProblem {
	if v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: where + ": must be an object"}
	}
	for k := range obj {
		if !provenanceKeys[k] {
			if k == "fetchedAt" || k == "sliced" {
				return &designProblem{code: ErrSchemaViolation, message: where + ": " + k + ` is retired — write "readOn" for the instant the source was read; a contract is a whole document, never a slice.`}
			}
			return &designProblem{code: ErrSchemaViolation, message: where + ": unknown property " + k}
		}
	}
	for _, f := range []string{"sourceUrl", "registry", "sha256", "readOn"} {
		if fv, present := obj[f]; present {
			if _, ok := fv.(string); !ok {
				return &designProblem{code: ErrSchemaViolation, message: where + "." + f + ": must be a string"}
			}
		}
	}
	if s, present := obj["sha256"].(string); present && !sha256HexRe.MatchString(s) {
		return &designProblem{code: ErrSchemaViolation, message: where + ".sha256: a lower-case hex SHA-256"}
	}
	if r, present := obj["registry"].(string); present && (strings.Contains(r, "://") || strings.HasPrefix(r, "/")) {
		return &designProblem{code: ErrSchemaViolation, message: where + `.registry: the org-resource-docs path ("<name>/<file>"), never a URL`}
	}
	return nil
}

func validateAssumption(v any, where string) *designProblem {
	if v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: where + ": must be an object"}
	}
	for k := range obj {
		if !assumptionKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: where + ": unknown property " + k}
		}
	}
	for _, f := range []string{"by", "at"} {
		s, ok := obj[f].(string)
		if !ok || s == "" {
			return &designProblem{code: ErrSchemaViolation, message: where + "." + f + ": must be a non-empty string"}
		}
	}
	if nv, present := obj["note"]; present {
		if _, ok := nv.(string); !ok {
			return &designProblem{code: ErrSchemaViolation, message: where + ".note: must be a string"}
		}
	}
	return nil
}

func validateSuggestions(v any) *designProblem {
	list, ok := v.([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "suggestions: must be an array"}
	}
	for i, c := range list {
		obj, ok := c.(map[string]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("suggestions[%d]: must be an object", i)}
		}
		for k := range obj {
			if !suggestionKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("suggestions[%d]: unknown property %s", i, k)}
			}
		}
		if n, ok := obj["name"].(string); !ok || n == "" {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("suggestions[%d].name: must be a non-empty string", i)}
		}
		if sv, present := obj["style"]; present {
			if s, ok := sv.(string); !ok || !dependencyStyles[s] {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("suggestions[%d].style: %q is not an allowed value", i, sv)}
			}
		}
		if dv, present := obj["description"]; present {
			if _, ok := dv.(string); !ok {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("suggestions[%d].description: must be a string", i)}
			}
		}
	}
	return nil
}

func validateConfigKeys(v any) *designProblem {
	list, ok := v.([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "config: must be an array"}
	}
	for i, c := range list {
		obj, ok := c.(map[string]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("config[%d]: must be an object", i)}
		}
		for k := range obj {
			if !configKeyKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("config[%d]: unknown property %s", i, k)}
			}
		}
		key, ok := obj["key"].(string)
		if !ok || key == "" {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("config[%d].key: must be a non-empty string", i)}
		}
		secret := false
		if v, present := obj["secret"]; present {
			b, ok := v.(bool)
			if !ok {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("config[%d].secret: must be a boolean", i)}
			}
			secret = b
		}
		for _, f := range []string{"description", "defaultValue"} {
			if v, present := obj[f]; present {
				if _, ok := v.(string); !ok {
					return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("config[%d].%s: must be a string", i, f)}
				}
			}
		}
		if _, hasDefault := obj["defaultValue"]; hasDefault && secret {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("config key %q is secret and cannot carry a defaultValue.", key)}
		}
	}
	return nil
}

func validateSdkManifest(content string) *designProblem {
	var parsed any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &designProblem{code: ErrInvalidJSON, message: "content is not valid JSON: " + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "must be an object"}
	}
	for k := range obj {
		if !sdkManifestKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "unknown property " + k}
		}
	}
	packages, ok := obj["packages"].(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "packages: must be an object"}
	}
	if len(packages) == 0 {
		return &designProblem{code: ErrSchemaViolation, message: `"packages" needs at least one language → package entry (e.g. "typescript": "npm:stripe@^14").`}
	}
	for lang, pv := range packages {
		pkg, ok := pv.(string)
		if !ok || pkg == "" {
			return &designProblem{code: ErrSchemaViolation, message: "packages." + lang + ": must be a non-empty string"}
		}
		if lang != strings.ToLower(lang) {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("language keys are lower-case (%q), got %q.", strings.ToLower(lang), lang)}
		}
		if !packageRefRe.MatchString(pkg) {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf(`package for %q must be ecosystem-prefixed ("npm:…", "go:…", "pypi:…", "ballerina:…"), got %q.`, lang, pkg)}
		}
	}
	if dv, present := obj["docsUrl"]; present {
		if _, ok := dv.(string); !ok {
			return &designProblem{code: ErrSchemaViolation, message: "docsUrl: must be a string"}
		}
	}
	if av, present := obj["assumed"]; present {
		if _, ok := av.(bool); !ok {
			return &designProblem{code: ErrSchemaViolation, message: "assumed: must be a boolean"}
		}
	}
	if dv, present := obj["derived"]; present {
		if _, ok := dv.(bool); !ok {
			return &designProblem{code: ErrSchemaViolation, message: "derived: must be a boolean"}
		}
	}
	if cv, present := obj["calls"]; present {
		list, ok := cv.([]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: "calls: must be an array"}
		}
		for i, c := range list {
			if s, ok := c.(string); !ok || s == "" {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("calls[%d]: must be a non-empty string", i)}
			}
		}
	}
	return nil
}

// canonicalJSON is JSON with sorted object keys — the TS gate compares the
// same way (its `canonical`), so a re-ordered echo reads equal on both sides.
// encoding/json sorts map keys, so one marshal round trip is the canon.
func canonicalJSON(v any) string {
	if v == nil {
		return "null"
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "undefined"
	}
	return string(out)
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

// priorResource is the prior file's resource block. A file written before the
// nested shape has none, and its acceptance record sits at the top level as
// `assumed` — the codec lifts that when it DECODES, but the gate reads the raw
// prior, so it lifts it here too. Without this, the first nested write over a
// flat file silently drops the user's acceptance.
func priorResource(before map[string]any) map[string]any {
	if res, ok := before["resource"].(map[string]any); ok {
		return res
	}
	assumed, ok := before["assumed"].(map[string]any)
	if !ok || assumed == nil {
		return nil
	}
	return map[string]any{"contract": map[string]any{"accepted": assumed}}
}
