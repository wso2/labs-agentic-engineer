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

package codingagent

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

func TestUsageFromLogReadsTheResultLine(t *testing.T) {
	log := `2026-07-21T10:00:00.000000000Z {"schemaVersion":1,"ts":"t","seq":1,"kind":"phase","phase":"agent"}
2026-07-21T10:00:01.000000000Z [oneshot] plain bootstrap line
2026-07-21T10:00:02.000000000Z {"schemaVersion":1,"ts":"t","seq":9,"kind":"result","status":"success","usage":{"inputTokens":100,"outputTokens":20,"cacheReadTokens":3000,"cacheCreationTokens":40,"model":"claude-fable-5"}}
`
	u := usageFromLog(log)
	if u == nil {
		t.Fatal("expected usage, got nil")
	}
	if u.InputTokens != 100 || u.OutputTokens != 20 || u.CacheReadTokens != 3000 ||
		u.CacheCreationTokens != 40 || u.Model != "claude-fable-5" {
		t.Fatalf("unexpected usage: %+v", u)
	}
}

func TestUsageFromLogLastResultWins(t *testing.T) {
	log := `{"schemaVersion":1,"ts":"t","seq":1,"kind":"result","status":"failure","usage":{"inputTokens":1,"outputTokens":1,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
{"schemaVersion":1,"ts":"t","seq":2,"kind":"result","status":"success","usage":{"inputTokens":7,"outputTokens":3,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
`
	u := usageFromLog(log)
	if u == nil || u.InputTokens != 7 {
		t.Fatalf("expected the last result's usage, got %+v", u)
	}
}

func TestUsageFromLogKeepsThePerModelSplit(t *testing.T) {
	log := `{"schemaVersion":1,"ts":"t","seq":1,"kind":"result","status":"success","usage":{"inputTokens":110,"outputTokens":55,"cacheReadTokens":1000,"cacheCreationTokens":200,"model":"","models":[{"inputTokens":100,"outputTokens":50,"cacheReadTokens":1000,"cacheCreationTokens":200,"model":"claude-sonnet-5"},{"inputTokens":10,"outputTokens":5,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-haiku-4-5"}]}}
`
	u := usageFromLog(log)
	if u == nil {
		t.Fatal("expected usage, got nil")
	}
	if u.Model != "" || u.InputTokens != 110 {
		t.Fatalf("unexpected aggregate: %+v", u.TokenUsage)
	}
	want := []contracts.TokenUsage{
		{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 1000, CacheCreationTokens: 200, Model: "claude-sonnet-5"},
		{InputTokens: 10, OutputTokens: 5, Model: "claude-haiku-4-5"},
	}
	if len(u.Models) != len(want) || u.Models[0] != want[0] || u.Models[1] != want[1] {
		t.Fatalf("models = %+v, want %+v", u.Models, want)
	}
	// PricingSlices prefers the split…
	if got := u.PricingSlices(); len(got) != 2 || got[0].Model != "claude-sonnet-5" {
		t.Fatalf("PricingSlices = %+v, want the per-model split", got)
	}
	// …and falls back to the aggregate for pre-split runners.
	legacy := contracts.CapturedUsage{TokenUsage: contracts.TokenUsage{InputTokens: 7, Model: "claude-sonnet-5"}}
	if got := legacy.PricingSlices(); len(got) != 1 || got[0].Model != "claude-sonnet-5" {
		t.Fatalf("PricingSlices(legacy) = %+v, want the aggregate as one slice", got)
	}
}

func TestUsageFromLogAbsentForPreCaptureRunners(t *testing.T) {
	log := `{"schemaVersion":1,"ts":"t","seq":1,"kind":"result","status":"success"}
some stray text mentioning "result" and "usage" but not JSON
`
	if u := usageFromLog(log); u != nil {
		t.Fatalf("expected nil for a usage-less log, got %+v", u)
	}
}

// TestUsageFromLogReadsAV2RunSettledLine covers the envelope a v2 runner
// actually writes. It emits NO `result` kind at all — its run settles as
// `run_settled` and the usage rides RunEvent.usage — so a capture that matched
// only `result` returned nil for every v2 run, RecordUsage was never called,
// and the cycle row kept an empty model id and a null cost while the pricing
// path downstream worked perfectly.
func TestUsageFromLogReadsAV2RunSettledLine(t *testing.T) {
	log := `2026-09-04T09:25:39.000000000Z {"v":2,"seq":1,"kind":"run_started","agentId":"lead","ts":"2026-09-04T09:25:39Z"}
2026-09-04T09:55:02.000000000Z {"v":2,"seq":812,"kind":"run_settled","agentId":"lead","ts":"2026-09-04T09:55:02Z","outcome":"success","usage":{"inputTokens":110,"outputTokens":55,"cacheReadTokens":1000,"cacheCreationTokens":200,"model":"","models":[{"inputTokens":100,"outputTokens":50,"cacheReadTokens":1000,"cacheCreationTokens":200,"model":"claude-sonnet-5"},{"inputTokens":10,"outputTokens":5,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-haiku-4-5"}]}}
`
	u := usageFromLog(log)
	if u == nil {
		t.Fatal("a v2 run settled with usage captured nothing — the run would be unbilled")
	}
	if u.InputTokens != 110 || u.OutputTokens != 55 || u.CacheReadTokens != 1000 || u.CacheCreationTokens != 200 {
		t.Fatalf("aggregate = %+v", u.TokenUsage)
	}
	// The split is what makes a multi-model run priceable: the aggregate's model
	// id is blank (two models disagree), so pricing has nothing to key on
	// without it.
	want := []contracts.TokenUsage{
		{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 1000, CacheCreationTokens: 200, Model: "claude-sonnet-5"},
		{InputTokens: 10, OutputTokens: 5, Model: "claude-haiku-4-5"},
	}
	if len(u.Models) != len(want) {
		t.Fatalf("models = %+v, want %+v", u.Models, want)
	}
	for i := range want {
		if u.Models[i] != want[i] {
			t.Errorf("models[%d] = %+v, want %+v", i, u.Models[i], want[i])
		}
	}
	slices := u.PricingSlices()
	if len(slices) != 2 || slices[0].Model != "claude-sonnet-5" {
		t.Errorf("PricingSlices = %+v, want the per-model split", slices)
	}
}

// TestUsageFromLogNeverSumsTurnEnded pins the accounting rule the runtime's own
// reporting forces: usage is CUMULATIVE across a session, so the terminal line
// already holds the whole run. Folding the turns in on the way past would
// multiply the bill by roughly the number of turns.
func TestUsageFromLogNeverSumsTurnEnded(t *testing.T) {
	log := `{"v":2,"seq":10,"kind":"turn_ended","agentId":"lead","outcome":"success","usage":{"inputTokens":40,"outputTokens":10,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
{"v":2,"seq":20,"kind":"turn_ended","agentId":"lead","outcome":"success","usage":{"inputTokens":90,"outputTokens":25,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
{"v":2,"seq":30,"kind":"run_settled","agentId":"lead","outcome":"success","usage":{"inputTokens":90,"outputTokens":25,"cacheReadTokens":0,"cacheCreationTokens":0,"model":"claude-fable-5"}}
`
	u := usageFromLog(log)
	if u == nil {
		t.Fatal("expected usage, got nil")
	}
	if u.InputTokens != 90 || u.OutputTokens != 25 {
		t.Fatalf("usage = %+v, want the terminal line's cumulative total, never a sum", u.TokenUsage)
	}
}
