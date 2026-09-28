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

// Package modelcost owns model pricing and the write-time USD stamp (#291,
// amended ADR-0011). Rates are DATA — a model_rates row per (host, model) —
// not service config. The host is part of the key because the same model id
// can be served, and billed, by more than one provider; it is the host the
// usage row was stamped with at launch (aep-api's copy of the org's model
// connection), never one a producer reports. USD is stamped at CAPTURE time from the rates then in force
// and persisted on the usage row; a later rate change never reprices history.
//
// The Stamper is an immutable in-memory price card built once at boot from the
// model_rates table (rates are ops-managed and change rarely, so a per-capture
// DB read would be wasteful; a rate edit takes effect on the next restart).
// This package imports no gorm: ModelRate carries struct tags only, migrate
// owns its AutoMigrate + seed, and app owns the boot-time load.
package modelcost

import "math"

// ModelRate is the model_rates row: one model's USD-per-MTok price card on
// one host. Struct tags only — the gorm dependency lives in migrate
// (AutoMigrate) and app (boot load), never here (§ arch: gorm is fenced to
// repositories + the kernel allowlist).
//
// Host is declared before ModelID so a fresh CreateTable keys the table
// (host, model_id), the same order migrate's re-key of an existing table uses.
type ModelRate struct {
	Host              string  `gorm:"primaryKey;type:text"`
	ModelID           string  `gorm:"primaryKey;type:text"`
	InputPerMTok      float64 `gorm:"not null;default:0"`
	OutputPerMTok     float64 `gorm:"not null;default:0"`
	CacheReadPerMTok  float64 `gorm:"not null;default:0"`
	CacheWritePerMTok float64 `gorm:"not null;default:0"`
	UpdatedAt         int64   `gorm:"autoUpdateTime:milli"`
}

// TableName pins the table so the entity can live in platform/ rather than a
// domain package without gorm inferring "model_rates" differently.
func (ModelRate) TableName() string { return "model_rates" }

// Tokens is the capture-time token count a stamp prices. It mirrors
// contracts.TokenUsage's fields without importing it — modelcost is a leaf the
// capture paths depend on, so it must not depend back on the contract shape.
//
// Host is the model host the usage was stamped with at launch; an empty host
// is unpriceable, exactly like an empty model id.
type Tokens struct {
	Host                string
	ModelID             string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// Stamper prices token counts against a fixed set of rates loaded at boot.
// Immutable after New — safe to share across the spec + delivery capture paths
// with no locking.
type Stamper struct {
	rates map[string]map[string]ModelRate // host → model id → rate
}

// NewStamper builds the price card from the model_rates rows.
func NewStamper(rows []ModelRate) *Stamper {
	rates := make(map[string]map[string]ModelRate)
	for _, r := range rows {
		byModel, ok := rates[r.Host]
		if !ok {
			byModel = make(map[string]ModelRate)
			rates[r.Host] = byModel
		}
		byModel[r.ModelID] = r
	}
	return &Stamper{rates: rates}
}

// Cost returns the stamped USD for a captured record (rounded to cents), or
// nil when it cannot be priced: no host or model id, or no rate row for that
// (host, model).
// A nil stamp persists as a null cost_usd — the console then shows tokens
// only for any aggregate that includes the unstamped row. The historical
// record is immutable: this is computed once, at capture, and never revisited.
func (s *Stamper) Cost(t Tokens) *float64 {
	usd, ok := s.raw(t)
	if !ok {
		return nil
	}
	usd = math.Round(usd*100) / 100
	return &usd
}

// SumCost prices a multi-model capture (#291): each slice at its own rate row,
// summed unrounded and rounded to cents once — per-slice rounding would zero
// out small contributors. All-or-nothing: a token-bearing slice the stamper
// cannot price (unknown/empty host or model) makes the WHOLE stamp nil, because a
// partial dollar figure silently under-reports spend, and null degrades to the
// honest tokens-only display. Zero-token slices are ignored; no priceable
// traffic at all is nil (nothing was spent, nothing to stamp).
func (s *Stamper) SumCost(slices []Tokens) *float64 {
	total := 0.0
	priced := false
	for _, t := range slices {
		if t.InputTokens+t.OutputTokens+t.CacheReadTokens+t.CacheCreationTokens == 0 {
			continue
		}
		usd, ok := s.raw(t)
		if !ok {
			return nil
		}
		total += usd
		priced = true
	}
	if !priced {
		return nil
	}
	total = math.Round(total*100) / 100
	return &total
}

// Priced reports whether (host, model) has a rate row, so usage on it is
// stamped in dollars. The model connection's `priced` reads this same lookup,
// so the Settings card and the Usage page cannot disagree.
// A nil Stamper prices nothing.
func (s *Stamper) Priced(host, model string) bool {
	if s == nil {
		return false
	}
	_, ok := s.rates[host][model]
	return ok && host != "" && model != ""
}

// raw is the unrounded USD for one record; ok is false when the (host, model)
// has no rate row (or either is empty).
func (s *Stamper) raw(t Tokens) (float64, bool) {
	if t.Host == "" || t.ModelID == "" {
		return 0, false
	}
	rate, ok := s.rates[t.Host][t.ModelID]
	if !ok {
		return 0, false
	}
	return (float64(t.InputTokens)*rate.InputPerMTok +
		float64(t.OutputTokens)*rate.OutputPerMTok +
		float64(t.CacheReadTokens)*rate.CacheReadPerMTok +
		float64(t.CacheCreationTokens)*rate.CacheWritePerMTok) / 1e6, true
}
