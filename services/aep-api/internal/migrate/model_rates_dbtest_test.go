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

package migrate_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// Every format's default connection must be priced: an org that connects
// with the defaults would otherwise see tokens only from its first turn. A
// format with no default host has no default connection to price. This is the
// executable half of "the default model is one the platform seeds a rate for",
// over the real migrated schema.
func TestEveryDefaultConnectionIsPriced_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)

	rows, err := migrate.LoadModelRates(context.Background(), db)
	if err != nil {
		t.Fatalf("load model rates: %v", err)
	}
	stamper := modelcost.NewStamper(rows)
	for _, f := range modelconn.Formats {
		if f.DefaultBaseURL == "" {
			continue
		}
		u, err := url.Parse(f.DefaultBaseURL)
		if err != nil {
			t.Fatalf("format %s default URL: %v", f.Format, err)
		}
		if !stamper.Priced(u.Hostname(), f.DefaultModel) {
			t.Errorf("format %s: default (%s, %s) has no seeded model_rates row", f.Format, u.Hostname(), f.DefaultModel)
		}
	}
}

// Rates are keyed by (host, model), over the real migrated schema: the seeded
// (api.anthropic.com, claude-sonnet-5) is priced; (ollama.com, kimi-k3) is
// unpriced until ops inserts its row, and priced after it — the whole of "a new
// rate is an ops INSERT, no contract change".
func TestModelRates_PricedPerHostAndModel_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	kimi := modelcost.Tokens{Host: modelconn.OllamaHost, ModelID: "kimi-k3", InputTokens: 1_000_000, OutputTokens: 1_000_000}
	sonnet := modelcost.Tokens{Host: modelconn.AnthropicHost, ModelID: "claude-sonnet-5", InputTokens: 1_000_000}

	rows, err := migrate.LoadModelRates(ctx, db)
	if err != nil {
		t.Fatalf("load model rates: %v", err)
	}
	if got := modelcost.NewStamper(rows).Cost(sonnet); got == nil || *got != 2.00 {
		t.Fatalf("(api.anthropic.com, claude-sonnet-5) cost = %v, want the seeded 2.00", got)
	}
	if got := modelcost.NewStamper(rows).Cost(kimi); got != nil {
		t.Fatalf("(ollama.com, kimi-k3) cost = %v before any ops row, want nil", *got)
	}

	if err := db.Exec(`INSERT INTO model_rates (host, model_id, input_per_m_tok, output_per_m_tok)
	                   VALUES ('ollama.com', 'kimi-k3', 0.60, 2.50)`).Error; err != nil {
		t.Fatalf("ops insert: %v", err)
	}
	rows, err = migrate.LoadModelRates(ctx, db)
	if err != nil {
		t.Fatalf("reload model rates: %v", err)
	}
	if got := modelcost.NewStamper(rows).Cost(kimi); got == nil || *got != 3.10 {
		t.Fatalf("(ollama.com, kimi-k3) cost = %v after the ops row, want 3.10", got)
	}
}

// The seed never clobbers an ops row: an adjusted Anthropic rate survives a
// re-seed, and an ops row for the same model on ANOTHER host neither blocks the
// Anthropic seed nor is touched by it.
func TestModelRatesSeed_NeverClobbersAnOpsRow_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	for _, stmt := range []string{
		// The intended ops override: sonnet's step-up to standard rates.
		`UPDATE model_rates SET input_per_m_tok = 3, output_per_m_tok = 15
		  WHERE host = 'api.anthropic.com' AND model_id = 'claude-sonnet-5'`,
		// haiku's Anthropic row is gone, and another host serves haiku.
		`DELETE FROM model_rates WHERE host = 'api.anthropic.com' AND model_id = 'claude-haiku-4-5'`,
		`INSERT INTO model_rates (host, model_id, input_per_m_tok, output_per_m_tok)
		 VALUES ('openrouter.ai', 'claude-haiku-4-5', 7, 70)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("ops edit: %v", err)
		}
	}

	for i := range 2 {
		if err := migrate.RunModelRatesSeed(ctx, db); err != nil {
			t.Fatalf("seed run %d: %v", i+1, err)
		}
	}

	type rate struct {
		Host, ModelID               string
		InputPerMTok, OutputPerMTok float64
	}
	var got []rate
	if err := db.Raw(`SELECT host, model_id, input_per_m_tok, output_per_m_tok
	                    FROM model_rates ORDER BY model_id, host`).Scan(&got).Error; err != nil {
		t.Fatalf("read rates: %v", err)
	}
	want := []rate{
		{"api.anthropic.com", "claude-haiku-4-5", 1, 5}, // re-seeded beside the other host's row
		{"openrouter.ai", "claude-haiku-4-5", 7, 70},    // untouched
		{"api.anthropic.com", "claude-sonnet-5", 3, 15}, // the ops override survives
		{"api.anthropic.com", "claude-sonnet-5-5", 2, 10},
	}
	if len(got) != len(want) {
		t.Fatalf("model_rates = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("model_rates[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
