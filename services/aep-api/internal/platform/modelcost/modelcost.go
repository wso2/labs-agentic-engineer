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

// Package modelcost derives read-time USD from stored token counts
// (ADR-0011): tokens + model id are the persisted truth, dollars are always
// computed against the currently configured rates, so a rate fix
// retroactively corrects every displayed figure and one formula prices every
// surface. The rates come from deployment config (config.ModelPricingConfig)
// and never leave the server.
package modelcost

import (
	"math"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

// Rates is the single active model's USD-per-MTok price card.
type Rates struct {
	ModelID              string
	InputUSDPerMTok      float64
	OutputUSDPerMTok     float64
	CacheReadUSDPerMTok  float64
	CacheWriteUSDPerMTok float64
}

// Pricer prices usage records against one Rates card.
type Pricer struct{ rates Rates }

// New builds a Pricer.
func New(rates Rates) *Pricer { return &Pricer{rates: rates} }

// Cost returns the record's derived USD (rounded to cents), or nil when it
// cannot be priced honestly: the record ran on a model other than the
// configured one, or an aggregate mixed models ("" model with tokens). The
// console degrades a nil to a token figure. A zero record prices to 0 — the
// console hides zero chips regardless.
func (p *Pricer) Cost(u contracts.TokenUsage) *float64 {
	if u.Model != p.rates.ModelID && !(u.Model == "" && u.IsZero()) {
		return nil
	}
	usd := (float64(u.InputTokens)*p.rates.InputUSDPerMTok +
		float64(u.OutputTokens)*p.rates.OutputUSDPerMTok +
		float64(u.CacheReadTokens)*p.rates.CacheReadUSDPerMTok +
		float64(u.CacheCreationTokens)*p.rates.CacheWriteUSDPerMTok) / 1e6
	usd = math.Round(usd*100) / 100
	return &usd
}
