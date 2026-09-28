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
	_ "embed"
	"strings"
	"testing"
)

// validAfm is byte-identical to VALID in
// packages/agent-stream/test/agent-afm-gate.test.ts.
const validAfm = `---
spec_version: "0.4.0"
name: "lunch-agent"
description: "Helps a teammate order lunch."
max_iterations: 12
model:
  provider: "anthropic"
  name: "${env:MODEL_NAME}"
  url: "${env:MODEL_ENDPOINT}"
  authentication:
    type: "api-key"
    api_key: "${env:MODEL_API_KEY}"
interfaces:
  - type: webchat
x-aep:
  tools:
    openapi:
      - component: "lunch-api"
        baseUrl: "${env:LUNCH_API_URL}"
        allow: [addItem]
---

# Role

You help teammates order lunch.

# Instructions

- Confirm before adding anything.
`

// liveAfmFixture is a real agent.afm.md the platform's own design flow
// produced (the lunch-design playground project's lunch-chat-agent), kept as
// testdata because playground/.projects is not tracked. A real document rather
// than one written for this test is the point: this gate must never reject the
// platform's own output. Embedded, so a fixture edit invalidates a cached PASS.
//
//go:embed testdata/lunch-chat-agent.afm.md
var liveAfmFixture string

// TestValidateAgentAfm_LiveFixture guards the class of bug a struct-plus-
// KnownFields decode produced: it hard-rejected x-aep.memory, x-aep.identity
// and interfaces[].exposure, which the platform's own AFM generator emits
// (see liveAfmFixture).
func TestValidateAgentAfm_LiveFixture(t *testing.T) {
	if problem := validateAgentAfm(liveAfmFixture, "lunch-chat-agent"); problem != nil {
		t.Fatalf("want the live fixture accepted — it exercises x-aep.memory, x-aep.identity "+
			"and interfaces[].exposure, all real optional zod fields — got %q", problem.message)
	}
}

func TestValidateAgentAfm(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(string) string
		dirName string
		wantErr string // substring; "" means valid
	}{
		{"valid", func(s string) string { return s }, "lunch-agent", ""},
		{"missing provider", func(s string) string {
			return strings.Replace(s, "  provider: \"anthropic\"\n", "", 1)
		}, "lunch-agent", "provider"},
		{"literal credential", func(s string) string {
			return strings.Replace(s, "\"${env:MODEL_API_KEY}\"", "\"sk-ant-real\"", 1)
		}, "lunch-agent", "${env:...}"},
		{"unsupported interface", func(s string) string {
			return strings.Replace(s, "type: webchat", "type: webhook", 1)
		}, "lunch-agent", "webchat"},
		{"name mismatch", func(s string) string { return s }, "other-agent", "directory name"},
		{"no role section", func(s string) string {
			return strings.Replace(s, "# Role", "# Purpose", 1)
		}, "lunch-agent", "# Role"},
		{"memory type server is accepted", func(s string) string {
			return strings.Replace(s, "        allow: [addItem]\n", "        allow: [addItem]\n  memory:\n    type: \"server\"\n", 1)
		}, "lunch-agent", ""},
		{"memory type shared is rejected", func(s string) string {
			return strings.Replace(s, "        allow: [addItem]\n", "        allow: [addItem]\n  memory:\n    type: \"shared\"\n", 1)
		}, "lunch-agent", "x-aep.memory.type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			problem := validateAgentAfm(tc.mutate(validAfm), tc.dirName)
			if tc.wantErr == "" {
				if problem != nil {
					t.Fatalf("want valid, got %q", problem.message)
				}
				return
			}
			if problem == nil {
				t.Fatalf("want error containing %q, got valid", tc.wantErr)
			}
			if !strings.Contains(problem.message, tc.wantErr) {
				t.Errorf("message %q does not contain %q", problem.message, tc.wantErr)
			}
		})
	}
}
