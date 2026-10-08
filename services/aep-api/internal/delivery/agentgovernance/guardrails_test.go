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

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// guardrailCatalogJSON is four entries of the catalog a live gateway returned
// (2026-09-30), schemas and all — so the resolver is tested against what the
// gateway actually publishes, OpenAI-shaped defaults included.
//
//go:embed testdata/guardrail-catalog.json
var guardrailCatalogJSON []byte

func testCatalog(t *testing.T) []agentmanager.PolicyDefinition {
	t.Helper()
	var c struct {
		List []agentmanager.PolicyDefinition `json:"list"`
	}
	if err := json.Unmarshal(guardrailCatalogJSON, &c); err != nil {
		t.Fatal(err)
	}
	return c.List
}

func declare(policy string, params map[string]any) delivery.GuardrailDeclaration {
	return delivery.GuardrailDeclaration{Policy: policy, Params: params, Why: "because"}
}

func onlyOutcome(t *testing.T, outcomes []GuardrailOutcome) GuardrailOutcome {
	t.Helper()
	if len(outcomes) != 1 {
		t.Fatalf("want one outcome, got %+v", outcomes)
	}
	return outcomes[0]
}

// resolve runs the resolver the way a governed Anthropic agent does, against
// the live catalog fixture.
// resolve runs the resolver for an agent that calls tools — the common case,
// and the one where the last message is not always the user's text.
func resolve(t *testing.T, instructions string, decls ...delivery.GuardrailDeclaration) ([]agentmanager.BindingPolicy, []GuardrailOutcome) {
	t.Helper()
	return resolveGuardrails(decls, guardrailContext{catalog: testCatalog(t), format: modelconn.FormatAnthropic,
		instructions: instructions, agentUsesTools: true})
}

// resolvePlain runs it for an agent with no tools and no file uploads, whose
// last message is always the user's text.
func resolvePlain(t *testing.T, instructions string, decls ...delivery.GuardrailDeclaration) ([]agentmanager.BindingPolicy, []GuardrailOutcome) {
	t.Helper()
	return resolveGuardrails(decls, guardrailContext{catalog: testCatalog(t), format: modelconn.FormatAnthropic, instructions: instructions})
}

// withPolicy adds one synthetic entry to the live catalog, for rules no real
// policy in the fixture exercises.
func withPolicy(t *testing.T, name, schema string) guardrailContext {
	t.Helper()
	c := testCatalog(t)
	c = append(c, agentmanager.PolicyDefinition{Name: name, Version: "v1.0.0", Parameters: json.RawMessage(schema)})
	return guardrailContext{catalog: c, format: modelconn.FormatAnthropic}
}

// Every model call resends the whole conversation, and the gateway's path
// syntax cannot pick "the latest user message" out of it, so the check reads
// the whole request: every turn is covered, history included.
func TestResolveGuardrails_PIIMaskingReadsTheWholeRequest(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("pii-masking-regex", map[string]any{"email": true}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailApplied {
		t.Fatalf("outcome = %+v, want applied", o)
	}
	want := []agentmanager.BindingPolicy{{Name: "pii-masking-regex", Version: "v1.0.4",
		Paths: []agentmanager.BindingPolicyPath{{Path: "/*", Methods: []string{"*"},
			Params: map[string]any{"email": true, "jsonPath": wholeRequest}}}}}
	if !reflect.DeepEqual(policies, want) {
		t.Fatalf("policies = %+v\nwant %+v", policies, want)
	}
}

// For an agent with tools the last message is a tool result on a tool turn
// (verified live: a last-message path refuses that call), so a block reads
// the whole request.
func TestResolveGuardrails_ARegexBlockReadsTheWholeRequestForAnAgentWithTools(t *testing.T) {
	policies, outcomes := resolve(t, "You read receipts.", declare("regex-guardrail", map[string]any{
		"request": map[string]any{"regex": "(?i)casino", "invert": true}}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailApplied {
		t.Fatalf("outcome = %+v", o)
	}
	req := policies[0].Paths[0].Params["request"].(map[string]any)
	if req["jsonPath"] != wholeRequest || req["regex"] != "(?i)casino" || req["invert"] != true {
		t.Fatalf("request params = %+v", req)
	}
	if _, has := policies[0].Paths[0].Params["response"]; has {
		t.Fatalf("a response block appeared that the spec never declared: %+v", policies[0].Paths[0].Params)
	}
}

// The whole request carries the agent's own instructions. A block pattern
// that matches them would refuse every call the agent makes.
func TestResolveGuardrails_ABlockPatternMatchingTheAgentsInstructionsIsInvalid(t *testing.T) {
	policies, outcomes := resolve(t, "Never log gambling expenses.", declare("regex-guardrail", map[string]any{
		"request": map[string]any{"regex": "(?i)gambling", "invert": true}}))

	o := onlyOutcome(t, outcomes)
	if o.Status != GuardrailInvalid || !strings.Contains(o.Reason, "instructions") {
		t.Fatalf("outcome = %+v, want invalid naming the instructions", o)
	}
	if len(policies) != 0 {
		t.Fatalf("a pattern that blocks every call was written: %+v", policies)
	}
}

// A tool-using agent sends its tool definitions — names and descriptions from
// the provider's contract — in every request too. A block pattern that matches
// one would refuse every call just as surely as one matching the instructions.
func TestResolveGuardrails_ABlockPatternMatchingTheAgentsToolsIsInvalid(t *testing.T) {
	gc := guardrailContext{catalog: testCatalog(t), format: modelconn.FormatAnthropic, agentUsesTools: true,
		instructions: "Help the employee file an expense.",
		toolText:     []string{"logExpense", "Log a casino or restaurant receipt against the trip"}}
	policies, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("regex-guardrail", map[string]any{
		"request": map[string]any{"regex": "(?i)casino", "invert": true}})}, gc)

	o := onlyOutcome(t, outcomes)
	if o.Status != GuardrailInvalid || !strings.Contains(o.Reason, "tool") {
		t.Fatalf("outcome = %+v, want invalid naming the agent's tools", o)
	}
	if len(policies) != 0 {
		t.Fatalf("a pattern that blocks every call was written: %+v", policies)
	}
}

// A plain agent's check reads only the latest message; its tool text, if any
// were passed, is never in what the check reads.
func TestResolveGuardrails_ToolTextDoesNotMatterForALastMessageCheck(t *testing.T) {
	gc := guardrailContext{catalog: testCatalog(t), format: modelconn.FormatAnthropic,
		toolText: []string{"Log a casino receipt"}}
	_, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("regex-guardrail", map[string]any{
		"request": map[string]any{"regex": "(?i)casino", "invert": true}})}, gc)

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailApplied {
		t.Fatalf("outcome = %+v, want applied", o)
	}
}

// Verified live: the gateway does not stop a streamed reply, and every
// generated agent streams. A reply-side check would promise protection it
// does not give, so it is dropped and said so.
func TestResolveGuardrails_ReplySideCheckIsDroppedAndPartial(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("regex-guardrail", map[string]any{
		"request":  map[string]any{"regex": "(?i)casino", "invert": true},
		"response": map[string]any{"enabled": true, "regex": "(?i)roulette", "invert": true},
	}))

	o := onlyOutcome(t, outcomes)
	if o.Status != GuardrailPartial || !strings.Contains(o.Reason, "streamed") {
		t.Fatalf("outcome = %+v, want partial naming streamed replies", o)
	}
	if _, has := policies[0].Paths[0].Params["response"]; has {
		t.Fatalf("the reply-side block was written: %+v", policies[0].Paths[0].Params)
	}
}

// A reply side that is on by default is on without being named.
func TestResolveGuardrails_AReplySideOnByDefaultIsDropped(t *testing.T) {
	gc := withPolicy(t, "reply-default-guardrail", `{"type":"object","properties":{
		"request":{"type":"object","properties":{"regex":{"type":"string"}}},
		"response":{"type":"object","properties":{"enabled":{"type":"boolean","default":true},"regex":{"type":"string"}}}}}`)
	policies, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("reply-default-guardrail", map[string]any{
		"request": map[string]any{"regex": "a"}, "response": map[string]any{"regex": "b"}})}, gc)

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailPartial {
		t.Fatalf("outcome = %+v, want partial", o)
	}
	if _, has := policies[0].Paths[0].Params["response"]; has {
		t.Fatalf("a reply side on by default was written: %+v", policies[0].Paths[0].Params)
	}
}

// Found live: a phase policy left with neither request nor response is
// refused by the gateway, and ONE refused policy fails the agent's whole
// policy chain — every model call then answers 500. A guardrail that only had
// a reply-side check is therefore not written at all.
func TestResolveGuardrails_OnlyAReplySideCheckIsUnsupportedAndNotWritten(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("regex-guardrail", map[string]any{
		"response": map[string]any{"enabled": true, "regex": "(?i)roulette", "invert": true}}))

	o := onlyOutcome(t, outcomes)
	if o.Status != GuardrailUnsupported || !strings.Contains(o.Reason, "streamed") {
		t.Fatalf("outcome = %+v, want unsupported naming streamed replies", o)
	}
	if len(policies) != 0 {
		t.Fatalf("a policy with no phase left was written — the gateway would refuse it and take the agent down: %+v", policies)
	}
}

// A count is about the user's message. For an agent with tools no path lands
// on it in every call, and over the whole request it would measure the
// conversation, so it is not written.
func TestResolveGuardrails_ARequestSideCountIsUnsupportedForAnAgentWithTools(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("word-count-guardrail", map[string]any{
		"request": map[string]any{"enabled": true, "min": 1, "max": 400}}))

	o := onlyOutcome(t, outcomes)
	if o.Status != GuardrailUnsupported || !strings.Contains(o.Reason, "tools or file uploads") {
		t.Fatalf("outcome = %+v, want unsupported naming tools or file uploads", o)
	}
	if len(policies) != 0 {
		t.Fatalf("a whole-conversation count was written: %+v", policies)
	}
}

// For an agent with no tools and no file uploads the last message is always
// the user's text (verified live), so a count reads exactly that.
func TestResolveGuardrails_ARequestSideCountReadsTheLastMessageForAPlainAgent(t *testing.T) {
	policies, outcomes := resolvePlain(t, "", declare("word-count-guardrail", map[string]any{
		"request": map[string]any{"enabled": true, "min": 1, "max": 400}}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailApplied {
		t.Fatalf("outcome = %+v, want applied", o)
	}
	req := policies[0].Paths[0].Params["request"].(map[string]any)
	if req["jsonPath"] != lastMessageText {
		t.Fatalf("request jsonPath = %v, want the last message's text", req["jsonPath"])
	}
}

// A plain agent's block reads only the user's latest message, so the agent's
// own instructions cannot trip it and need not be checked against it.
func TestResolveGuardrails_ARegexBlockReadsTheLastMessageForAPlainAgent(t *testing.T) {
	policies, outcomes := resolvePlain(t, "Never log gambling expenses.", declare("regex-guardrail", map[string]any{
		"request": map[string]any{"regex": "(?i)gambling", "invert": true}}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailApplied {
		t.Fatalf("outcome = %+v, want applied", o)
	}
	if got := policies[0].Paths[0].Params["request"].(map[string]any)["jsonPath"]; got != lastMessageText {
		t.Fatalf("request jsonPath = %v, want the last message's text", got)
	}
}

// A policy that needs settings from the gateway's own configuration — an
// external model's endpoint and key — is listed by the catalog whether or not
// they are set, and fails every call when they are not. Agent Manager's own
// console hides these until an operator enables them.
func TestResolveGuardrails_AGuardrailNeedingGatewayConfigurationIsUnavailable(t *testing.T) {
	c := testCatalog(t)
	c = append(c, agentmanager.PolicyDefinition{Name: "granite-guardian-prompt-injection", Version: "v0.9.0",
		Parameters:       json.RawMessage(`{"type":"object","properties":{"threshold":{"type":"number"}}}`),
		SystemParameters: json.RawMessage(`{"type":"object","properties":{"endpoint":{"type":"string"},"apiKey":{"type":"string"}}}`)})
	policies, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("granite-guardian-prompt-injection", map[string]any{"threshold": 0.5})},
		guardrailContext{catalog: c, format: modelconn.FormatAnthropic})

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailUnavailable || !strings.Contains(o.Reason, "gateway") {
		t.Fatalf("outcome = %+v, want unavailable naming the gateway configuration", o)
	}
	if len(policies) != 0 {
		t.Fatalf("a guardrail the gateway is not configured for was written: %+v", policies)
	}
}

// Prompt decorating rewrites every call rather than guarding it, and the
// policy hub does not file it under guardrails.
func TestResolveGuardrails_APromptDecoratorIsNotAGuardrail(t *testing.T) {
	gc := withPolicy(t, "prompt-decorator", `{"type":"object","properties":{"append":{"type":"boolean"}}}`)
	_, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("prompt-decorator", map[string]any{"append": true})}, gc)

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailUnavailable {
		t.Fatalf("outcome = %+v, want unavailable", o)
	}
}

// A YAML integer is an int; the catalog schema is JSON, where every number is
// a float64. Without a round trip an integer param reads as the wrong type.
func TestResolveGuardrails_AnIntegerParamValidates(t *testing.T) {
	gc := withPolicy(t, "limit-guardrail", `{"type":"object","properties":{
		"request":{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":10}}}}}`)
	_, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("limit-guardrail", map[string]any{
		"request": map[string]any{"limit": 3}})}, gc)

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailApplied {
		t.Fatalf("outcome = %+v, want applied", o)
	}
}

func TestResolveGuardrails_AParamOutsideItsBoundsIsInvalid(t *testing.T) {
	gc := withPolicy(t, "limit-guardrail", `{"type":"object","properties":{
		"request":{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":10}}}}}`)
	policies, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("limit-guardrail", map[string]any{
		"request": map[string]any{"limit": 0}})}, gc)

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailInvalid || !strings.Contains(o.Reason, "at least 1") {
		t.Fatalf("outcome = %+v, want invalid naming the bound", o)
	}
	if len(policies) != 0 {
		t.Fatalf("an out-of-bounds guardrail was written: %+v", policies)
	}
}

// The gateway compiles the pattern on every request and refuses the call when
// it cannot, so a pattern that does not compile would refuse every call.
func TestResolveGuardrails_ARegexThatDoesNotCompileIsInvalid(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("regex-guardrail", map[string]any{
		"request": map[string]any{"regex": "(casino", "invert": true}}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailInvalid || !strings.Contains(o.Reason, "regex") {
		t.Fatalf("outcome = %+v, want invalid naming the regex", o)
	}
	if len(policies) != 0 {
		t.Fatalf("an uncompilable pattern was written: %+v", policies)
	}
}

func TestResolveGuardrails_AMinimumAboveTheMaximumIsInvalid(t *testing.T) {
	gc := withPolicy(t, "range-guardrail", `{"type":"object","properties":{
		"request":{"type":"object","properties":{"min":{"type":"integer"},"max":{"type":"integer"}}}}}`)
	_, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("range-guardrail", map[string]any{
		"request": map[string]any{"min": 50, "max": 10}})}, gc)

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailInvalid || !strings.Contains(o.Reason, "min") {
		t.Fatalf("outcome = %+v, want invalid naming min and max", o)
	}
}

// A check that cannot run is not a pass: a schema AEP cannot read, or one
// using rules it cannot check, writes nothing.
func TestResolveGuardrails_AnUncheckableSchemaIsInvalid(t *testing.T) {
	for name, schema := range map[string]string{
		"unparseable":        `{"type":`,
		"an unknown keyword": `{"type":"object","oneOf":[{"required":["a"]}],"properties":{"a":{"type":"string"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			gc := withPolicy(t, "odd-guardrail", schema)
			policies, outcomes := resolveGuardrails([]delivery.GuardrailDeclaration{declare("odd-guardrail", map[string]any{"a": "x"})}, gc)
			if o := onlyOutcome(t, outcomes); o.Status != GuardrailInvalid {
				t.Fatalf("outcome = %+v, want invalid", o)
			}
			if len(policies) != 0 {
				t.Fatalf("an unchecked guardrail was written: %+v", policies)
			}
		})
	}
}

func TestResolveGuardrails_APolicyTheGatewayDoesNotOfferIsUnavailable(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("url-guardrail", map[string]any{}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailUnavailable {
		t.Fatalf("outcome = %+v", o)
	}
	if len(policies) != 0 {
		t.Fatalf("an unavailable guardrail was written: %+v", policies)
	}
}

// The catalog lists every gateway policy — rate limits and auth included. The
// spec may only switch on guardrails.
func TestResolveGuardrails_ANonGuardrailPolicyIsUnavailable(t *testing.T) {
	_, outcomes := resolve(t, "", declare("basic-ratelimit", map[string]any{"limits": []any{}}))

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailUnavailable {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestResolveGuardrails_AParamOfTheWrongTypeIsInvalid(t *testing.T) {
	policies, outcomes := resolve(t, "", declare("pii-masking-regex", map[string]any{"email": "yes"}))

	o := onlyOutcome(t, outcomes)
	if o.Status != GuardrailInvalid || !strings.Contains(o.Reason, "email") {
		t.Fatalf("outcome = %+v, want invalid naming the param", o)
	}
	if len(policies) != 0 {
		t.Fatalf("an invalid guardrail was written: %+v", policies)
	}
}

func TestResolveGuardrails_ANonAnthropicConnectionIsUnsupported(t *testing.T) {
	policies, outcomes := resolveGuardrails(
		[]delivery.GuardrailDeclaration{declare("pii-masking-regex", map[string]any{"email": true})},
		guardrailContext{catalog: testCatalog(t), format: modelconn.FormatOpenAICompatible})

	if o := onlyOutcome(t, outcomes); o.Status != GuardrailUnsupported {
		t.Fatalf("outcome = %+v", o)
	}
	if len(policies) != 0 {
		t.Fatalf("an unsupported guardrail was written: %+v", policies)
	}
}

// --- merge ---

func policy(name string) agentmanager.BindingPolicy {
	return agentmanager.BindingPolicy{Name: name, Version: "v1",
		Paths: []agentmanager.BindingPolicyPath{{Path: "/*", Methods: []string{"*"}, Params: map[string]any{"k": name}}}}
}

func names(ps []agentmanager.BindingPolicy) []string {
	out := []string{}
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestMergeGuardrails_KeepsAnOperatorsGuardrail(t *testing.T) {
	next, owned, conflicts, changed := mergeGuardrails(
		[]agentmanager.BindingPolicy{policy("url-guardrail")}, nil,
		[]agentmanager.BindingPolicy{policy("pii-masking-regex")})

	if got := names(next); !reflect.DeepEqual(got, []string{"url-guardrail", "pii-masking-regex"}) {
		t.Fatalf("next = %v", got)
	}
	if !reflect.DeepEqual(owned, []string{"pii-masking-regex"}) || len(conflicts) != 0 || !changed {
		t.Fatalf("owned=%v conflicts=%v changed=%v", owned, conflicts, changed)
	}
}

// Taking a guardrail out of the spec takes AEP's entry off the binding — and
// only AEP's.
func TestMergeGuardrails_RemovesItsOwnEntryWhenNoLongerDeclared(t *testing.T) {
	next, owned, _, changed := mergeGuardrails(
		[]agentmanager.BindingPolicy{policy("url-guardrail"), policy("pii-masking-regex")},
		[]string{"pii-masking-regex"}, nil)

	if got := names(next); !reflect.DeepEqual(got, []string{"url-guardrail"}) {
		t.Fatalf("next = %v", got)
	}
	if len(owned) != 0 || !changed {
		t.Fatalf("owned=%v changed=%v", owned, changed)
	}
}

// An operator added the same policy by hand. AEP does not overwrite their
// settings; it reports the clash.
func TestMergeGuardrails_ASameNamedOperatorGuardrailIsAConflict(t *testing.T) {
	operators := policy("pii-masking-regex")
	next, owned, conflicts, changed := mergeGuardrails(
		[]agentmanager.BindingPolicy{operators}, nil,
		[]agentmanager.BindingPolicy{policy("pii-masking-regex")})

	if !conflicts["pii-masking-regex"] || len(owned) != 0 || changed {
		t.Fatalf("owned=%v conflicts=%v changed=%v", owned, conflicts, changed)
	}
	if !reflect.DeepEqual(next, []agentmanager.BindingPolicy{operators}) {
		t.Fatalf("the operator's entry was touched: %+v", next)
	}
}

func TestMergeGuardrails_NothingToChangeIsNotAWrite(t *testing.T) {
	current := []agentmanager.BindingPolicy{policy("url-guardrail"), policy("pii-masking-regex")}
	_, _, _, changed := mergeGuardrails(current, []string{"pii-masking-regex"},
		[]agentmanager.BindingPolicy{policy("pii-masking-regex")})

	if changed {
		t.Fatal("an identical binding must not be written: each write redeploys the agent's proxy")
	}
}

// Agent Manager may hand entries back in another order. That is not a change,
// and must not cost a write — each one redeploys the agent's proxy.
func TestMergeGuardrails_AReorderedBindingIsNotAWrite(t *testing.T) {
	current := []agentmanager.BindingPolicy{policy("pii-masking-regex"), policy("url-guardrail")}
	_, _, _, changed := mergeGuardrails(current, []string{"pii-masking-regex"},
		[]agentmanager.BindingPolicy{policy("pii-masking-regex")})

	if changed {
		t.Fatal("a reordering of the same entries must not be written")
	}
}
