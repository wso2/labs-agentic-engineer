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

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const depPath = "specs/design/dependencies/payment-provider/dependency.json"
const sdkPath = "specs/design/dependencies/payment-provider/sdk.json"

// dep builds a resolved REST dependency.json for dir "payment-provider" with
// overrides spliced in — top-level keys by name, resource-block keys with a
// "resource." prefix; a nil value deletes the key — the same fixture shape the
// zod gate's dependency-design-gate.test.ts uses, so the two tables read alike.
func dep(overrides map[string]any) string {
	res := map[string]any{
		"name":        "payment-provider",
		"description": "Charges the customer for shipping.",
		"provider":    "Stripe",
		"config":      []any{map[string]any{"key": "PAYMENT_API_KEY", "secret": true}},
		"contract":    map[string]any{"type": "openapi", "path": "openapi.yaml", "origin": "provider"},
	}
	m := map[string]any{"name": "payment-provider", "resource": res}
	for k, v := range overrides {
		target, key := m, k
		if strings.HasPrefix(k, "resource.") {
			target, key = res, strings.TrimPrefix(k, "resource.")
		}
		if v == nil {
			delete(target, key)
		} else {
			target[key] = v
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// contract is a contract object with overrides.
func contract(overrides map[string]any) map[string]any {
	c := map[string]any{"type": "openapi", "path": "openapi.yaml", "origin": "provider"}
	for k, v := range overrides {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	return c
}

func str(s string) *string { return &s }

// TestDependencyGate_Parity locks fold-parity with checkDependencyDesign: every
// case here has its twin in packages/agent-stream/test/dependency-design-gate.test.ts.
func TestDependencyGate_Parity(t *testing.T) {
	two := []any{map[string]any{"name": "a", "style": "sdk"}, map[string]any{"name": "b"}}
	open := map[string]any{"resource.provider": nil, "resource.contract": nil, "resource.config": nil}
	withOpen := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range open {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		name    string
		content string
		prior   *string
		wantOK  bool
		wantMsg string
	}{
		{"resolved rest dependency", dep(nil), nil, true, ""},
		{"open suggestions, nothing chosen", dep(withOpen(map[string]any{"suggestions": two})), nil, true, ""},
		{"a single suggestion is fine", dep(withOpen(map[string]any{"suggestions": two[:1]})), nil, true, ""},
		{"the need alone", dep(open), nil, true, ""},
		{"config before a provider", dep(map[string]any{"resource.provider": nil, "resource.contract": nil}), nil, false, "derived from the chosen service"},
		{"registry stub: the agent asks by name", `{"name":"payment-provider","resource":{"ref":"payment-provider","name":"payment-provider"}}`, nil, true, ""},
		{"a copy carries config without a project-chosen provider", `{"name":"payment-provider","resource":{"ref":"payment-provider","name":"payment-provider","config":[{"key":"K"}]}}`, nil, true, ""},
		{"ref not the dependency name", `{"name":"payment-provider","resource":{"ref":"stripe","name":"payment-provider"}}`, nil, false, `"resource.ref" must equal`},
		{"provider chosen, no contract yet", dep(map[string]any{"resource.contract": nil}), nil, true, ""},
		{"name not the directory", dep(map[string]any{"name": "stripe", "resource.name": "stripe"}), nil, false, `"payment-provider"`},
		{"resource.name not the dependency name", dep(map[string]any{"resource.name": "stripe"}), nil, false, `"resource.name" must equal`},
		{"no resource block", `{"name":"payment-provider"}`, nil, false, `"resource" is required`},
		{"read-time status rejected", dep(map[string]any{"status": "resolved"}), nil, false, "unknown property status"},
		{"retired specPath rejected", dep(map[string]any{"specPath": "https://x"}), nil, false, "unknown property specPath"},
		{"retired flat provider", dep(map[string]any{"provider": "Stripe"}), nil, false, `"provider" is not a top-level field`},
		{"retired flat style", dep(map[string]any{"style": "rest-api"}), nil, false, `"style" is not a top-level field`},
		{"retired flat source", dep(map[string]any{"source": "org"}), nil, false, `"source" is not a top-level field`},
		{"retired candidates", dep(withOpen(map[string]any{"candidates": two})), nil, false, `"candidates" is not a top-level field`},
		{"suggestions with a provider", dep(map[string]any{"suggestions": two}), nil, false, "never coexist"},
		{"suggestions with a contract", dep(map[string]any{"resource.provider": nil, "resource.config": nil, "suggestions": two}), nil, false, "stay unset"},
		{"contract path not fitting the type", dep(map[string]any{"resource.contract": contract(map[string]any{"path": "schema.graphql"})}), nil, false, `for type "openapi"`},
		{"graphql contract", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "graphql", "path": "schema.graphql"})}), nil, true, ""},
		{"contract path as a path", dep(map[string]any{"resource.contract": contract(map[string]any{"path": "specs/x/openapi.yaml"})}), nil, false, "not a path"},
		{"contract without a type", dep(map[string]any{"resource.contract": contract(map[string]any{"type": nil})}), nil, false, "resource.contract.type"},
		{"contract with a url", dep(map[string]any{"resource.contract": map[string]any{"type": "openapi", "url": "https://x/openapi.yaml"}}), nil, false, `no "url" form`},
		{"unknown contract type", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "asyncapi", "path": "asyncapi.yaml"})}), nil, false, "not an allowed value"},
		{"bad origin", dep(map[string]any{"resource.contract": contract(map[string]any{"origin": "guessed"})}), nil, false, "origin"},
		{"sdk contract is the manifest", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "sdk", "path": "sdk.json"})}), nil, true, ""},
		{"sdk contract misnamed", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "sdk", "path": "manifest.json"})}), nil, false, `"sdk.json"`},
		{"secret key with a default", dep(map[string]any{"resource.config": []any{map[string]any{"key": "K", "secret": true, "defaultValue": "x"}}}), nil, false, "secret"},
		{"bad sha256", dep(map[string]any{"provenance": map[string]any{"sha256": "nope"}}), nil, false, "sha256"},
		{"good provenance", dep(map[string]any{"provenance": map[string]any{"sourceUrl": "https://x", "sha256": strings.Repeat("ab", 32), "readOn": "2026-09-17"}}), nil, true, ""},
		{"registry provenance", dep(map[string]any{"provenance": map[string]any{"registry": "payment-provider/openapi.yaml", "sha256": strings.Repeat("ab", 32)}}), nil, true, ""},
		{"registry provenance as a url", dep(map[string]any{"provenance": map[string]any{"registry": "https://x/openapi.yaml"}}), nil, false, "never a URL"},
		{"retired provenance.sliced", dep(map[string]any{"provenance": map[string]any{"sliced": true}}), nil, false, "sliced is retired"},
		{"retired provenance.fetchedAt", dep(map[string]any{"provenance": map[string]any{"fetchedAt": "x"}}), nil, false, "fetchedAt is retired"},
		// Types the zod schema pins — the fold must refuse the same shapes.
		{"secret not a boolean", dep(map[string]any{"resource.config": []any{map[string]any{"key": "K", "secret": "yes"}}}), nil, false, "secret: must be a boolean"},
		{"defaultValue not a string", dep(map[string]any{"resource.config": []any{map[string]any{"key": "K", "defaultValue": 5}}}), nil, false, "defaultValue: must be a string"},
		{"suggestion description not a string", dep(withOpen(map[string]any{"suggestions": []any{map[string]any{"name": "a", "description": 1}}})), nil, false, "description: must be a string"},
		{"suggestion carries a package", dep(withOpen(map[string]any{"suggestions": []any{map[string]any{"name": "a", "package": "npm:a"}}})), nil, false, "unknown property package"},
		{"unknown resource property", dep(map[string]any{"resource.style": "rest-api"}), nil, false, "resource: unknown property style"},
		// zod's optional() never admits null; the fold refuses the same.
		{"null provenance", `{"name":"payment-provider","resource":{"name":"payment-provider"},"provenance":null}`, nil, false, "must not be null"},
		{"null suggestions", `{"name":"payment-provider","resource":{"name":"payment-provider"},"suggestions":null}`, nil, false, "must not be null"},
		{"null resource contract", `{"name":"payment-provider","resource":{"name":"payment-provider","provider":"Stripe","contract":null}}`, nil, false, "must not be null"},
		{"absent accepted is fine", dep(map[string]any{"resource.contract": contract(map[string]any{"accepted": nil})}), nil, true, ""},
		{"null accepted", `{"name":"payment-provider","resource":{"name":"payment-provider","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","accepted":null}}}`, nil, false, "must not be null"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validateDependencyDesign(c.content, "payment-provider", c.prior)
			if c.wantOK && p != nil {
				t.Fatalf("want accepted, got rejected: %s", p.message)
			}
			if !c.wantOK {
				if p == nil {
					t.Fatalf("want rejected, got accepted")
				}
				if !strings.Contains(p.message, c.wantMsg) {
					t.Fatalf("message %q does not contain %q", p.message, c.wantMsg)
				}
			}
		})
	}
}

// The acceptance record is the user's and the instructions are the
// organization's; the agent may echo either, never author it.
func TestDependencyGate_PlatformFieldsEchoedNeverAuthored(t *testing.T) {
	accepted := map[string]any{"by": "admin", "at": "2026-09-08T10:15:00Z", "note": "auth guessed"}
	withAccepted := dep(map[string]any{"resource.contract": contract(map[string]any{"origin": "assumed", "accepted": accepted})})

	if p := validateDependencyDesign(withAccepted, "payment-provider", nil); p == nil || !strings.Contains(p.message, "permission record") {
		t.Fatalf("introducing the record on a create must be refused, got %v", p)
	}
	if p := validateDependencyDesign(withAccepted, "payment-provider", str(`{}`)); p == nil {
		t.Fatalf("introducing the record where the file has none must be refused")
	}
	edited := dep(map[string]any{"resource.contract": contract(map[string]any{"origin": "assumed", "accepted": accepted}), "resource.description": "edited"})
	if p := validateDependencyDesign(edited, "payment-provider", str(withAccepted)); p != nil {
		t.Fatalf("echoing the record must pass, got %s", p.message)
	}
	altered := dep(map[string]any{"resource.contract": contract(map[string]any{"origin": "assumed", "accepted": map[string]any{"by": "agent", "at": "2026-09-08T10:15:00Z", "note": "auth guessed"}})})
	if p := validateDependencyDesign(altered, "payment-provider", str(withAccepted)); p == nil {
		t.Fatalf("altering the record must be refused")
	}
	if p := validateDependencyDesign(dep(nil), "payment-provider", str(withAccepted)); p != nil {
		t.Fatalf("dropping the record (a real contract replaced it) must pass, got %s", p.message)
	}

	// The organization's instructions ride only on the platform's copy.
	withInstructions := dep(map[string]any{"resource.ref": "payment-provider", "resource.consumptionInstructions": "Never log the key."})
	if p := validateDependencyDesign(withInstructions, "payment-provider", nil); p == nil || !strings.Contains(p.message, "consumptionInstructions") {
		t.Fatalf("authoring the instructions must be refused, got %v", p)
	}
	if p := validateDependencyDesign(withInstructions, "payment-provider", str(withInstructions)); p != nil {
		t.Fatalf("echoing the instructions must pass, got %s", p.message)
	}
}

func TestDependencyGate_SdkManifest(t *testing.T) {
	ok := `{"packages":{"typescript":"npm:stripe@^14","go":"go:github.com/stripe/stripe-go/v79"},"calls":["paymentIntents.create"]}`
	if p := validateSdkManifest(ok); p != nil {
		t.Fatalf("want accepted, got %s", p.message)
	}
	for name, c := range map[string]struct{ content, want string }{
		"empty packages":  {`{"packages":{}}`, "at least one"},
		"upper-case lang": {`{"packages":{"TypeScript":"npm:stripe"}}`, "lower-case"},
		"no ecosystem":    {`{"packages":{"go":"stripe-go"}}`, "ecosystem-prefixed"},
		"unknown key":     {`{"packages":{"go":"go:x"},"version":"1"}`, "unknown property version"},
		"docsUrl type":    {`{"packages":{"go":"go:x"},"docsUrl":3}`, "docsUrl: must be a string"},
		"assumed type":    {`{"packages":{"go":"go:x"},"assumed":"yes"}`, "assumed: must be a boolean"},
		"derived type":    {`{"packages":{"go":"go:x"},"derived":"yes"}`, "derived: must be a boolean"},
	} {
		t.Run(name, func(t *testing.T) {
			p := validateSdkManifest(c.content)
			if p == nil || !strings.Contains(p.message, c.want) {
				t.Fatalf("want rejection containing %q, got %v", c.want, p)
			}
		})
	}
	if code, _ := checkDependencyDesignGuard(sdkPath, "{nope", nil); code != ErrInvalidJSON {
		t.Fatalf("invalid JSON must be ErrInvalidJSON, got %q", code)
	}
	if code, _ := checkDependencyDesignGuard(depPath, dep(nil), nil); code != "" {
		t.Fatalf("a valid dependency.json must fold, got %q", code)
	}
	if code, _ := checkDependencyDesignGuard("specs/design/dependencies/payment-provider/openapi.yaml", "openapi: 3", nil); code != "" {
		t.Fatalf("a contract file is not this gate's, got %q", code)
	}
}

// The platform's fields ride through a write that leaves them out: the copy's
// ref and the organization's instructions, and the user's acceptance record.
func TestDependencyGate_PlatformFieldsPreservedThroughAWrite(t *testing.T) {
	accepted := map[string]any{"by": "admin", "at": "2026-09-08T10:15:00Z"}
	prior := dep(map[string]any{"resource.ref": "payment-provider", "resource.consumptionInstructions": "Never log the key.", "resource.contract": contract(map[string]any{"origin": "assumed", "accepted": accepted})})
	restored := preservePlatformFields(depPath, dep(map[string]any{"resource.ref": "payment-provider", "resource.description": "edited", "resource.contract": contract(map[string]any{"origin": "assumed"})}), str(prior))
	var got map[string]any
	if err := json.Unmarshal([]byte(restored), &got); err != nil {
		t.Fatalf("restored not JSON: %s", restored)
	}
	res, _ := got["resource"].(map[string]any)
	if res["ref"] != "payment-provider" || res["consumptionInstructions"] != "Never log the key." || res["description"] != "edited" {
		t.Fatalf("ref/instructions not put back: %s", restored)
	}
	if c, _ := res["contract"].(map[string]any); c["accepted"] == nil {
		t.Fatalf("acceptance not put back: %s", restored)
	}
	if p := validateDependencyDesign(restored, "payment-provider", str(prior)); p != nil {
		t.Fatalf("the put-back fields must read as echoes, got %s", p.message)
	}
	carried := dep(map[string]any{"resource.ref": "payment-provider", "resource.consumptionInstructions": "Never log the key.", "resource.contract": contract(map[string]any{"origin": "assumed", "accepted": accepted}), "resource.description": "edited"})
	if preservePlatformFields(depPath, carried, str(prior)) != carried {
		t.Fatalf("carried fields must be left as written")
	}
	// A write that DROPS the ref is the agent replacing the copy with a
	// resource the project defines: neither the ref nor the organization's
	// instructions come back (the acceptance record still does).
	dropped := preservePlatformFields(depPath, dep(map[string]any{"resource.provider": "Adyen", "resource.contract": contract(map[string]any{"origin": "assumed"})}), str(prior))
	var got2 map[string]any
	_ = json.Unmarshal([]byte(dropped), &got2)
	droppedRes, _ := got2["resource"].(map[string]any)
	if _, has := droppedRes["ref"]; has {
		t.Fatalf("a dropped ref must stay dropped: %s", dropped)
	}
	if _, has := droppedRes["consumptionInstructions"]; has {
		t.Fatalf("instructions must not come back onto an inline resource: %s", dropped)
	}
	if c2, _ := droppedRes["contract"].(map[string]any); c2["accepted"] == nil {
		t.Fatalf("the acceptance record still rides through: %s", dropped)
	}
	if preservePlatformFields("specs/design/components/api/design.json", "{}", str(prior)) != "{}" {
		t.Fatalf("another path must be left alone")
	}
	if preservePlatformFields(depPath, "{nope", str(prior)) != "{nope" || preservePlatformFields(depPath, dep(nil), nil) != dep(nil) {
		t.Fatalf("an unparseable write and no prior must be left alone")
	}
	// Through the fold, the way a wholesale re-emission goes — removeFile, then
	// addFile without the record: the commit still carries it.
	f := NewFromSnapshot(map[string]string{depPath: prior})
	ctx := context.Background()
	if res, err := f.RemoveFile(ctx, depPath); err != nil || res.Status != StatusApplied {
		t.Fatalf("remove: %+v %v", res, err)
	}
	res2, err := f.AddFile(ctx, depPath, dep(map[string]any{"resource.ref": "payment-provider", "resource.description": "edited", "resource.contract": contract(map[string]any{"origin": "assumed"})}))
	if err != nil || res2.Status != StatusApplied {
		t.Fatalf("add: %+v %v", res2, err)
	}
	if out := f.Touched()[depPath]; out == nil || !strings.Contains(*out, `"accepted"`) || !strings.Contains(*out, `"consumptionInstructions"`) {
		t.Fatalf("committed content lost the platform fields: %v", out)
	}
	// And an altered record on the re-add is still refused.
	f2 := NewFromSnapshot(map[string]string{depPath: prior})
	_, _ = f2.RemoveFile(ctx, depPath)
	if res, _ := f2.AddFile(ctx, depPath, dep(map[string]any{"resource.contract": contract(map[string]any{"origin": "assumed", "accepted": map[string]any{"by": "agent", "at": "2026-09-08T10:15:00Z"}})})); res.Status == StatusApplied {
		t.Fatalf("an altered record on a re-add must be refused")
	}
}

// A file written before the nested shape carries its acceptance at the top
// level as `assumed`. The fold calls preservePlatformFields on the RAW prior,
// so it lifts that too — otherwise the first nested write over a flat file
// silently drops what the user accepted.
func TestPreservePlatformFields_LiftsAFlatPriorsAcceptance(t *testing.T) {
	const path = "specs/design/dependencies/payment-provider/dependency.json"
	flatPrior := `{"name":"payment-provider","provider":"Stripe","style":"rest-api","contract":"openapi.yaml","assumed":{"by":"admin","at":"2026-09-08T10:15:00Z","note":"proceed"}}`
	write := `{"name":"payment-provider","resource":{"name":"payment-provider","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","origin":"assumed"}}}`

	out := preservePlatformFields(path, write, &flatPrior)
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, out)
	}
	res, _ := got["resource"].(map[string]any)
	contract, _ := res["contract"].(map[string]any)
	accepted, _ := contract["accepted"].(map[string]any)
	if accepted["by"] != "admin" || accepted["note"] != "proceed" {
		t.Fatalf("the user's acceptance must survive the shape change: %s", out)
	}

	// A flat prior with nothing accepted leaves the write exactly as written.
	plainFlat := `{"name":"payment-provider","provider":"Stripe","contract":"openapi.yaml"}`
	if out := preservePlatformFields(path, write, &plainFlat); out != write {
		t.Fatalf("nothing to put back must leave the write alone:\n%s", out)
	}
}
