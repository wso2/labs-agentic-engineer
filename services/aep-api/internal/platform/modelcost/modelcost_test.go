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

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

var rates = Rates{
	ModelID:              "claude-fable-5",
	InputUSDPerMTok:      10,
	OutputUSDPerMTok:     50,
	CacheReadUSDPerMTok:  1,
	CacheWriteUSDPerMTok: 12.5,
}

func TestCostPricesTheConfiguredModel(t *testing.T) {
	p := New(rates)
	got := p.Cost(contracts.TokenUsage{
		InputTokens:         1_000_000,
		OutputTokens:        200_000,
		CacheReadTokens:     3_000_000,
		CacheCreationTokens: 400_000,
		Model:               "claude-fable-5",
	})
	if got == nil {
		t.Fatal("expected a priced record, got nil")
	}
	// 10 + 10 + 3 + 5 = 28 USD
	if *got != 28 {
		t.Fatalf("cost = %v, want 28", *got)
	}
}

func TestCostRoundsToCents(t *testing.T) {
	p := New(rates)
	got := p.Cost(contracts.TokenUsage{InputTokens: 123_456, Model: "claude-fable-5"})
	if got == nil || *got != 1.23 {
		t.Fatalf("cost = %v, want 1.23", got)
	}
}

func TestCostRefusesForeignModel(t *testing.T) {
	p := New(rates)
	if got := p.Cost(contracts.TokenUsage{InputTokens: 5, Model: "gpt-oss"}); got != nil {
		t.Fatalf("foreign model priced to %v, want nil", *got)
	}
}

func TestCostRefusesMixedAggregate(t *testing.T) {
	p := New(rates)
	// "" model with tokens = an aggregate that mixed models — unpriceable.
	if got := p.Cost(contracts.TokenUsage{InputTokens: 5}); got != nil {
		t.Fatalf("mixed aggregate priced to %v, want nil", *got)
	}
}

func TestCostZeroRecordPricesToZero(t *testing.T) {
	p := New(rates)
	got := p.Cost(contracts.TokenUsage{})
	if got == nil || *got != 0 {
		t.Fatalf("zero record = %v, want 0", got)
	}
}
