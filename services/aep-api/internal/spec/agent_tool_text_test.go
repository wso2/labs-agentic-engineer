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
	"slices"
	"testing"
)

const toolAgentAFM = `---
name: "expense-helper"
x-aep:
  tools:
    openapi:
      - component: expense-api
        allow: [logExpense]
---
Help the employee file an expense.
`

const expenseContract = `openapi: 3.0.3
info: { title: expenses, version: "1" }
paths:
  /expenses:
    post:
      operationId: logExpense
      summary: Log a receipt against the trip
      description: Records a casino or restaurant receipt.
      parameters:
        - name: tripId
          in: query
          description: the trip the receipt belongs to
          schema: { type: string }
      responses: { "201": { description: created } }
  /expenses/{id}:
    delete:
      operationId: deleteExpense
      summary: Remove a gambling entry
      responses: { "204": { description: gone } }
`

// The text a tool-using agent sends with every request is its tool
// definitions, built from the provider's contract: each ALLOWED operation's
// name, summary, description and parameters. An operation the agent may not
// call is never a tool, so its words are not in any request.
func TestAgentToolText_CollectsTheAllowedOperationsFromTheContract(t *testing.T) {
	got := AgentToolText(toolAgentAFM, []DesignComponent{
		{Name: "expense-helper", ComponentType: ComponentTypeAIAgent, AgentAFM: toolAgentAFM},
		{Name: "expense-api", OpenAPISpec: expenseContract},
	})
	for _, want := range []string{"logExpense", "Log a receipt against the trip", "Records a casino or restaurant receipt.",
		"tripId", "the trip the receipt belongs to"} {
		if !slices.Contains(got, want) {
			t.Errorf("tool text %q lacks %q", got, want)
		}
	}
	for _, unwanted := range []string{"deleteExpense", "Remove a gambling entry"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("tool text %q carries %q, an operation the agent may not call", got, unwanted)
		}
	}
}

func TestAgentToolText_NoToolsOrNoContractIsNothing(t *testing.T) {
	if got := AgentToolText(guardrailAFM, nil); len(got) != 0 {
		t.Errorf("an agent with no tools: %q, want nothing", got)
	}
	if got := AgentToolText(toolAgentAFM, []DesignComponent{{Name: "expense-api"}}); len(got) != 0 {
		t.Errorf("a provider with no contract in the design: %q, want nothing", got)
	}
}
