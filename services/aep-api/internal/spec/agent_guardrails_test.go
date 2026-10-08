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

package spec

import (
	"reflect"
	"strings"
	"testing"
)

const guardrailAFM = `---
spec_version: "0.4.0"
name: "receipt-agent"
description: "Reads receipts."
x-aep:
  guardrails:
    - policy: pii-masking-regex
      params: { email: true, phone: true }
      why: "The model never needs contact details."
    - policy: regex-guardrail
      params:
        request: { regex: "(?i)casino", invert: true }
      why: "Not a business expense."
---

# Role
Reads receipts.
`

func TestAgentGuardrails_ReadsEveryDeclaredGuardrailAndTheInstructions(t *testing.T) {
	got := AgentGuardrails(guardrailAFM)
	want := []AgentGuardrail{
		{Policy: "pii-masking-regex", Params: map[string]any{"email": true, "phone": true}, Why: "The model never needs contact details."},
		{Policy: "regex-guardrail", Params: map[string]any{"request": map[string]any{"regex": "(?i)casino", "invert": true}}, Why: "Not a business expense."},
	}
	if !got.Readable || !reflect.DeepEqual(got.Guardrails, want) {
		t.Fatalf("AgentGuardrails =\n %#v\nwant readable with\n %#v", got, want)
	}
	if strings.TrimSpace(got.Instructions) != "# Role\nReads receipts." {
		t.Errorf("instructions = %q", got.Instructions)
	}
}

func TestAgentGuardrails_NoneDeclared(t *testing.T) {
	got := AgentGuardrails("---\nname: \"a\"\nx-aep:\n  memory:\n    type: server\n---\n# Role\n")
	if !got.Readable || got.Guardrails != nil {
		t.Fatalf("want a readable document declaring none, got %#v", got)
	}
}

// The write gate guarantees a committed document's shape, so a malformed entry
// is skipped rather than failing the deploy that reads it.
func TestAgentGuardrails_SkipsAnEntryWithoutAPolicy(t *testing.T) {
	got := AgentGuardrails("---\nname: \"a\"\nx-aep:\n  guardrails:\n    - params: { email: true }\n      why: \"x\"\n---\n# Role\n")
	if !got.Readable || len(got.Guardrails) != 0 {
		t.Fatalf("want the entry skipped, got %#v", got)
	}
}

// Unreadable is not "declares nothing": a deploy that took it for that would
// strip every guardrail the agent has.
func TestAgentGuardrails_AnUnreadableDocumentSaysSo(t *testing.T) {
	for name, afm := range map[string]string{
		"no front matter": "# Role\nno front matter\n",
		"bad yaml":        "---\nx-aep: [unclosed\n---\n# Role\n",
		"empty":           "",
	} {
		t.Run(name, func(t *testing.T) {
			if got := AgentGuardrails(afm); got.Readable {
				t.Fatalf("want unreadable, got %#v", got)
			}
		})
	}
}

// Where a request-side guardrail can read the user's message depends on the
// agent's shape, so the parser reports it.
func TestAgentGuardrails_ReportsWhetherTheAgentUsesToolsOrTakesFiles(t *testing.T) {
	plain := AgentGuardrails("---\nname: \"a\"\nx-aep:\n  memory:\n    type: server\n---\n# Role\n")
	if plain.UsesTools || plain.TakesFiles {
		t.Fatalf("plain agent = %+v, want neither", plain)
	}
	shaped := AgentGuardrails("---\nname: \"a\"\nx-aep:\n  tools:\n    openapi:\n      - component: api\n        allow: [listX]\n  attachments:\n    types: [image/png]\n    maxFiles: 1\n    maxFileSizeMB: 5\n---\n# Role\n")
	if !shaped.UsesTools || !shaped.TakesFiles {
		t.Fatalf("agent with tools and attachments = %+v, want both", shaped)
	}
}
