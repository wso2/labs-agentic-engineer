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

package organization

// UNIT tier: SreModelConnectionService.ApplySeed over the same in-memory
// fakes as sre_model_connection_service_test.go. What it applies, what it
// skips, and that a seen hash (applied or refused) is never retried.

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func seedMarkerOf(w *sreWorld, org string) string { return string(w.keys[org+"/"+seedAppliedStoreKey]) }

func TestApplySeed_AppliesWhenNothingStored(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	prober := &sreProber{}
	s := newSreService(w, prober, sreOrgConn{})
	seed := Seed{BaseURL: "https://a.example/v1", Model: "gpt-4o-mini", APIKey: sreKey}

	outcome, err := s.ApplySeed(ctx, sreOrg, seed)
	if err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	if outcome != SeedApplied {
		t.Fatalf("outcome = %q, want %q", outcome, SeedApplied)
	}
	if len(prober.targets) != 1 {
		t.Fatalf("probe calls = %d, want 1", len(prober.targets))
	}
	if w.row == nil || w.row.Host != "a.example" || w.key(sreOrg) != sreKey {
		t.Fatalf("row/key not stored: row=%+v key=%q", w.row, w.key(sreOrg))
	}
	if got, want := seedMarkerOf(w, sreOrg), seedHash(seed)+":applied"; got != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
}

func TestApplySeed_SkipsWhenStored(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	seedSre(w, "a.example", sreKey)
	prober := &sreProber{}
	s := newSreService(w, prober, sreOrgConn{})
	seed := Seed{BaseURL: "https://b.example/v1", Model: "gpt-4o", APIKey: sreOtherKey}

	outcome, err := s.ApplySeed(ctx, sreOrg, seed)
	if err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	if outcome != SeedSkippedStored {
		t.Fatalf("outcome = %q, want %q", outcome, SeedSkippedStored)
	}
	if len(prober.targets) != 0 {
		t.Errorf("probe called %d times, want 0 (a stored connection is never probed)", len(prober.targets))
	}
	if w.row.Host != "a.example" {
		t.Errorf("row = %+v, want the stored connection left alone", w.row)
	}
}

func TestApplySeed_SkipsSameHashAfterApplied(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	prober := &sreProber{}
	s := newSreService(w, prober, sreOrgConn{})
	seed := Seed{BaseURL: "https://a.example/v1", Model: "gpt-4o-mini", APIKey: sreKey}

	if _, err := s.ApplySeed(ctx, sreOrg, seed); err != nil {
		t.Fatalf("first ApplySeed: %v", err)
	}
	// A console removal clears the row/key but not the seed marker: only the
	// marker should gate a retry of the unchanged seed.
	if err := s.Clear(ctx, sreOrg, sreActor); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	prober.targets = nil

	outcome, err := s.ApplySeed(ctx, sreOrg, seed)
	if err != nil {
		t.Fatalf("second ApplySeed: %v", err)
	}
	if outcome != SeedSkippedSeen {
		t.Fatalf("outcome = %q, want %q", outcome, SeedSkippedSeen)
	}
	if len(prober.targets) != 0 {
		t.Errorf("probe called %d times on a seen hash, want 0", len(prober.targets))
	}
	if w.row != nil {
		t.Errorf("row = %+v, want none (the seen seed was not re-applied)", w.row)
	}
}

func TestApplySeed_SkipsSameHashAfterRefused(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	prober := &sreProber{err: &ValidationError{Code: "llm_key_rejected", Message: "a.example rejected the key (401)"}}
	s := newSreService(w, prober, sreOrgConn{})
	seed := Seed{BaseURL: "https://a.example/v1", Model: "gpt-4o-mini", APIKey: sreKey}

	outcome, err := s.ApplySeed(ctx, sreOrg, seed)
	if err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}
	if outcome != SeedRefused {
		t.Fatalf("outcome = %q, want %q", outcome, SeedRefused)
	}
	if got, want := seedMarkerOf(w, sreOrg), seedHash(seed)+":refused"; got != want {
		t.Errorf("marker = %q, want %q", got, want)
	}

	prober.targets = nil
	outcome, err = s.ApplySeed(ctx, sreOrg, seed)
	if err != nil {
		t.Fatalf("second ApplySeed: %v", err)
	}
	if outcome != SeedSkippedSeen {
		t.Fatalf("outcome = %q, want %q", outcome, SeedSkippedSeen)
	}
	if len(prober.targets) != 0 {
		t.Errorf("probe called %d times on a seen refusal, want 0", len(prober.targets))
	}
}

func TestApplySeed_ChangedSeedAfterRemovalAppliesAgain(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	prober := &sreProber{}
	s := newSreService(w, prober, sreOrgConn{})
	seed := Seed{BaseURL: "https://a.example/v1", Model: "gpt-4o-mini", APIKey: sreKey}

	if _, err := s.ApplySeed(ctx, sreOrg, seed); err != nil {
		t.Fatalf("first ApplySeed: %v", err)
	}
	if err := s.Clear(ctx, sreOrg, sreActor); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	prober.targets = nil

	changed := Seed{BaseURL: "https://a.example/v1", Model: "gpt-4o", APIKey: sreKey}
	outcome, err := s.ApplySeed(ctx, sreOrg, changed)
	if err != nil {
		t.Fatalf("second ApplySeed: %v", err)
	}
	if outcome != SeedApplied {
		t.Fatalf("outcome = %q, want %q", outcome, SeedApplied)
	}
	if len(prober.targets) != 1 {
		t.Errorf("probe calls = %d, want 1 (a changed seed is tried again)", len(prober.targets))
	}
	if w.row == nil || w.row.Model != "gpt-4o" {
		t.Errorf("row = %+v, want the changed model applied", w.row)
	}
}

func TestApplySeed_NeverLogsTheKey(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	s := newSreService(newSreWorld(), &sreProber{}, sreOrgConn{})
	seed := Seed{BaseURL: "https://a.example/v1", Model: "gpt-4o-mini", APIKey: sreKey}
	if _, err := s.ApplySeed(ctx, sreOrg, seed); err != nil {
		t.Fatalf("ApplySeed: %v", err)
	}

	s2 := newSreService(newSreWorld(),
		&sreProber{err: &ValidationError{Code: "llm_key_rejected", Message: "rejected"}}, sreOrgConn{})
	refused := Seed{BaseURL: "https://b.example/v1", Model: "gpt-4o", APIKey: sreOtherKey}
	if _, err := s2.ApplySeed(ctx, "globex", refused); err != nil {
		t.Fatalf("ApplySeed (refused): %v", err)
	}

	logged := buf.String()
	if strings.Contains(logged, sreKey) || strings.Contains(logged, sreOtherKey) {
		t.Errorf("the log carries a seed key: %q", logged)
	}
}
