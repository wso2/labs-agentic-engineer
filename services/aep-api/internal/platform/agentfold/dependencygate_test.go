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
// overrides spliced in (a nil value deletes the key) — the same fixture shape
// the zod gate's dependency-design-gate.test.ts uses, so the two tables read
// alike.
func dep(overrides map[string]any) string {
	m := map[string]any{
		"name":        "payment-provider",
		"description": "Charges the customer for shipping.",
		"provider":    "Stripe",
		"style":       "rest-api",
		"contract":    "openapi.yaml",
		"config":      []any{map[string]any{"key": "PAYMENT_API_KEY", "secret": true}},
	}
	for k, v := range overrides {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func str(s string) *string { return &s }

// TestDependencyGate_Parity locks fold-parity with checkDependencyDesign: every
// case here has its twin in packages/agent-stream/test/dependency-design-gate.test.ts.
func TestDependencyGate_Parity(t *testing.T) {
	two := []any{map[string]any{"name": "a", "style": "sdk"}, map[string]any{"name": "b"}}
	cases := []struct {
		name    string
		content string
		prior   *string
		wantOK  bool
		wantMsg string
	}{
		{"resolved rest dependency", dep(nil), nil, true, ""},
		{"open suggestions, nothing chosen", dep(map[string]any{"provider": nil, "style": nil, "contract": nil, "config": nil, "suggestions": two}), nil, true, ""},
		{"a single suggestion is fine", dep(map[string]any{"provider": nil, "style": nil, "contract": nil, "config": nil, "suggestions": two[:1]}), nil, true, ""},
		{"the need alone", dep(map[string]any{"provider": nil, "style": nil, "contract": nil, "config": nil}), nil, true, ""},
		{"config before a provider", dep(map[string]any{"provider": nil, "style": nil, "contract": nil}), nil, false, "derived from the chosen service"},
		{"org copy carries config without a project provider", `{"name":"payment-provider","source":"org","config":[{"key":"K"}]}`, nil, true, ""},
		{"registered-org stub", `{"name":"payment-provider","source":"org"}`, nil, true, ""},
		{"provider chosen, no contract yet", dep(map[string]any{"contract": nil}), nil, true, ""},
		{"name not the directory", dep(map[string]any{"name": "stripe"}), nil, false, `"payment-provider"`},
		{"read-time status rejected", dep(map[string]any{"status": "resolved"}), nil, false, "unknown property status"},
		{"retired specPath rejected", dep(map[string]any{"specPath": "https://x"}), nil, false, "unknown property specPath"},
		{"retired candidates", dep(map[string]any{"provider": nil, "style": nil, "contract": nil, "config": nil, "candidates": two}), nil, false, `"candidates" is retired`},
		{"suggestions with a provider", dep(map[string]any{"suggestions": two}), nil, false, "never coexist"},
		{"suggestions with a style", dep(map[string]any{"provider": nil, "contract": nil, "config": nil, "suggestions": two}), nil, false, "stay unset"},
		{"contract not fitting the style", dep(map[string]any{"contract": "schema.graphql"}), nil, false, `for style "rest-api"`},
		{"graphql contract", dep(map[string]any{"style": "graphql", "contract": "schema.graphql"}), nil, true, ""},
		{"contract as a path", dep(map[string]any{"contract": "specs/x/openapi.yaml"}), nil, false, "not a path"},
		{"contract without a style", dep(map[string]any{"style": nil}), nil, false, `"style" is required`},
		{"sdk with manifest and slice", dep(map[string]any{"style": "sdk", "sdk": "sdk.json"}), nil, true, ""},
		{"sdk with manifest only", dep(map[string]any{"style": "sdk", "sdk": "sdk.json", "contract": nil}), nil, true, ""},
		{"sdk without manifest", dep(map[string]any{"style": "sdk", "contract": nil}), nil, false, "sdk.json"},
		{"sdk on a rest style", dep(map[string]any{"sdk": "sdk.json"}), nil, false, `only meaningful on style "sdk"`},
		{"sdk manifest misnamed", dep(map[string]any{"style": "sdk", "sdk": "manifest.json"}), nil, false, `"sdk.json"`},
		{"secret key with a default", dep(map[string]any{"config": []any{map[string]any{"key": "K", "secret": true, "defaultValue": "x"}}}), nil, false, "secret"},
		{"bad sha256", dep(map[string]any{"provenance": map[string]any{"sha256": "nope"}}), nil, false, "sha256"},
		{"good provenance", dep(map[string]any{"provenance": map[string]any{"sourceUrl": "https://x", "sha256": strings.Repeat("ab", 32), "sliced": true}}), nil, true, ""},
		// Types the zod schema pins — the fold must refuse the same shapes.
		{"secret not a boolean", dep(map[string]any{"config": []any{map[string]any{"key": "K", "secret": "yes"}}}), nil, false, "secret: must be a boolean"},
		{"defaultValue not a string", dep(map[string]any{"config": []any{map[string]any{"key": "K", "defaultValue": 5}}}), nil, false, "defaultValue: must be a string"},
		{"suggestion description not a string", dep(map[string]any{"provider": nil, "style": nil, "contract": nil, "config": nil, "suggestions": []any{map[string]any{"name": "a", "description": 1}}}), nil, false, "description: must be a string"},
		{"suggestion carries a package", dep(map[string]any{"provider": nil, "style": nil, "contract": nil, "config": nil, "suggestions": []any{map[string]any{"name": "a", "package": "npm:a"}}}), nil, false, "unknown property package"},
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

// The assumption record is the user's; the agent may echo it, never author it.
func TestDependencyGate_AssumptionEchoedNeverAuthored(t *testing.T) {
	assumed := map[string]any{"by": "admin", "at": "2026-09-08T10:15:00Z", "note": "auth guessed"}
	withAssumed := dep(map[string]any{"assumed": assumed})

	if p := validateDependencyDesign(withAssumed, "payment-provider", nil); p == nil || !strings.Contains(p.message, "permission record") {
		t.Fatalf("introducing the record on a create must be refused, got %v", p)
	}
	if p := validateDependencyDesign(withAssumed, "payment-provider", str(`{}`)); p == nil {
		t.Fatalf("introducing the record where the file has none must be refused")
	}
	edited := dep(map[string]any{"assumed": assumed, "description": "edited"})
	if p := validateDependencyDesign(edited, "payment-provider", str(withAssumed)); p != nil {
		t.Fatalf("echoing the record must pass, got %s", p.message)
	}
	altered := dep(map[string]any{"assumed": map[string]any{"by": "agent", "at": "2026-09-08T10:15:00Z", "note": "auth guessed"}})
	if p := validateDependencyDesign(altered, "payment-provider", str(withAssumed)); p == nil {
		t.Fatalf("altering the record must be refused")
	}
	if p := validateDependencyDesign(dep(nil), "payment-provider", str(withAssumed)); p != nil {
		t.Fatalf("dropping the record (a real contract replaced it) must pass, got %s", p.message)
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

// The user's record is the platform's: a write that leaves it out gets it back.
func TestDependencyGate_AssumptionPreservedThroughAWrite(t *testing.T) {
	assumed := map[string]any{"by": "admin", "at": "2026-09-08T10:15:00Z"}
	prior := dep(map[string]any{"assumed": assumed})
	restored := preserveAssumption(depPath, dep(map[string]any{"description": "edited"}), str(prior))
	var got map[string]any
	if err := json.Unmarshal([]byte(restored), &got); err != nil || got["assumed"] == nil || got["description"] != "edited" {
		t.Fatalf("record not put back: %s", restored)
	}
	if p := validateDependencyDesign(restored, "payment-provider", str(prior)); p != nil {
		t.Fatalf("the put-back record must read as an echo, got %s", p.message)
	}
	carried := dep(map[string]any{"assumed": assumed, "description": "edited"})
	if preserveAssumption(depPath, carried, str(prior)) != carried {
		t.Fatalf("a carried record must be left as written")
	}
	if preserveAssumption("specs/design/components/api/design.json", "{}", str(prior)) != "{}" {
		t.Fatalf("another path must be left alone")
	}
	if preserveAssumption(depPath, "{nope", str(prior)) != "{nope" || preserveAssumption(depPath, dep(nil), nil) != dep(nil) {
		t.Fatalf("an unparseable write and no prior must be left alone")
	}
	// Through the fold: the commit carries the record even when the agent's
	// content does not.
	// Through the fold, the way a wholesale re-emission goes — removeFile, then
	// addFile without the record: the commit still carries it.
	f := NewFromSnapshot(map[string]string{depPath: prior})
	ctx := context.Background()
	if res, err := f.RemoveFile(ctx, depPath); err != nil || res.Status != StatusApplied {
		t.Fatalf("remove: %+v %v", res, err)
	}
	res, err := f.AddFile(ctx, depPath, dep(map[string]any{"description": "edited"}))
	if err != nil || res.Status != StatusApplied {
		t.Fatalf("add: %+v %v", res, err)
	}
	if out := f.Touched()[depPath]; out == nil || !strings.Contains(*out, `"assumed"`) {
		t.Fatalf("committed content lost the record: %v", out)
	}
	// And an altered record on the re-add is still refused.
	f2 := NewFromSnapshot(map[string]string{depPath: prior})
	_, _ = f2.RemoveFile(ctx, depPath)
	if res, _ := f2.AddFile(ctx, depPath, dep(map[string]any{"assumed": map[string]any{"by": "agent", "at": "2026-09-08T10:15:00Z"}})); res.Status == StatusApplied {
		t.Fatalf("an altered record on a re-add must be refused")
	}
}
