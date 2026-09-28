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

package modelcost

import "testing"

const anthropic = "api.anthropic.com"

func sonnetStamper() *Stamper {
	return NewStamper([]ModelRate{{
		Host:              anthropic,
		ModelID:           "claude-sonnet-5",
		InputPerMTok:      2.00,
		OutputPerMTok:     10.00,
		CacheReadPerMTok:  0.20,
		CacheWritePerMTok: 2.50,
	}})
}

func TestCostStampsAtSeededRates(t *testing.T) {
	s := sonnetStamper()
	// 1M input @ $2 + 1M output @ $10 + 1M cache-read @ $0.20 + 1M cache-write
	// @ $2.50 = $14.70, rounded to cents.
	got := s.Cost(Tokens{
		Host:                anthropic,
		ModelID:             "claude-sonnet-5",
		InputTokens:         1_000_000,
		OutputTokens:        1_000_000,
		CacheReadTokens:     1_000_000,
		CacheCreationTokens: 1_000_000,
	})
	if got == nil {
		t.Fatal("expected a stamped cost, got nil")
	}
	if *got != 14.70 {
		t.Fatalf("cost = %v, want 14.70", *got)
	}
}

func TestCostRoundsToCents(t *testing.T) {
	s := sonnetStamper()
	// 12,345 input @ $2/MTok = $0.02469 → rounds to $0.02.
	got := s.Cost(Tokens{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 12_345})
	if got == nil || *got != 0.02 {
		t.Fatalf("cost = %v, want 0.02", got)
	}
}

func TestCostNilWhenNoRateForModel(t *testing.T) {
	s := sonnetStamper()
	// A model with no rate row cannot be priced honestly — null, not zero.
	if got := s.Cost(Tokens{Host: anthropic, ModelID: "claude-opus-4-8", InputTokens: 1_000}); got != nil {
		t.Fatalf("cost = %v, want nil for an unpriced model", *got)
	}
}

func TestCostNilWhenNoModel(t *testing.T) {
	s := sonnetStamper()
	// A mixed-model aggregate (model "") is unpriceable at the row level.
	if got := s.Cost(Tokens{Host: anthropic, ModelID: "", InputTokens: 1_000}); got != nil {
		t.Fatalf("cost = %v, want nil for an empty model id", *got)
	}
}

func TestCostZeroTokensStampsZero(t *testing.T) {
	s := sonnetStamper()
	// A priced model with no traffic stamps $0 (distinct from an unpriceable
	// null): the model was known, the spend was genuinely nothing.
	got := s.Cost(Tokens{Host: anthropic, ModelID: "claude-sonnet-5"})
	if got == nil || *got != 0 {
		t.Fatalf("cost = %v, want 0", got)
	}
}

func multiModelStamper() *Stamper {
	return NewStamper([]ModelRate{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputPerMTok: 2.00, OutputPerMTok: 10.00, CacheReadPerMTok: 0.20, CacheWritePerMTok: 2.50},
		{Host: anthropic, ModelID: "claude-haiku-4-5", InputPerMTok: 1.00, OutputPerMTok: 5.00, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25},
	})
}

func TestSumCostPricesEachSliceAtItsOwnRate(t *testing.T) {
	s := multiModelStamper()
	// sonnet: 1M in + 100k out = $2 + $1 = $3.00; haiku: 1M in = $1.00.
	got := s.SumCost([]Tokens{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 1_000_000, OutputTokens: 100_000},
		{Host: anthropic, ModelID: "claude-haiku-4-5", InputTokens: 1_000_000},
	})
	if got == nil || *got != 4.00 {
		t.Fatalf("cost = %v, want 4.00", got)
	}
}

func TestSumCostRoundsOnceOverTheSum(t *testing.T) {
	s := multiModelStamper()
	// Each slice alone is $0.004 (rounds to $0.00); the sum is $0.008, which
	// must round to $0.01 — per-slice rounding would report a false zero.
	got := s.SumCost([]Tokens{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 2_000},
		{Host: anthropic, ModelID: "claude-haiku-4-5", InputTokens: 4_000},
	})
	if got == nil || *got != 0.01 {
		t.Fatalf("cost = %v, want 0.01", got)
	}
}

func TestSumCostNilWhenAnyTokenBearingSliceUnpriceable(t *testing.T) {
	s := multiModelStamper()
	// One slice with no rate row poisons the whole stamp: a partial dollar
	// figure would silently under-report the run's spend.
	got := s.SumCost([]Tokens{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 1_000_000},
		{Host: anthropic, ModelID: "some-unknown-model", InputTokens: 10},
	})
	if got != nil {
		t.Fatalf("cost = %v, want nil when a contributing slice has no rate", *got)
	}
}

func TestSumCostIgnoresZeroTokenSlices(t *testing.T) {
	s := multiModelStamper()
	// A zero-token slice — even for an unknown model — contributed nothing and
	// must not block pricing the real spend.
	got := s.SumCost([]Tokens{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 1_000_000},
		{Host: anthropic, ModelID: "some-unknown-model"},
	})
	if got == nil || *got != 2.00 {
		t.Fatalf("cost = %v, want 2.00", got)
	}
}

func TestSumCostNilOnEmptyOrAllZero(t *testing.T) {
	s := multiModelStamper()
	if got := s.SumCost(nil); got != nil {
		t.Fatalf("cost = %v, want nil for no slices", *got)
	}
	if got := s.SumCost([]Tokens{{Host: anthropic, ModelID: "claude-sonnet-5"}}); got != nil {
		t.Fatalf("cost = %v, want nil for all-zero slices", *got)
	}
}

// Rates are keyed by host AND model: the same model id served by another host
// is a different price card, and one with no row is unpriced — null, never the
// first host's figure.
func TestCostIsKeyedByHostAndModel(t *testing.T) {
	s := sonnetStamper()
	if got := s.Cost(Tokens{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 1_000_000}); got == nil || *got != 2.00 {
		t.Fatalf("(api.anthropic.com, claude-sonnet-5) cost = %v, want 2.00", got)
	}
	if got := s.Cost(Tokens{Host: "ollama.com", ModelID: "claude-sonnet-5", InputTokens: 1_000_000}); got != nil {
		t.Fatalf("(ollama.com, claude-sonnet-5) cost = %v, want nil: the rate is api.anthropic.com's", *got)
	}
}

// A model on a host with no rate row is unpriced until ops inserts one; the
// new row prices it without touching the existing host's card.
func TestCostUnpricedUntilAnOpsRowForTheHost(t *testing.T) {
	kimi := Tokens{Host: "ollama.com", ModelID: "kimi-k3", InputTokens: 1_000_000, OutputTokens: 1_000_000}
	if got := sonnetStamper().Cost(kimi); got != nil {
		t.Fatalf("(ollama.com, kimi-k3) cost = %v before any rate row, want nil", *got)
	}

	withOps := NewStamper([]ModelRate{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputPerMTok: 2.00, OutputPerMTok: 10.00},
		{Host: "ollama.com", ModelID: "kimi-k3", InputPerMTok: 0.60, OutputPerMTok: 2.50},
	})
	if got := withOps.Cost(kimi); got == nil || *got != 3.10 {
		t.Fatalf("(ollama.com, kimi-k3) cost = %v after the ops row, want 3.10", got)
	}
	if got := withOps.Cost(Tokens{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 1_000_000}); got == nil || *got != 2.00 {
		t.Fatalf("(api.anthropic.com, claude-sonnet-5) cost = %v beside the ops row, want 2.00", got)
	}
}

func TestCostNilWhenNoHost(t *testing.T) {
	s := sonnetStamper()
	// A row stamped with no host (a dispatch that never recorded one) cannot be
	// priced honestly, even when its model has a rate on some host.
	if got := s.Cost(Tokens{ModelID: "claude-sonnet-5", InputTokens: 1_000}); got != nil {
		t.Fatalf("cost = %v, want nil for an empty host", *got)
	}
}

func TestSumCostNilWhenASliceIsOnAnUnpricedHost(t *testing.T) {
	s := multiModelStamper()
	got := s.SumCost([]Tokens{
		{Host: anthropic, ModelID: "claude-sonnet-5", InputTokens: 1_000_000},
		{Host: "ollama.com", ModelID: "claude-haiku-4-5", InputTokens: 10},
	})
	if got != nil {
		t.Fatalf("cost = %v, want nil when a contributing slice's host has no rate", *got)
	}
}

// Priced is the lookup the model connection's `priced` reads: the same rows the
// stamp prices against, keyed on host AND model.
func TestPricedKeysOnHostAndModel(t *testing.T) {
	s := sonnetStamper()
	for _, tc := range []struct {
		host, model string
		want        bool
	}{
		{anthropic, "claude-sonnet-5", true},
		{"ollama.com", "claude-sonnet-5", false},
		{anthropic, "kimi-k3", false},
		{"", "", false},
	} {
		if got := s.Priced(tc.host, tc.model); got != tc.want {
			t.Errorf("Priced(%q, %q) = %v, want %v", tc.host, tc.model, got, tc.want)
		}
	}
}
