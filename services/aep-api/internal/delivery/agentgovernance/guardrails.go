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

package agentgovernance

// guardrails.go turns an agent's declared guardrails (x-aep.guardrails) into
// the policies its Agent Manager binding carries. Pure: no I/O, so every rule
// below is a table test.
//
// The spec names a gateway policy and the params its use case needs. What the
// platform adds is everything that depends on the platform rather than the use
// case — the catalog's exact version, the JSONPaths for the agent's real
// traffic, and the global scope — and it checks the result against the
// policy's own schema before anything is written. Each rule here was verified
// against a live gateway (2026-09-30):
//
//   - Every model call resends the whole conversation, and the gateway's
//     path syntax has fixed positions only — no "last message whose role is
//     user". A first-message path misses every later turn; a last-message
//     path lands on a tool result in a tool turn and fails the call. So a
//     guardrail reads the WHOLE request (an empty jsonPath). That is also
//     where the agent's own instructions sit, so a block pattern matching
//     them is refused, and a request-side count or length check — which
//     would measure the whole conversation — is not written.
//   - The gateway refuses a policy it cannot build, and ONE refused policy
//     fails the agent's whole policy chain: every model call answers 500,
//     while Agent Manager's write still answers 200. So nothing is written
//     that has not been checked: an unreadable schema, a rule this checker
//     cannot evaluate, an uncompilable regex or min above max is invalid.
//   - A reply-side check does not stop a STREAMED reply: the gateway asks to
//     end the stream after the offending chunk is already forwarded. Every
//     generated agent streams, so a reply-side check is dropped and reported
//     rather than left in place promising protection it does not give.

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// GuardrailStatus is what became of one declared guardrail at deploy.
type GuardrailStatus string

const (
	GuardrailApplied     GuardrailStatus = "applied"
	GuardrailPartial     GuardrailStatus = "partial"
	GuardrailUnavailable GuardrailStatus = "unavailable"
	GuardrailInvalid     GuardrailStatus = "invalid"
	GuardrailConflict    GuardrailStatus = "conflict"
	GuardrailFailed      GuardrailStatus = "failed"
	GuardrailUnsupported GuardrailStatus = "unsupported"
)

// GuardrailOutcome is one declared guardrail's status, with the reason when it
// is anything but applied.
type GuardrailOutcome struct {
	Policy string          `json:"policy"`
	Status GuardrailStatus `json:"status"`
	Reason string          `json:"reason,omitempty"`
}

// wholeRequest is the jsonPath that hands the gateway policy the whole
// request body as text.
const wholeRequest = ""

// guardrailContext is what resolving a declaration depends on besides the
// declaration itself.
type guardrailContext struct {
	catalog []agentmanager.PolicyDefinition
	format  modelconn.Format
	// instructions is the agent's prompt body, which every request carries.
	instructions string
	// toolText is what the agent's tool definitions are built from — each
	// allowed operation's name, summary, description and parameters — which a
	// tool-using agent also sends in every request.
	toolText []string
	// agentUsesTools and agentTakesFiles decide where a request-side check can
	// find the user's message. With either, the last message is not always the
	// user's text: on a tool turn it is a tool result, and a message may be a
	// file alone.
	agentUsesTools  bool
	agentTakesFiles bool
}

// lastMessageText is where an agent with no tools and no file uploads always
// finds the user's latest message.
const lastMessageText = "$.messages[-1].content[0].text"

// requestPath is where this agent's request-side checks read: the user's
// latest message when it is always there, else the whole request.
func (gc guardrailContext) requestPath() string {
	if gc.agentUsesTools || gc.agentTakesFiles {
		return wholeRequest
	}
	return lastMessageText
}

// IsGuardrailPolicy reports whether a catalog policy is a guardrail. The
// catalog lists every gateway policy — auth, rate limits, transformers — and
// carries no category, so this follows the policy hub's Guardrails category
// (https://wso2.com/api-platform/policy-hub?categories=Guardrails): every name
// there ends in -guardrail but PII masking's.
func IsGuardrailPolicy(name string) bool {
	return strings.HasSuffix(name, "-guardrail") || name == "pii-masking-regex"
}

// needsGatewayConfiguration reports whether a policy takes settings from the
// gateway's own configuration — an external model's endpoint and key. The
// catalog lists such a policy whether or not they are set, and it fails every
// call when they are not; Agent Manager's console hides these until an
// operator enables them, and so does AEP.
func needsGatewayConfiguration(def agentmanager.PolicyDefinition) bool {
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if len(def.SystemParameters) == 0 || json.Unmarshal(def.SystemParameters, &schema) != nil {
		return false
	}
	return len(schema.Properties) > 0
}

// restoresReplies names the policies whose reply-side work is inherent rather
// than configured, and was verified to hold on a streamed reply: PII masking
// restores the masked values in the reply stream.
var restoresReplies = map[string]bool{"pii-masking-regex": true}

// measuresRequest names the policies whose request side measures or shapes
// the input. Over the whole request they would measure the instructions and
// the history rather than what the user typed.
var measuresRequest = map[string]bool{
	"word-count-guardrail":     true,
	"sentence-count-guardrail": true,
	"content-length-guardrail": true,
	"json-schema-guardrail":    true,
}

const (
	reasonReplySide    = "the reply-side check is not applied: the gateway does not enforce it on streamed replies"
	reasonWholeRequest = "the request-side check is not applied: for an agent with tools or file uploads no path finds the user's message in every call, and the whole request would measure the conversation"
	reasonNothingLeft  = "nothing left to apply: "
)

// resolveGuardrails turns the declared guardrails into binding policies, and
// reports one outcome per declaration, in order.
func resolveGuardrails(declared []delivery.GuardrailDeclaration, gc guardrailContext) ([]agentmanager.BindingPolicy, []GuardrailOutcome) {
	byName := make(map[string]agentmanager.PolicyDefinition, len(gc.catalog))
	for _, p := range gc.catalog {
		byName[p.Name] = p
	}
	var policies []agentmanager.BindingPolicy
	outcomes := make([]GuardrailOutcome, 0, len(declared))
	for _, d := range declared {
		policy, outcome := resolveOne(d, byName, gc)
		if policy != nil {
			policies = append(policies, *policy)
		}
		outcomes = append(outcomes, outcome)
	}
	return policies, outcomes
}

func resolveOne(d delivery.GuardrailDeclaration, byName map[string]agentmanager.PolicyDefinition, gc guardrailContext) (*agentmanager.BindingPolicy, GuardrailOutcome) {
	out := func(status GuardrailStatus, reason string) (*agentmanager.BindingPolicy, GuardrailOutcome) {
		return nil, GuardrailOutcome{Policy: d.Policy, Status: status, Reason: reason}
	}
	if gc.format != modelconn.FormatAnthropic {
		return out(GuardrailUnsupported, "guardrails need an Anthropic-format model connection so far")
	}
	def, offered := byName[d.Policy]
	if !offered || !IsGuardrailPolicy(d.Policy) {
		return out(GuardrailUnavailable, "this environment's AI gateway does not offer the guardrail "+d.Policy)
	}
	if needsGatewayConfiguration(def) {
		return out(GuardrailUnavailable, d.Policy+" calls an external service the AI gateway would need configuring for")
	}
	schema, raw, err := checkableSchema(def.Parameters)
	if err != nil {
		return out(GuardrailInvalid, "AEP cannot check this guardrail's settings: "+err.Error())
	}
	params, err := jsonRoundTrip(d.Params)
	if err != nil {
		return out(GuardrailInvalid, err.Error())
	}

	var dropped []string
	if phaseEnabled(params, raw, "response", false) && !restoresReplies[d.Policy] {
		delete(params, "response")
		dropped = append(dropped, reasonReplySide)
	}
	path := gc.requestPath()
	if measuresRequest[d.Policy] && path == wholeRequest && phaseEnabled(params, raw, "request", true) {
		delete(params, "request")
		dropped = append(dropped, reasonWholeRequest)
	}
	if isPhasePolicy(schema) && !phaseEnabled(params, raw, "request", true) && !phaseEnabled(params, raw, "response", false) {
		// The gateway refuses a phase policy with no phase, failing the agent's
		// whole policy chain. Not written.
		reason := "no request or reply check was declared"
		if len(dropped) > 0 {
			reason = reasonNothingLeft + strings.Join(dropped, "; ")
		}
		return out(GuardrailUnsupported, reason)
	}

	injectRequestPath(params, schema, path)
	if msgs := jsonschema.Validate(params, schema); len(msgs) > 0 {
		return out(GuardrailInvalid, msgs[0])
	}
	if reason := semanticProblem(params); reason != "" {
		return out(GuardrailInvalid, reason)
	}
	if path == wholeRequest {
		if part := blocksTheAgentsOwnRequest(d.Policy, params, gc); part != "" {
			return out(GuardrailInvalid, "the pattern matches "+part+", which every request carries, so it would block every call")
		}
	}

	status, reason := GuardrailApplied, ""
	if len(dropped) > 0 {
		status, reason = GuardrailPartial, strings.Join(dropped, "; ")
	}
	return &agentmanager.BindingPolicy{
		Name:    def.Name,
		Version: def.Version,
		Paths:   []agentmanager.BindingPolicyPath{{Path: "/*", Methods: []string{"*"}, Params: params}},
	}, GuardrailOutcome{Policy: d.Policy, Status: status, Reason: reason}
}

// jsonRoundTrip gives params the types the catalog's JSON Schema describes: a
// YAML integer decodes as an int, where JSON has only float64. It also copies,
// so the declaration is never mutated.
func jsonRoundTrip(params map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// annotationKeywords carry no rule, so a schema may use them freely.
var annotationKeywords = map[string]bool{
	"description": true, "default": true, "title": true, "examples": true, "deprecated": true,
	"readOnly": true, "writeOnly": true, "$comment": true, "$id": true,
}

// checkableSchema reads a catalog parameters schema, and refuses one this
// checker cannot fully evaluate: an unchecked rule would pass a value the
// gateway then refuses — taking the agent down. It returns the parsed schema
// and the raw document (for defaults, which the interpreter does not model).
func checkableSchema(raw json.RawMessage) (*jsonschema.Schema, map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil, errors.New("the gateway published no schema for it")
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, errors.New("its schema does not parse")
	}
	var unknown []string
	for _, k := range jsonschema.UnsupportedKeywords(raw) {
		if !annotationKeywords[k] && !strings.HasPrefix(k, "x-") {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, nil, fmt.Errorf("its schema uses rules AEP does not check (%s)", strings.Join(unknown, ", "))
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, nil, errors.New("its schema does not parse")
	}
	return &s, doc, nil
}

// phaseEnabled reports whether the request or response block is configured
// and on: its own `enabled`, else the schema's default for it, else
// fallback.
func phaseEnabled(params map[string]any, raw map[string]any, phase string, fallback bool) bool {
	block, ok := params[phase].(map[string]any)
	if !ok {
		return false
	}
	if enabled, ok := block["enabled"].(bool); ok {
		return enabled
	}
	if def, ok := schemaDefault(raw, phase, "enabled").(bool); ok {
		return def
	}
	return fallback
}

// schemaDefault reads properties.<phase>.properties.<key>.default.
func schemaDefault(raw map[string]any, phase, key string) any {
	props, _ := raw["properties"].(map[string]any)
	phaseSchema, _ := props[phase].(map[string]any)
	phaseProps, _ := phaseSchema["properties"].(map[string]any)
	keySchema, _ := phaseProps[key].(map[string]any)
	return keySchema["default"]
}

// semanticProblem catches what no schema expresses but the gateway refuses on
// every call: a regex that does not compile, and a minimum above a maximum.
func semanticProblem(params map[string]any) string {
	scopes := []map[string]any{params}
	for _, phase := range []string{"request", "response"} {
		if block, ok := params[phase].(map[string]any); ok {
			scopes = append(scopes, block)
		}
	}
	for _, scope := range scopes {
		if re, ok := scope["regex"].(string); ok {
			if _, err := regexp.Compile(re); err != nil {
				return "the regex does not compile: " + err.Error()
			}
		}
		minV, minOK := scope["min"].(float64)
		maxV, maxOK := scope["max"].(float64)
		if minOK && maxOK && minV > maxV {
			return "min is greater than max"
		}
	}
	if entities, ok := params["customPIIEntities"].([]any); ok {
		for _, e := range entities {
			entity, _ := e.(map[string]any)
			if re, ok := entity["piiRegex"].(string); ok {
				if _, err := regexp.Compile(re); err != nil {
					return "a custom PII regex does not compile: " + err.Error()
				}
			}
		}
	}
	return ""
}

// blocksTheAgentsOwnRequest names the part of every request an inverted
// regex block would match — the agent's instructions or its tool definitions —
// or "" when it matches neither. A whole-request check reads both on every
// call, so a match there refuses the agent outright.
//
// The tool text is the contract each tool is built from; parameter wording the
// agent adds in code is not known here, so this catches the certain cases, not
// every one.
func blocksTheAgentsOwnRequest(policy string, params map[string]any, gc guardrailContext) string {
	if policy != "regex-guardrail" {
		return ""
	}
	req, ok := params["request"].(map[string]any)
	if !ok || req["invert"] != true {
		return ""
	}
	pattern, _ := req["regex"].(string)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	if gc.instructions != "" && re.MatchString(gc.instructions) {
		return "the agent's own instructions"
	}
	for _, text := range gc.toolText {
		if re.MatchString(text) {
			return fmt.Sprintf("the agent's tool definitions (%q)", text)
		}
	}
	return ""
}

// isPhasePolicy reports whether a policy is configured per phase — a request
// block and a response block, at least one of which must be present.
func isPhasePolicy(schema *jsonschema.Schema) bool {
	if schema == nil {
		return false
	}
	_, req := schema.Properties["request"]
	_, resp := schema.Properties["response"]
	return req && resp
}

// injectRequestPath sets where the policy's request-side check reads: the
// top-level jsonPath (a request-rewriting policy) at the whole request, and
// request.jsonPath at requestPath. A policy with neither takes no path.
func injectRequestPath(params map[string]any, schema *jsonschema.Schema, requestPath string) {
	if schema == nil {
		return
	}
	// A top-level path belongs to a policy that rewrites the request — PII
	// masking — and masking every message, history included, is the point.
	if _, has := schema.Properties["jsonPath"]; has {
		params["jsonPath"] = wholeRequest
	}
	if _, has := schema.Properties["request"]; has {
		if req, ok := params["request"].(map[string]any); ok {
			req["jsonPath"] = requestPath
		}
	}
}

// mergeGuardrails folds the resolved policies into the binding's current ones.
//
// AEP manages only what it wrote: entries whose names it applied last time are
// replaced, every other entry is an operator's and stays exactly as it is. A
// resolved policy that shares its name with an operator's entry is not written
// — that would overwrite their settings — and is reported as a conflict.
// changed is false when the result is what the binding already holds, because
// every write redeploys the agent's proxy.
func mergeGuardrails(current []agentmanager.BindingPolicy, previouslyOwned []string,
	resolved []agentmanager.BindingPolicy) (next []agentmanager.BindingPolicy, owned []string, conflicts map[string]bool, changed bool) {
	wasOwned := make(map[string]bool, len(previouslyOwned))
	for _, n := range previouslyOwned {
		wasOwned[n] = true
	}
	operators := map[string]bool{}
	next = []agentmanager.BindingPolicy{}
	for _, p := range current {
		if wasOwned[p.Name] {
			continue
		}
		operators[p.Name] = true
		next = append(next, p)
	}
	conflicts = map[string]bool{}
	owned = []string{}
	for _, p := range resolved {
		if operators[p.Name] {
			conflicts[p.Name] = true
			continue
		}
		next = append(next, p)
		owned = append(owned, p.Name)
	}
	return next, owned, conflicts, !sameJSON(current, next)
}

// sameJSON compares two policy lists as the JSON Agent Manager stores,
// regardless of order: a number read back as float64 equals the same number
// written as an int, and a binding handed back in another order is not a
// change.
func sameJSON(a, b []agentmanager.BindingPolicy) bool {
	if len(a) != len(b) {
		return false
	}
	return reflect.DeepEqual(canonical(a), canonical(b))
}

// canonical is the policies as decoded JSON, keyed by name.
func canonical(ps []agentmanager.BindingPolicy) map[string]any {
	out := make(map[string]any, len(ps))
	for _, p := range ps {
		raw, err := json.Marshal(p)
		if err != nil {
			return nil
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil
		}
		out[p.Name] = v
	}
	return out
}
