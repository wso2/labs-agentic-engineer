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
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
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
	h := WebhookHandler(secret, &fakeForwarder{err: webhook.ErrUpstreamUnavailable})
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
		{"valid, aep-api unavailable", signBody(secret, body), body, 503, "aep_api_unavailable"},
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
			if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "zen") || strings.Contains(logs.String(), validHex) {
				t.Fatal("secret, signature or payload in logs")
			}
		})
	}
}

func TestWebhook_RejectedEventNamesDeliveryEventReason(t *testing.T) {
	logs := captureLogs(t)
	WebhookHandler("s3cret", &fakeForwarder{}).ServeHTTP(httptest.NewRecorder(), webhookRequest([]byte(`{}`), ""))
	for _, want := range []string{`"delivery":"d-1"`, `"event":"ping"`, `"reason":"signature_invalid"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("webhook.rejected lacks %s: %s", want, logs.String())
		}
	}
}

// fakeForwarder records what it was handed and answers upstream and err.
type fakeForwarder struct {
	upstream              int
	err                   error
	calls                 int
	delivery, event, body string
}

func (f *fakeForwarder) Forward(_ context.Context, delivery, event string, body []byte) (int, error) {
	f.calls++
	f.delivery, f.event, f.body = delivery, event, string(body)
	return f.upstream, f.err
}

func TestWebhook_ForwardsVerifiedDelivery(t *testing.T) {
	logs := captureLogs(t)
	f := &fakeForwarder{upstream: http.StatusAccepted}
	body := []byte(`{"zen":"ok"}`)
	rec := httptest.NewRecorder()
	WebhookHandler("s3cret", f).ServeHTTP(rec, webhookRequest(body, signBody("s3cret", body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if f.calls != 1 || f.delivery != "d-1" || f.event != "ping" || f.body != string(body) {
		t.Fatalf("forwarded %+v", f)
	}
	if !strings.Contains(logs.String(), `"msg":"webhook.forwarded"`) || !strings.Contains(logs.String(), `"status":202`) || strings.Contains(logs.String(), "zen") {
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
	WebhookHandler("", &fakeForwarder{})
}

func TestWebhook_UnknownLengthOverCapIs413(t *testing.T) {
	captureLogs(t)
	big := bytes.Repeat([]byte("a"), 25<<20+1)
	r := webhookRequest(big, signBody("s3cret", big))
	r.ContentLength = -1 // chunked: only the read cap can catch it
	rec := httptest.NewRecorder()
	WebhookHandler("s3cret", &fakeForwarder{}).ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "payload_too_large") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// The reply rule (04 §8), end to end through the real forwarder: GitHub sees
// 200 when aep-api took the delivery or refused it for good, 503 when aep-api
// failed or could not be reached, and the log names aep-api's status.
func TestWebhook_ReplyRule(t *testing.T) {
	body := []byte(`{"repository":{"full_name":"acme/greeter"}}`)
	down := httptest.NewServer(http.NotFoundHandler())
	unreachable := down.URL
	down.Close()
	for name, tc := range map[string]struct {
		upstream int // 0: aep-api not reachable
		want     int
		log      []string
	}{
		"dispatched":          {http.StatusAccepted, http.StatusOK, []string{`"msg":"webhook.forwarded"`, `"status":202`}},
		"duplicate":           {http.StatusOK, http.StatusOK, []string{`"msg":"webhook.forwarded"`, `"status":200`}},
		"unknown repository":  {http.StatusNotFound, http.StatusOK, []string{`"msg":"webhook.forwarded"`, `"status":404`}},
		"aep-api 5xx":         {http.StatusInternalServerError, http.StatusServiceUnavailable, []string{`"msg":"webhook.rejected"`, `"reason":"aep_api_unavailable"`, `"status":500`}},
		"aep-api unreachable": {0, http.StatusServiceUnavailable, []string{`"msg":"webhook.rejected"`, `"reason":"aep_api_unavailable"`, `"status":0`}},
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			base := unreachable
			if tc.upstream != 0 {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.upstream) }))
				defer srv.Close()
				base = srv.URL
			}
			client, err := aepapi.NewClientWithResponses(base + "/internal/v1")
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			WebhookHandler("s3cret", webhook.NewForwarder(client)).ServeHTTP(rec, webhookRequest(body, signBody("s3cret", body)))
			if rec.Code != tc.want {
				t.Fatalf("GitHub sees %d, want %d", rec.Code, tc.want)
			}
			for _, want := range append(tc.log, `"delivery":"d-1"`, `"event":"ping"`) {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("logs lack %s: %s", want, logs.String())
				}
			}
			if strings.Contains(logs.String(), "webhook.forward_failed") || strings.Contains(logs.String(), "greeter") {
				t.Fatalf("logs = %s", logs.String())
			}
		})
	}
}

// Delivery and event are attacker-chosen before the signature check: every
// webhook log line carries at most 64 runes of each.
func TestWebhook_LogFieldsTruncated(t *testing.T) {
	long := strings.Repeat("é", 64)
	for name, f := range map[string]*fakeForwarder{
		"rejected":  {err: webhook.ErrUpstreamUnavailable},
		"forwarded": {upstream: http.StatusAccepted},
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			body := []byte(`{}`)
			r := webhookRequest(body, signBody("s3cret", body))
			r.Header.Set("X-GitHub-Delivery", long+"TAIL-D")
			r.Header.Set("X-GitHub-Event", long+"TAIL-E")
			WebhookHandler("s3cret", f).ServeHTTP(httptest.NewRecorder(), r)
			if !strings.Contains(logs.String(), `"delivery":"`+long+`"`) || !strings.Contains(logs.String(), `"event":"`+long+`"`) {
				t.Fatalf("logs = %s", logs.String())
			}
			if strings.Contains(logs.String(), "TAIL") {
				t.Fatalf("untruncated field: %s", logs.String())
			}
			if f.delivery != long+"TAIL-D" || f.event != long+"TAIL-E" {
				t.Fatalf("aep-api must get the headers whole: %q %q", f.delivery, f.event)
			}
		})
	}
}

// A body that trickles in is cut at the read deadline: the connection is not
// held open by an unauthenticated sender.
func TestWebhook_SlowBodyHitsTheReadDeadline(t *testing.T) {
	logs := captureLogs(t)
	f := &fakeForwarder{}
	srv := httptest.NewServer(webhookHandler("s3cret", f, webhookLimits{readTimeout: 200 * time.Millisecond, concurrency: 1}))
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "POST /webhooks/github HTTP/1.1\r\nHost: x\r\nX-GitHub-Delivery: d-1\r\nX-GitHub-Event: ping\r\nContent-Length: 100\r\n\r\n{\"zen\"")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(logs.String(), `"msg":"webhook.rejected"`) || !strings.Contains(logs.String(), `"reason":"request_timeout"`) {
		t.Fatalf("logs = %s", logs.String())
	}
	if f.calls != 0 {
		t.Fatal("a cut-off body was forwarded")
	}
}

// After the body is read, the read deadline no longer applies: a forward that
// outlasts it still answers GitHub.
func TestWebhook_ForwardMayOutlastTheReadDeadline(t *testing.T) {
	captureLogs(t)
	slow := &slowForwarder{delay: 400 * time.Millisecond}
	srv := httptest.NewServer(webhookHandler("s3cret", slow, webhookLimits{readTimeout: 100 * time.Millisecond, concurrency: 1}))
	defer srv.Close()
	body := []byte(`{}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody("s3cret", body))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || slow.ctxErr != nil {
		t.Fatalf("status %d, forward ctx %v", resp.StatusCode, slow.ctxErr)
	}
}

// slowForwarder takes delay and records whether its ctx ended meanwhile.
type slowForwarder struct {
	delay  time.Duration
	ctxErr error
}

func (f *slowForwarder) Forward(ctx context.Context, _, _ string, _ []byte) (int, error) {
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
	}
	f.ctxErr = ctx.Err()
	return http.StatusAccepted, nil
}

// Past the concurrency cap a delivery is refused before its body is read.
func TestWebhook_OverTheConcurrencyCapIsRefusedUnread(t *testing.T) {
	logs := captureLogs(t)
	blocked := &blockingForwarder{entered: make(chan struct{}), release: make(chan struct{})}
	h := webhookHandler("s3cret", blocked, webhookLimits{readTimeout: time.Second, concurrency: 1})
	body := []byte(`{}`)
	done := make(chan int)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, webhookRequest(body, signBody("s3cret", body)))
		done <- rec.Code
	}()
	<-blocked.entered
	counted := &countingReader{r: bytes.NewReader(body)}
	r := webhookRequest(body, signBody("s3cret", body))
	r.Body = io.NopCloser(counted)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "busy") || counted.reads != 0 {
		t.Fatalf("%d %s, %d reads", rec.Code, rec.Body.String(), counted.reads)
	}
	if !strings.Contains(logs.String(), `"reason":"busy"`) {
		t.Fatalf("logs = %s", logs.String())
	}
	close(blocked.release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("held delivery answered %d", code)
	}
	// The slot is free again.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, webhookRequest(body, signBody("s3cret", body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("after release: %d", rec.Code)
	}
}

type blockingForwarder struct {
	entered, release chan struct{}
	once             sync.Once
}

func (f *blockingForwarder) Forward(context.Context, string, string, []byte) (int, error) {
	f.once.Do(func() {
		close(f.entered)
		<-f.release
	})
	return http.StatusAccepted, nil
}

type countingReader struct {
	r     io.Reader
	reads int
}

func (c *countingReader) Read(p []byte) (int, error) { c.reads++; return c.r.Read(p) }
