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

package genaiturns

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// The prototype feedback rules are the kit's (@wso2/prototype-kit/feedback),
// which the agents service and the console import. This BFF and the contract
// mirror them; these tests hold the mirror to the one table every side
// asserts, packages/prototype-kit/test/fixtures/feedback-cases.json (the kit's
// test/feedback.test.ts, @aep/agent-stream's test/turn-spec.test.ts).

// genaiturns → spec → internal → aep-api → services → repo root.
const feedbackCasesPath = "../../../../../packages/prototype-kit/test/fixtures/feedback-cases.json"

type feedbackCase struct {
	Name         string         `json:"name"`
	Valid        bool           `json:"valid"`
	Batch        map[string]any `json:"batch"`
	Request      map[string]any `json:"request"`
	Fill         *feedbackFill  `json:"fill"`
	RequestCount *int           `json:"requestCount"`
}

type feedbackFill struct {
	Field string `json:"field"`
	Unit  string `json:"unit"`
	Count int    `json:"count"`
}

type feedbackTable struct {
	Limits struct {
		Requests int `json:"requests"`
		Text     int `json:"text"`
		ID       int `json:"id"`
	} `json:"limits"`
	Base  json.RawMessage `json:"base"`
	Cases []feedbackCase  `json:"cases"`
}

func readFeedbackTable(t *testing.T) feedbackTable {
	t.Helper()
	raw, err := os.ReadFile(feedbackCasesPath)
	if err != nil {
		t.Fatalf("read the kit's feedback table (%s) — layout drift?: %v", feedbackCasesPath, err)
	}
	var table feedbackTable
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatalf("parse the feedback table: %v", err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("the shared feedback table is empty")
	}
	return table
}

// batchFor builds a row's batch from the table's base, as the kit's
// test/feedback-cases.ts does: `request` overrides the first request (null
// removes the field), `fill` repeats a unit into one field, `requestCount`
// repeats the first request, `batch` overrides top-level fields.
func batchFor(t *testing.T, base json.RawMessage, c feedbackCase) []byte {
	t.Helper()
	var batch map[string]any
	if err := json.Unmarshal(base, &batch); err != nil {
		t.Fatalf("parse the base batch: %v", err)
	}
	requests := batch["requests"].([]any)
	first := requests[0].(map[string]any)
	for key, value := range c.Request {
		if value == nil {
			delete(first, key)
		} else {
			first[key] = value
		}
	}
	if c.Fill != nil {
		value := strings.Repeat(c.Fill.Unit, c.Fill.Count)
		switch c.Fill.Field {
		case "component":
			batch["component"] = value
		case "elementIds":
			first["elementIds"] = []any{value}
		default:
			first[c.Fill.Field] = value
		}
	}
	if c.RequestCount != nil {
		many := make([]any, *c.RequestCount)
		for i := range many {
			many[i] = first
		}
		batch["requests"] = many
	}
	for key, value := range c.Batch {
		batch[key] = value
	}
	out, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal the batch: %v", err)
	}
	return out
}

func TestFeedbackCasesSharedWithKit(t *testing.T) {
	table := readFeedbackTable(t)
	if table.Limits.Requests != maxFeedbackRequests || table.Limits.Text != maxFeedbackText || table.Limits.ID != maxFeedbackID {
		t.Fatalf("limits %+v differ from this package's (%d requests, %d text, %d id)", table.Limits, maxFeedbackRequests, maxFeedbackText, maxFeedbackID)
	}
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			// A batch the request body cannot even decode into is a 400 too.
			var input gen.PrototypeFeedbackInput
			valid := json.Unmarshal(batchFor(t, table.Base, c), &input) == nil
			if valid {
				_, err := prototypeFeedbackFromJSON("/prototype", true, false, &input)
				valid = err == nil
			}
			if valid != c.Valid {
				t.Fatalf("valid = %v, want %v", valid, c.Valid)
			}
		})
	}
}

// The contract states the same ceilings as the kit and this package.
func TestFeedbackContractStatesTheKitsLimits(t *testing.T) {
	table := readFeedbackTable(t)
	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatalf("load the embedded contract: %v", err)
	}
	input := spec.Components.Schemas["PrototypeFeedbackInput"].Value
	request := spec.Components.Schemas["PrototypeFeedbackRequest"].Value
	limit := func(name string, got *uint64, want int) {
		t.Helper()
		if got == nil || int(*got) != want {
			t.Errorf("%s = %v, want %d", name, got, want)
		}
	}
	limit("PrototypeFeedbackInput.requests maxItems", input.Properties["requests"].Value.MaxItems, table.Limits.Requests)
	limit("PrototypeFeedbackInput.component maxLength", input.Properties["component"].Value.MaxLength, table.Limits.ID)
	limit("PrototypeFeedbackRequest.text maxLength", request.Properties["text"].Value.MaxLength, table.Limits.Text)
	for _, id := range []string{"screenId", "flowId", "roleId", "stateId"} {
		limit("PrototypeFeedbackRequest."+id+" maxLength", request.Properties[id].Value.MaxLength, table.Limits.ID)
	}
	limit("PrototypeFeedbackRequest.elementIds items maxLength", request.Properties["elementIds"].Value.Items.Value.MaxLength, table.Limits.ID)
}

func TestUTF16Len(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{{"", 0}, {"abc", 3}, {"Café", 4}, {"😀", 2}, {"a😀b", 4}, {"𐍈", 2}} {
		if got := utf16Len(c.in); got != c.want {
			t.Errorf("utf16Len(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
