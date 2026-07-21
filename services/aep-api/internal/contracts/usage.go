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
// (ADR-0011 — tokens + model are the stored truth; USD is derived at read
// time, never carried here). Field names match the runner NDJSON `usage`
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
