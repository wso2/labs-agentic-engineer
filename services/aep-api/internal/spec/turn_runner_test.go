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

import "testing"

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
