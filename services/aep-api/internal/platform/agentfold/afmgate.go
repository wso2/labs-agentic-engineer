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

// afmgate.go — the agent.afm.md write-gate, an accept/reject port of
// packages/agent-stream/src/agent-afm-schema.ts checkAgentAfm (the zod gate
// the TS FileBundle runs on every write). Only the FIRST problem is reported,
// in the same rule order the zod gate evaluates it in, so the first-failure
// message corresponds across both gates — accept/reject parity is what the
// fold needs, not identical wording (message text is log-only, same
// convention as designgate.go).
//
// Validates over map[string]any + explicit known-key sets, mirroring
// designgate.go's own approach, rather than a typed struct decoded with
// yaml.Decoder.KnownFields(true): a struct field list silently drifts from
// the zod schema whenever the schema gains an optional field (version,
// interfaces[].exposure, x-aep.memory, x-aep.identity were all missed by an
// earlier struct-based draft of this file, which then hard-rejected every
// document using them — including the platform's own generated AFM output).
// A known-key set is the same shape as the zod strictObject it mirrors, so a
// schema addition is a one-line diff here instead of a silent gap.
//
// The "at most as strict as the TS gate" precedent designgate.go documents
// (for validateComponentDesign) is narrower than it may first read: it
// covers fields that are PLATFORM-owned (exposesAPI, componentAgentInstructions
// — never authored by the agent), where under-validating is safe by
// construction because the agent cannot produce a bad value for them. Every
// field this gate checks below is agent-authored and zod-required, so this
// file checks all of them — description, model.name,
// model.authentication.type, and the `.min(1)` constraints on interfaces,
// x-aep.tools.openapi, and allow included. Skipping any of those would let
// this gate accept a document the TS zod gate rejects, which breaks parity
// in the unsafe direction (the fold would commit a write the agent's own
// gate should have refused).
//
// afmgate_test.go's TestValidateAgentAfm_LiveFixture guards the type of drift
// that motivated the map-based rewrite: it runs an actual AFM document that
// the platform's own design flow produced
// (testdata/lunch-chat-agent.afm.md) through this gate.

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// afmFrontMatterRe is FRONT_MATTER from agent-afm-schema.ts: the leading
// `---\n<block>\n---` fence followed by the body.
var afmFrontMatterRe = regexp.MustCompile(`^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$`)

// envRefPattern mirrors the zod gate's envRef: a value the platform injects.
// A literal here reaches git.
var envRefPattern = regexp.MustCompile(`^\$\{env:[A-Za-z_][A-Za-z0-9_]*\}$`)

// afmUnsupportedKeys mirrors UNSUPPORTED in agent-afm-schema.ts: AFM keys the
// spec defines but this platform does not carry yet. A named message says
// the truth — the field is real, we do not support it — instead of reading
// like a typo (which a generic unknown-property error would).
var afmUnsupportedKeys = map[string]string{
	"tools":  "MCP tools are not supported — an agent reaches our services over their OpenAPI contracts (x-aep.tools.openapi)",
	"skills": "agent skills are not supported in this version",
}

// afmModelProviders mirrors the zod gate's model.provider enum. Decides which
// AI SDK adapter the build compiles against — a v1 constraint, not a
// preference; do not widen without widening the zod gate first.
var afmModelProviders = map[string]bool{"anthropic": true, "openai": true}

// afmIdentityModes mirrors the zod gate's x-aep.identity.mode enum.
var afmIdentityModes = map[string]bool{"on-behalf-of": true, "agent": true}

// Known-key sets, one per strictObject in frontMatterSchema
// (agent-afm-schema.ts). Keep these — not a struct's field list — as the
// single source of truth for "what shape does this object have"; the zod
// schema is the thing being mirrored, and it is defined as key sets too.
var (
	afmTopKnownKeys = map[string]bool{
		"spec_version": true, "name": true, "description": true,
		"version": true, "max_iterations": true, "model": true,
		"interfaces": true, "x-aep": true,
	}
	afmModelKnownKeys       = map[string]bool{"provider": true, "name": true, "url": true, "authentication": true}
	afmAuthKnownKeys        = map[string]bool{"type": true, "api_key": true}
	afmInterfaceKnownKeys   = map[string]bool{"type": true, "exposure": true}
	afmExposureKnownKeys    = map[string]bool{"http": true}
	afmExposureHTTPKeys     = map[string]bool{"path": true}
	afmXAepKnownKeys        = map[string]bool{"tools": true, "memory": true, "identity": true}
	afmToolsKnownKeys       = map[string]bool{"openapi": true}
	afmOpenAPIToolKnownKeys = map[string]bool{"component": true, "baseUrl": true, "allow": true}
	afmMemoryKnownKeys      = map[string]bool{"type": true}
	afmIdentityKnownKeys    = map[string]bool{"mode": true}
)

// validateAgentAfm mirrors checkAgentAfm: parse the `---` front matter fence,
// reject the named-unsupported AFM keys, then walk the same field checks in
// the same order (frontMatterSchema's shape order) so the first failure
// corresponds, finishing with the name==dirName business rule and the body
// section check — both of which run only after the whole front matter shape
// is valid, same as the TS gate.
func validateAgentAfm(content, dirName string) *designProblem {
	m := afmFrontMatterRe.FindStringSubmatch(content)
	if m == nil {
		return &designProblem{code: ErrInvalidJSON, message: "no parseable YAML front matter — not an AFM document"}
	}
	fmBlock, body := m[1], m[2]

	var raw any
	if err := yaml.Unmarshal([]byte(fmBlock), &raw); err != nil {
		return &designProblem{code: ErrInvalidJSON, message: "no parseable YAML front matter — not an AFM document"}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "front matter must be an object"}
	}

	for _, key := range []string{"tools", "skills"} {
		if _, present := obj[key]; present {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("%s: %s", key, afmUnsupportedKeys[key])}
		}
	}

	for k := range obj {
		if !afmTopKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "unknown property " + k}
		}
	}

	sv, ok := obj["spec_version"].(string)
	if !ok || sv != "0.4.0" {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("spec_version: %v is not an allowed value", obj["spec_version"])}
	}
	name, ok := obj["name"].(string)
	if !ok || name == "" {
		return &designProblem{code: ErrSchemaViolation, message: "name: must be a non-empty string"}
	}
	if desc, ok := obj["description"].(string); !ok || desc == "" {
		return &designProblem{code: ErrSchemaViolation, message: "description: must be a non-empty string"}
	}
	if v, present := obj["version"]; present {
		if s, ok := v.(string); !ok || s == "" {
			return &designProblem{code: ErrSchemaViolation, message: "version: must be a non-empty string"}
		}
	}
	if mi, present := obj["max_iterations"]; present {
		n, ok := asInt(mi)
		if !ok || n < 1 {
			return &designProblem{code: ErrSchemaViolation, message: "max_iterations: must be an integer >= 1"}
		}
	}

	if p := validateAfmModel(obj["model"]); p != nil {
		return p
	}
	if p := validateAfmInterfaces(obj["interfaces"]); p != nil {
		return p
	}
	if xaep, present := obj["x-aep"]; present {
		if p := validateAfmXAep(xaep); p != nil {
			return p
		}
	}

	if name != dirName {
		return &designProblem{
			code:    ErrSchemaViolation,
			message: fmt.Sprintf("name %q must equal the component directory name %q", name, dirName),
		}
	}

	for _, section := range []string{"# Role", "# Instructions"} {
		if !strings.Contains(body, section) {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("body must contain a %s section", section)}
		}
	}
	return nil
}

// validateAfmModel mirrors modelSchema.strictObject: provider (enum), name
// (non-empty), url (envRef), authentication.{type (literal "api-key"),
// api_key (envRef)}.
func validateAfmModel(raw any) *designProblem {
	model, ok := raw.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "model: must be an object"}
	}
	for k := range model {
		if !afmModelKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "model: unknown property " + k}
		}
	}
	provider, _ := model["provider"].(string)
	if !afmModelProviders[provider] {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("model.provider: %v is not an allowed value", model["provider"])}
	}
	if mn, ok := model["name"].(string); !ok || mn == "" {
		return &designProblem{code: ErrSchemaViolation, message: "model.name: must be a non-empty string"}
	}
	if u, ok := model["url"].(string); !ok || !envRefPattern.MatchString(u) {
		return &designProblem{code: ErrSchemaViolation, message: "model.url: must be an ${env:...} reference"}
	}
	auth, ok := model["authentication"].(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "model.authentication: must be an object"}
	}
	for k := range auth {
		if !afmAuthKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "model.authentication: unknown property " + k}
		}
	}
	if t, ok := auth["type"].(string); !ok || t != "api-key" {
		return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("model.authentication.type: %v is not an allowed value — only \"api-key\" is supported", auth["type"])}
	}
	if k, ok := auth["api_key"].(string); !ok || !envRefPattern.MatchString(k) {
		return &designProblem{code: ErrSchemaViolation, message: "model.authentication.api_key: must be an ${env:...} reference"}
	}
	return nil
}

// validateAfmInterfaces mirrors z.array(interfaceSchema).min(1): every
// element is {type: "webchat", exposure?: {http: {path: non-empty string}}}.
func validateAfmInterfaces(raw any) *designProblem {
	ifaces, ok := raw.([]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "interfaces: must be an array"}
	}
	if len(ifaces) == 0 {
		return &designProblem{code: ErrSchemaViolation, message: "interfaces: must contain at least 1 element(s)"}
	}
	for i, raw := range ifaces {
		iface, ok := raw.(map[string]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d]: must be an object", i)}
		}
		for k := range iface {
			if !afmInterfaceKnownKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d]: unknown property %s", i, k)}
			}
		}
		t, _ := iface["type"].(string)
		if t != "webchat" {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d].type: %v is not an allowed value — only \"webchat\" is supported", i, iface["type"])}
		}
		if exposure, present := iface["exposure"]; present {
			expObj, ok := exposure.(map[string]any)
			if !ok {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d].exposure: must be an object", i)}
			}
			for k := range expObj {
				if !afmExposureKnownKeys[k] {
					return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d].exposure: unknown property %s", i, k)}
				}
			}
			http, ok := expObj["http"].(map[string]any)
			if !ok {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d].exposure.http: must be an object", i)}
			}
			for k := range http {
				if !afmExposureHTTPKeys[k] {
					return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d].exposure.http: unknown property %s", i, k)}
				}
			}
			if p, ok := http["path"].(string); !ok || p == "" {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("interfaces[%d].exposure.http.path: must be a non-empty string", i)}
			}
		}
	}
	return nil
}

// validateAfmXAep mirrors the optional `x-aep` strictObject: tools (optional
// {openapi: array(openApiToolSchema).min(1)}), memory (optional {type:
// "client"|"server"}), identity (optional {mode: enum}).
func validateAfmXAep(raw any) *designProblem {
	xaep, ok := raw.(map[string]any)
	if !ok {
		return &designProblem{code: ErrSchemaViolation, message: "x-aep: must be an object"}
	}
	for k := range xaep {
		if !afmXAepKnownKeys[k] {
			return &designProblem{code: ErrSchemaViolation, message: "x-aep: unknown property " + k}
		}
	}
	if tools, present := xaep["tools"]; present {
		toolsObj, ok := tools.(map[string]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: "x-aep.tools: must be an object"}
		}
		for k := range toolsObj {
			if !afmToolsKnownKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: "x-aep.tools: unknown property " + k}
			}
		}
		openapi, ok := toolsObj["openapi"].([]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: "x-aep.tools.openapi: must be an array"}
		}
		if len(openapi) == 0 {
			return &designProblem{code: ErrSchemaViolation, message: "x-aep.tools.openapi: must contain at least 1 element(s)"}
		}
		for i, raw := range openapi {
			entry, ok := raw.(map[string]any)
			if !ok {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d]: must be an object", i)}
			}
			for k := range entry {
				if !afmOpenAPIToolKnownKeys[k] {
					return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d]: unknown property %s", i, k)}
				}
			}
			if c, ok := entry["component"].(string); !ok || c == "" {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d].component: must be a non-empty string", i)}
			}
			if u, ok := entry["baseUrl"].(string); !ok || !envRefPattern.MatchString(u) {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d].baseUrl: must be an ${env:...} reference", i)}
			}
			allow, ok := entry["allow"].([]any)
			if !ok {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d].allow: must be an array", i)}
			}
			if len(allow) == 0 {
				return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d].allow: must contain at least 1 element(s)", i)}
			}
			for j, a := range allow {
				if s, ok := a.(string); !ok || s == "" {
					return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.tools.openapi[%d].allow[%d]: must be a non-empty string", i, j)}
				}
			}
		}
	}
	if memory, present := xaep["memory"]; present {
		mem, ok := memory.(map[string]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: "x-aep.memory: must be an object"}
		}
		for k := range mem {
			if !afmMemoryKnownKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: "x-aep.memory: unknown property " + k}
			}
		}
		if t, ok := mem["type"].(string); !ok || (t != "client" && t != "server") {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.memory.type: %v is not an allowed value — must be \"client\" or \"server\"", mem["type"])}
		}
	}
	if identity, present := xaep["identity"]; present {
		ident, ok := identity.(map[string]any)
		if !ok {
			return &designProblem{code: ErrSchemaViolation, message: "x-aep.identity: must be an object"}
		}
		for k := range ident {
			if !afmIdentityKnownKeys[k] {
				return &designProblem{code: ErrSchemaViolation, message: "x-aep.identity: unknown property " + k}
			}
		}
		mode, _ := ident["mode"].(string)
		if !afmIdentityModes[mode] {
			return &designProblem{code: ErrSchemaViolation, message: fmt.Sprintf("x-aep.identity.mode: %v is not an allowed value", ident["mode"])}
		}
	}
	return nil
}

// asInt accepts the numeric types yaml.v3 may decode a scalar into when the
// target is `any` (int for a plain YAML integer; float64 defensively, since
// callers of this package sometimes round-trip through encoding/json).
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}
