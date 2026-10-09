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
	"strings"
	"testing"
	"unicode/utf16"
)

// TestDesignOrCollabTurn pins the shared gate both mcpForTurn and the
// dispatched TurnRequest.WebSearch flag key off: a design-flow turn or any
// collab room-scoped turn attaches; a plain chat turn with no room does not.
func TestDesignOrCollabTurn(t *testing.T) {
	cases := []struct {
		name string
		job  turnJob
		want bool
	}{
		{"design flow, no room", turnJob{flow: "design"}, true},
		{"start flow, collab room", turnJob{flow: "start", collabRoomID: "spec-o-p"}, true},
		{"no flow, collab room", turnJob{collabRoomID: "spec-o-p"}, true},
		{"start flow, no room", turnJob{flow: "start"}, false},
		{"no flow, no room", turnJob{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := designOrCollabTurn(tc.job); got != tc.want {
				t.Errorf("designOrCollabTurn(%+v) = %v, want %v", tc.job, got, tc.want)
			}
		})
	}
}

// TestCatalogTurn pins the MCP discovery gate: everything designOrCollabTurn
// admits, plus the requirements flows without a room — the interview records
// a Registered External resource as a given, so it needs the catalog wherever
// it runs. A plain chat turn with no room still gets nothing.
func TestCatalogTurn(t *testing.T) {
	cases := []struct {
		name string
		job  turnJob
		want bool
	}{
		{"design flow, no room", turnJob{flow: "design"}, true},
		{"no flow, collab room", turnJob{collabRoomID: "spec-o-p"}, true},
		{"start flow, no room", turnJob{flow: "start"}, true},
		{"amend flow, no room", turnJob{flow: "amend"}, true},
		{"settle flow, no room", turnJob{flow: "settle"}, true},
		{"interview flow, no room", turnJob{flow: "interview"}, true},
		{"refine flow, no room", turnJob{flow: "refine"}, true},
		{"chat, no room", turnJob{flow: "chat"}, false},
		{"no flow, no room", turnJob{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := catalogTurn(tc.job); got != tc.want {
				t.Errorf("catalogTurn(%+v) = %v, want %v", tc.job, got, tc.want)
			}
		})
	}
}

// capOutcome holds a stored outcome to the wire's 400 UTF-16 units (the agents
// service counts JS string length), cutting to 399 + "…" without splitting a
// surrogate pair.
func TestCapOutcome(t *testing.T) {
	units := func(s string) int { return len(utf16.Encode([]rune(s))) }
	if got := capOutcome("Filed #12."); got != "Filed #12." {
		t.Fatalf("short outcome changed: %q", got)
	}
	exact := strings.Repeat("x", 398) + "😀" // 400 units
	if got := capOutcome(exact); got != exact {
		t.Fatalf("a 400-unit outcome must pass unchanged, got %d units", units(got))
	}
	long := strings.Repeat("é", 450)
	if got := capOutcome(long); units(got) != 400 || got != strings.Repeat("é", 399)+"…" {
		t.Fatalf("capOutcome(450 é) = %d units", units(got))
	}
	// The 399th unit is the high half of an emoji: the emoji goes whole.
	emoji := strings.Repeat("x", 398) + strings.Repeat("😀", 5)
	if got := capOutcome(emoji); got != strings.Repeat("x", 398)+"…" {
		t.Fatalf("capOutcome split a surrogate pair: %q", got[len(got)-8:])
	}
}
