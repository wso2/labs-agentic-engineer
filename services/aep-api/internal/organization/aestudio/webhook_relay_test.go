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

package aestudio

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/config"
)

func TestWebhookRelayURL_DeterministicUnguessable(t *testing.T) {
	seed := []byte("0123456789abcdef0123456789abcdef")
	a, b := WebhookRelayURL(seed, "default"), WebhookRelayURL(seed, "default")
	if a != b {
		t.Fatal("must be deterministic")
	}
	mac := hmac.New(sha256.New, seed)
	mac.Write([]byte("default"))
	want := "https://smee.io/" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:22]
	if a != want {
		t.Fatalf("got %s want %s", a, want)
	}
	if WebhookRelayURL(seed, "other") == a || WebhookRelayURL(nil, "default") != "" {
		t.Fatal("per org, and empty without a seed")
	}
	if !regexp.MustCompile(`^https://smee\.io/[A-Za-z0-9_-]{22}$`).MatchString(a) {
		t.Fatalf("smee.io channel ids must match ^[a-zA-Z0-9-_]{1,128}$: %s", a)
	}
}

// warnRecorder keeps every Warn-or-above record New logs.
type warnRecorder struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *warnRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *warnRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Level >= slog.LevelWarn {
		h.recs = append(h.recs, r)
	}
	return nil
}
func (h *warnRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnRecorder) WithGroup(string) slog.Handler      { return h }

func recordWarns(t *testing.T) *warnRecorder {
	t.Helper()
	h := &warnRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// A relay sends every org's GitHub payloads through a third-party relay, so
// boot says so once, naming neither the key nor any channel.
func TestNew_WebhookRelayOnWarnsOnce(t *testing.T) {
	h := recordWarns(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	New(Deps{Config: config.AEStudioConfig{WebhookRelaySeed: key}})
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recs) != 1 {
		t.Fatalf("%d Warn records, want exactly 1", len(h.recs))
	}
	r := h.recs[0]
	if r.Message != "webhook relay ON: GitHub payloads pass through smee.io; dev only" {
		t.Errorf("message %q", r.Message)
	}
	if r.NumAttrs() != 0 {
		t.Errorf("%d attrs, want none", r.NumAttrs())
	}
	var line strings.Builder
	slog.New(slog.NewTextHandler(&line, nil)).Handler().Handle(context.Background(), r)
	if out := line.String(); strings.Contains(out, "smee.io/") || strings.Contains(out, string(key)) ||
		strings.Contains(out, WebhookRelayURL(key, "default")) {
		t.Errorf("WARN line carries a channel or the key: %q", out)
	}
}

func TestNew_WebhookRelayOffLogsNothing(t *testing.T) {
	h := recordWarns(t)
	New(Deps{Config: config.AEStudioConfig{}})
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.recs) != 0 {
		t.Fatalf("relay off logged %d Warn records", len(h.recs))
	}
}
