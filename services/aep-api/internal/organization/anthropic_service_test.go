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

// UNIT tier: the REAL AnthropicCredentialService
// logic with no DB and the only out-of-process dependency — the Anthropic
// /v1/messages probe — faked at the HTTP boundary (WithAnthropicAPIBase →
// httptest). Proves the key-shape guard, the prefix/last4 preview the golden
// projection depends on, projectionFromAnthropicRow, every
// validateAnthropicKey status branch, and Connect's fail-before-persist
// ordering. The persistence contract (upsert, org_secrets, org isolation)
// lives in anthropic_dbtest_test.go; the HTTP contract (status codes, bodies,
// golden field-set) in anthropic_component_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// anthropicUnitKey is shaped like a real key ("sk-ant-api03-" + suffix), long
// enough for looksLikeAnthropicKey. preview: prefix [:15], last4 "1234".
const anthropicUnitKey = "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz1234"

// anthropicProbeCapture records what validateAnthropicKey actually sent.
type anthropicProbeCapture struct {
	calls   int
	method  string
	path    string
	apiKey  string
	version string
}

// anthropicFakeAPI serves the validation probe with a fixed status and
// captures the last request's shape. Shared by the dbtest tier (same package).
func anthropicFakeAPI(t testing.TB, status int) (string, *anthropicProbeCapture) {
	t.Helper()
	rec := &anthropicProbeCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		rec.method, rec.path = r.Method, r.URL.Path
		rec.apiKey = r.Header.Get("x-api-key")
		rec.version = r.Header.Get("anthropic-version")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":"msg_fake"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, rec
}

// anthropicValidationCode unwraps the *ValidationError code or fails the test.
func anthropicValidationCode(t *testing.T, err error) string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	return ve.Code
}

// --- looksLikeAnthropicKey ---------------------------------------------------

func TestAnthropicLooksLikeKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  string
		want bool
	}{
		{"real-shaped key", anthropicUnitKey, true},
		{"exactly the 20-char minimum", "sk-ant-0123456789012", true},
		{"one char under the minimum", "sk-ant-012345678901", false},
		{"wrong prefix, plausible length", "sk-oai-0123456789012345678901234", false},
		{"prefix only", "sk-ant-", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeAnthropicKey(tc.key); got != tc.want {
				t.Fatalf("looksLikeAnthropicKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// --- anthropicKeyPreview -------------------------------------------------------

func TestAnthropicKeyPreview(t *testing.T) {
	t.Parallel()
	// The golden (testdata/harvest/golden/get_org_credentials_anthropic.json)
	// shows keyPrefix "sk-ant-api03-XX" — 15 chars, i.e. "sk-ant-" + the next
	// 8 — and keyLast4 "XXXX": preview is k[:15] + k[len-4:].
	prefix, last4 := anthropicKeyPreview(anthropicUnitKey)
	if prefix != "sk-ant-api03-Ab" {
		t.Fatalf("prefix: got %q, want the golden-style 15-char %q", prefix, "sk-ant-api03-Ab")
	}
	if last4 != "1234" {
		t.Fatalf("last4: got %q, want %q", last4, "1234")
	}

	// Under 20 chars the whole key is returned as the "prefix" with no last4.
	// Unreachable via Connect (looksLikeAnthropicKey gates length first) but
	// pinned so a refactor can't silently start slicing short strings.
	prefix, last4 = anthropicKeyPreview("sk-ant-tiny")
	if prefix != "sk-ant-tiny" || last4 != "" {
		t.Fatalf("short key: got (%q,%q), want the whole key and empty last4", prefix, last4)
	}
}

// --- projectionFromAnthropicRow --------------------------------------------------

func TestAnthropicProjectionFromRow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 1, 5, 2, 1, 0, time.UTC)
	valErr := "probe failed"
	smRef := "must-not-leak"
	row := &OrgAnthropicCredential{
		OcOrgID:         "acme",
		KeyPrefix:       "sk-ant-api03-Ab",
		KeyLast4:        "1234",
		Status:          "active",
		ConnectedAt:     now,
		LastValidatedAt: &now,
		ValidationError: &valErr,
		// The SM-API triplet is row-internal; the projection must not carry it.
		SMAPISecretRefName: &smRef,
		SMAPIKVPath:        &smRef,
		SMAPIProperty:      &smRef,
	}

	p := projectionFromAnthropicRow(row)
	if p.OcOrgID != "acme" || p.KeyPrefix != "sk-ant-api03-Ab" || p.KeyLast4 != "1234" || p.Status != "active" {
		t.Fatalf("projection fields drifted: %+v", p)
	}
	if !p.ConnectedAt.Equal(now) || p.LastValidatedAt == nil || !p.LastValidatedAt.Equal(now) {
		t.Fatalf("projection timestamps drifted: %+v", p)
	}
	if p.ValidationError == nil || *p.ValidationError != valErr {
		t.Fatalf("validationError not carried: %+v", p.ValidationError)
	}

	// Wire shape: with no validation error the marshaled field set is EXACTLY
	// the golden's — {connectedAt, keyLast4, keyPrefix, lastValidatedAt,
	// ocOrgId, status} plus credentialKind — and never the SM-API triplet.
	p.ValidationError = nil
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal projection: %v", err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"connectedAt", "credentialKind", "keyLast4", "keyPrefix", "lastValidatedAt", "ocOrgId", "status"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("projection JSON field set drifted from golden:\n got %v\nwant %v", got, want)
	}
}

// --- validateAnthropicKey --------------------------------------------------------

func TestAnthropicValidateKey_StatusBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int
		wantCode string // "" → key accepted (nil error)
	}{
		{"200 key valid", http.StatusOK, ""},
		// 400 = key RECOGNIZED but payload arguable (e.g. unknown model) —
		// deliberately treated as authenticated, per the service comment.
		{"400 recognized-but-bad-payload accepted", http.StatusBadRequest, ""},
		{"401 key rejected", http.StatusUnauthorized, "anthropic_key_invalid"},
		{"403 key lacks permissions", http.StatusForbidden, "anthropic_key_forbidden"},
		// 429 (rate limit) is an unexpected NON-5xx: it stays a client-facing
		// ValidationError (400 at the edge). Only 5xx is treated as upstream —
		// see TestAnthropicValidateKey_UpstreamServerErrorIs502.
		{"429 unexpected non-5xx", http.StatusTooManyRequests, "anthropic_unexpected_status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, rec := anthropicFakeAPI(t, tc.status)
			svc := NewAnthropicCredentialService(nil, nil).WithAnthropicAPIBase(base)
			err := svc.validateAnthropicKey(context.Background(), AnthropicCredentialAPIKey, anthropicUnitKey)
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("status %d must accept the key, got %v", tc.status, err)
				}
			} else if got := anthropicValidationCode(t, err); got != tc.wantCode {
				t.Fatalf("code: got %q, want %q (err %v)", got, tc.wantCode, err)
			}
			if rec.calls != 1 {
				t.Fatalf("probe calls: got %d, want exactly 1", rec.calls)
			}
		})
	}
}

// TestAnthropicValidateKey_UpstreamServerErrorIs502 pins the FIXED behavior: an
// Anthropic 5xx is the upstream's fault, so validateAnthropicKey returns an
// *UpstreamError (mapped to 502 Bad Gateway at the edge) rather than the old
// ValidationError that collapsed a broken upstream into a client-fault 400.
func TestAnthropicValidateKey_UpstreamServerErrorIs502(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			base, _ := anthropicFakeAPI(t, status)
			svc := NewAnthropicCredentialService(nil, nil).WithAnthropicAPIBase(base)
			err := svc.validateAnthropicKey(context.Background(), AnthropicCredentialAPIKey, anthropicUnitKey)
			var ue *UpstreamError
			if !errors.As(err, &ue) {
				t.Fatalf("upstream %d must yield *UpstreamError, got %T: %v", status, err, err)
			}
			// The edge mapping (upstream 5xx → 502 bad_gateway, never a
			// client-fault 400) lives in orgconfig.sectionErrorFrom +
			// api.mapPatchError now; the typed UpstreamError above is the seam
			// this package owns.
		})
	}
}

func TestAnthropicValidateKey_ProbeShape(t *testing.T) {
	t.Parallel()
	base, rec := anthropicFakeAPI(t, http.StatusOK)
	svc := NewAnthropicCredentialService(nil, nil).WithAnthropicAPIBase(base)
	if err := svc.validateAnthropicKey(context.Background(), AnthropicCredentialAPIKey, anthropicUnitKey); err != nil {
		t.Fatalf("validate: %v", err)
	}
	// The probe is POST /v1/messages carrying the key in x-api-key plus the
	// mandatory anthropic-version header — both required by Anthropic; drop
	// either and every real validation breaks.
	if rec.method != http.MethodPost || rec.path != "/v1/messages" {
		t.Fatalf("probe target: got %s %s, want POST /v1/messages", rec.method, rec.path)
	}
	if rec.apiKey != anthropicUnitKey {
		t.Fatalf("x-api-key: got %q, want the key under test", rec.apiKey)
	}
	if rec.version != "2023-06-01" {
		t.Fatalf("anthropic-version: got %q, want 2023-06-01", rec.version)
	}
}

func TestAnthropicValidateKey_NetworkFailureIsUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // dead endpoint → connection refused
	svc := NewAnthropicCredentialService(nil, nil).WithAnthropicAPIBase(srv.URL)
	err := svc.validateAnthropicKey(context.Background(), AnthropicCredentialAPIKey, anthropicUnitKey)
	if got := anthropicValidationCode(t, err); got != "anthropic_unreachable" {
		t.Fatalf("code: got %q, want anthropic_unreachable (err %v)", got, err)
	}
}

// --- Connect ordering: reject before any I/O ------------------------------------

func TestAnthropicConnect_ShapeGuardsRejectBeforeAnyIO(t *testing.T) {
	t.Parallel()
	// db, store AND the API base are all unusable (nil / real anthropic.com is
	// never dialed because the guards fire first) — reaching any of them would
	// panic or hang, so a clean ValidationError proves the ordering.
	svc := NewAnthropicCredentialService(nil, nil).WithAnthropicAPIBase("http://127.0.0.1:0")
	cases := []struct {
		name     string
		key      string
		wantCode string
	}{
		{"empty", "", "anthropic_key_missing"},
		{"whitespace only trims to empty", "   \n", "anthropic_key_missing"},
		{"wrong prefix", strings.Repeat("x", 30), "anthropic_key_invalid"},
		{"too short", "sk-ant-short", "anthropic_key_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.Connect(context.Background(), "acme", AnthropicRoleDefault, AnthropicConnectRequest{APIKey: tc.key})
			if got := anthropicValidationCode(t, err); got != tc.wantCode {
				t.Fatalf("code: got %q, want %q (err %v)", got, tc.wantCode, err)
			}
		})
	}
}

func TestAnthropicConnect_UpstreamRejectionShortCircuitsPersistence(t *testing.T) {
	t.Parallel()
	base, _ := anthropicFakeAPI(t, http.StatusUnauthorized)
	// nil db/store: any persistence attempt would panic — passing proves the
	// upstream 401 fails Connect BEFORE the transaction begins.
	svc := NewAnthropicCredentialService(nil, nil).WithAnthropicAPIBase(base)
	_, err := svc.Connect(context.Background(), "acme", AnthropicRoleDefault, AnthropicConnectRequest{APIKey: anthropicUnitKey})
	if got := anthropicValidationCode(t, err); got != "anthropic_key_invalid" {
		t.Fatalf("code: got %q, want anthropic_key_invalid (err %v)", got, err)
	}
}
