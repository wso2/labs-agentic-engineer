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

package edge

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
)

// captureLogs points the default slog logger at a buffer of JSON lines for
// the test's duration. The buffer is safe for handlers that log after the
// response (the turns relay).
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func signBody(key string, b []byte) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write(b)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func webhookRequest(body []byte, sig string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	r.Header.Set("X-GitHub-Event", "ping")
	r.Header.Set("X-GitHub-Delivery", "d-1")
	if sig != "" {
		r.Header.Set("X-Hub-Signature-256", sig)
	}
	return r
}

func TestWebhook(t *testing.T) {
	secret := "s3cret"
	h := WebhookHandler(secret, webhook.Unwired())
	body := []byte(`{"zen":"ok"}`)
	validHex := strings.TrimPrefix(signBody(secret, body), "sha256=")
	cases := []struct {
		name, sig string
		body      []byte
		want      int
		code      string
	}{
		{"no signature", "", body, 401, "signature_invalid"},
		{"wrong key", signBody("nope", body), body, 401, "signature_invalid"},
		{"valid digest without prefix", validHex, body, 401, "signature_invalid"},
		{"sha1 prefix", "sha1=" + validHex, body, 401, "signature_invalid"},
		{"not hex", "sha256=zz" + validHex[2:], body, 401, "signature_invalid"},
		{"truncated digest", "sha256=" + validHex[:32], body, 401, "signature_invalid"},
		{"other body", signBody(secret, []byte(`{"zen":"no"}`)), body, 401, "signature_invalid"},
		{"valid, upstream not wired", signBody(secret, body), body, 503, "aep_api_unavailable"},
		{"too large", signBody(secret, body), bytes.Repeat([]byte("a"), 25<<20+1), 413, "payload_too_large"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLogs(t)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, webhookRequest(c.body, c.sig))
			if rec.Code != c.want || !strings.Contains(rec.Body.String(), c.code) {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
			}
			if c.want == 401 && !strings.Contains(logs.String(), `"msg":"webhook.rejected"`) {
				t.Fatal("no webhook.rejected event")
			}
			if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "zen") {
				t.Fatal("secret or payload in logs")
			}
		})
	}
}

func TestWebhook_RejectedEventNamesDeliveryEventReason(t *testing.T) {
	logs := captureLogs(t)
	WebhookHandler("s3cret", webhook.Unwired()).ServeHTTP(httptest.NewRecorder(), webhookRequest([]byte(`{}`), ""))
	for _, want := range []string{`"delivery":"d-1"`, `"event":"ping"`, `"reason":"signature_invalid"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("webhook.rejected lacks %s: %s", want, logs.String())
		}
	}
}

// fakeForwarder records what it was handed and answers err.
type fakeForwarder struct {
	err                   error
	calls                 int
	delivery, event, body string
}

func (f *fakeForwarder) Forward(_ context.Context, delivery, event string, body []byte) error {
	f.calls++
	f.delivery, f.event, f.body = delivery, event, string(body)
	return f.err
}

func TestWebhook_ForwardsVerifiedDelivery(t *testing.T) {
	logs := captureLogs(t)
	f := &fakeForwarder{}
	body := []byte(`{"zen":"ok"}`)
	rec := httptest.NewRecorder()
	WebhookHandler("s3cret", f).ServeHTTP(rec, webhookRequest(body, signBody("s3cret", body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if f.calls != 1 || f.delivery != "d-1" || f.event != "ping" || f.body != string(body) {
		t.Fatalf("forwarded %+v", f)
	}
	if !strings.Contains(logs.String(), `"msg":"webhook.forwarded"`) || strings.Contains(logs.String(), "zen") {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestWebhook_NotForwardedWhenRejected(t *testing.T) {
	f := &fakeForwarder{}
	h := WebhookHandler("s3cret", f)
	h.ServeHTTP(httptest.NewRecorder(), webhookRequest([]byte(`{}`), signBody("nope", []byte(`{}`))))
	big := bytes.Repeat([]byte("a"), 25<<20+1)
	h.ServeHTTP(httptest.NewRecorder(), webhookRequest(big, signBody("s3cret", big)))
	if f.calls != 0 {
		t.Fatalf("forwarded %d rejected deliveries", f.calls)
	}
}

func TestWebhook_AnyForwardErrorIs503(t *testing.T) {
	captureLogs(t)
	body := []byte(`{}`)
	for _, err := range []error{webhook.ErrUpstreamUnavailable, errors.New("boom")} {
		rec := httptest.NewRecorder()
		WebhookHandler("s3cret", &fakeForwarder{err: err}).ServeHTTP(rec, webhookRequest(body, signBody("s3cret", body)))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "aep_api_unavailable") {
			t.Fatalf("%v: %d %s", err, rec.Code, rec.Body.String())
		}
	}
}

func TestWebhookHandler_EmptySecretPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an empty secret must panic: anyone can sign with it")
		}
	}()
	WebhookHandler("", webhook.Unwired())
}

func TestWebhook_UnknownLengthOverCapIs413(t *testing.T) {
	captureLogs(t)
	big := bytes.Repeat([]byte("a"), 25<<20+1)
	r := webhookRequest(big, signBody("s3cret", big))
	r.ContentLength = -1 // chunked: only the read cap can catch it
	rec := httptest.NewRecorder()
	WebhookHandler("s3cret", webhook.Unwired()).ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "payload_too_large") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
