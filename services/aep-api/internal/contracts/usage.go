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

package contracts

// TokenUsage is the cross-runtime token-usage shape (#245/#249): what both
// agent runtimes report per unit of work and what the usage columns persist
// (amended ADR-0011 — tokens + model are the stored truth; USD is stamped at
// capture time onto the row's cost_usd from the model_rates then in force, and
// is not carried on this shape). Field names match the runner NDJSON `usage`
// object and the agents-stream manifest `usage` field byte-for-byte.
type TokenUsage struct {
	InputTokens         int64 `json:"inputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	// Model is the model id the work ran on; "" on aggregates that mix
	// models (or when the runtime could not determine it).
	Model string `json:"model,omitempty"`
}

// IsZero reports a record with no token traffic at all.
func (u TokenUsage) IsZero() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.CacheReadTokens == 0 && u.CacheCreationTokens == 0
}

// Add folds another record into an aggregate: tokens sum; the model id
// survives only while every non-zero contributor agrees on it.
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	model := u.Model
	switch {
	case u.IsZero():
		model = other.Model
	case other.IsZero():
		// keep u.Model
	case u.Model != other.Model:
		model = ""
	}
	return TokenUsage{
		InputTokens:         u.InputTokens + other.InputTokens,
		OutputTokens:        u.OutputTokens + other.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens + other.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens + other.CacheCreationTokens,
		Model:               model,
	}
}

// CapturedUsage is the wire shape of a runner result event's `usage` (#291):
// the folded aggregate every reader already consumed, plus the per-model split
// the stamper prices. Models carries one entry per model that spent tokens
// (canonical ids — the runner collapses versioned release ids), and is empty
// on captures from runners that predate the split; the capture path then falls
// back to pricing the aggregate by its single Model. The split exists because
// a real coding run regularly touches a second model (the SDK's small-model
// helpers), which blanks the aggregate Model under Add's agreement rule and
// made every such run unpriceable.
type CapturedUsage struct {
	TokenUsage
	Models []TokenUsage `json:"models,omitempty"`
}

// PricingSlices returns what the stamper should price: the per-model split
// when the runner reported one, else the aggregate as a single slice.
func (c CapturedUsage) PricingSlices() []TokenUsage {
	if len(c.Models) > 0 {
		return c.Models
	}
	return []TokenUsage{c.TokenUsage}
}

// StampedUsage is an aggregate of usage rows plus the sum of the USD stamped on
// them at capture (#291). CostUsd is nil when NO contributing row carried a
// stamp (SQL SUM(cost_usd) over all-null rows is NULL) — the console then shows
// tokens only. It never reprices: this sums frozen per-row stamps.
//
// Host is the model host the rows were billed by, stamped by the platform at
// launch, never by a producer; "" when the rows mix hosts (or predate
// stamping). The console reads it beside a tokens-only figure: "not priced,
// billed by <host>".
type StampedUsage struct {
	Tokens  TokenUsage
	CostUsd *float64
	Host    string
}

// UsageScope names ONE project lifetime's spend. It is the key the delivery
// roll-up returns, because a project slug is not an identity: a project can be
// deleted and recreated under the same name, and the two must not share a bill.
//
// Retired is the whole discriminator. False is the incarnation that is live now;
// true is spend that belonged to one the platform has already torn down. All
// retired generations of a slug share the scope — they are one closed story
// ("what this name used to cost"), and the record that separates them
// generation-by-generation stays in the ledger for anyone who needs it.
type UsageScope struct {
	ProjectID string
	Retired   bool
}

// Add folds another stamped aggregate in: tokens via TokenUsage.Add, the USD
// sums only where present — nil + nil stays nil, nil + x is x — and the host
// survives only while every non-zero contributor agrees on it, as the model
// does.
func (s StampedUsage) Add(other StampedUsage) StampedUsage {
	host := s.Host
	switch {
	case s.Tokens.IsZero():
		host = other.Host
	case other.Tokens.IsZero():
		// keep s.Host
	case s.Host != other.Host:
		host = ""
	}
	return StampedUsage{
		Tokens:  s.Tokens.Add(other.Tokens),
		CostUsd: addCost(s.CostUsd, other.CostUsd),
		Host:    host,
	}
}

func addCost(a, b *float64) *float64 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		sum := *a + *b
		return &sum
	}
}
