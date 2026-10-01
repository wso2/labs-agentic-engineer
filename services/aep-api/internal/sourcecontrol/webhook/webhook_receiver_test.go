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

package webhook

// RECEIVER tier (the webhook feature owns this):
// the REAL raw http.HandlerFunc driven with net/http/httptest, end to end
// through the receiver pipeline: routing → HMAC → dedup INSERT → dispatch →
// mark-processed → ack. "HMAC valid→200, bad/missing signature→401, duplicate
// X-GitHub-Delivery→deduped — all computable in-process, no token."
//
// Real pieces: Verifier (HMAC-SHA256 over the raw body), sourcecontrol.DeliveryStore over a
// per-test Postgres (dbtest.New — dedup is the genuine PK unique-violation, so
// this file rides the DB lane and skips under -short), Router. Faked seams:
// SecretProvider (known per-org secret; reuses staticProvider from
// verifier_test.go) and OcOrgIDLookup (routing key → ocOrgID). The dispatched
// handler is router_test.go's recordingHandler, so tests assert both the ack
// status AND whether dispatch actually fired.
//
// Pipeline-order properties pinned here (read off Receive):
//   - routing resolves BEFORE HMAC verify (an unroutable event 200-acks
//     without touching the verifier);
//   - Persist runs AFTER verify (a forged event never writes a delivery row);
//   - MarkProcessed drives the dedup ack (a replay of finished work 200-acks
//     with no re-dispatch), while a failed handler leaves processed_at NULL so
//     a GitHub redelivery re-enters the handler.

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// receiverSecret is the per-org HMAC key the fake SecretProvider serves; valid
// requests are signed with it, forged ones with anything else. staticProvider
// returns it for Force=false AND Force=true, so a bad signature stays bad even
// after the verifier's forced refetch.
const receiverSecret = "receiver-hmac-secret"

// fakeOrgLookup routes repo full names / installation ids to ocOrgIDs from a
// fixed map; unknown keys get the same *organization.NotFoundError the real
// CredentialService returns, which the receiver must 200-ack-noop.
type fakeOrgLookup struct {
	repos    map[string]string
	installs map[int64]string
}

func (f *fakeOrgLookup) OrgIDByInstallationID(_ context.Context, id int64) (string, error) {
	if v, ok := f.installs[id]; ok {
		return v, nil
	}
	return "", &organization.NotFoundError{What: fmt.Sprintf("installation %d", id)}
}

func (f *fakeOrgLookup) OrgIDByRepoFullName(_ context.Context, fullName string) (string, error) {
	if v, ok := f.repos[fullName]; ok {
		return v, nil
	}
	return "", &organization.NotFoundError{What: "repo " + fullName}
}

// receiverHarness assembles the real receiver exactly as production wires it,
// with the two external seams (secrets, routing lookup) faked and one
// recordingHandler registered for every repository-routed event so dispatch is
// observable.
type receiverHarness struct {
	db      *gorm.DB
	ctrl    WebhookController
	handler *recordingHandler
}

// repoRoutedEvents are the events extractRoutingKey resolves through
// repository.full_name. The harness registers the recorder for all of them —
// "push" alone would leave the two the delivery domain actually reacts to
// (pull_request, issues) untested end to end through the receiver, and a
// missing row in extractRoutingKey's switch 200-noops silently rather than
// failing.
var repoRoutedEvents = []string{"push", "pull_request", "issues"}

func newReceiverHarness(t *testing.T) *receiverHarness {
	t.Helper()
	db := dbtest.New(t)
	handler := &recordingHandler{}
	router := NewRouter()
	for _, event := range repoRoutedEvents {
		router.Register(event, "", handler)
	}
	lookup := &fakeOrgLookup{repos: map[string]string{"acme/web": "org-acme"}}
	ctrl := NewWebhookController(
		NewVerifier(newStaticProvider(receiverSecret)),
		sourcecontrol.NewDeliveryStore(db),
		router,
		lookup,
		NewRoutingCache(0),
	)
	// Dispatch runs after the ack in production, on a goroutine. Run it inline
	// here so an assertion straight after post() observes a finished dispatch
	// rather than racing one — the ordering under test is "ack does not wait
	// for the handler", not "the handler runs on another thread".
	ctrl.(*webhookController).spawn = func(f func()) { f() }
	return &receiverHarness{db: db, ctrl: ctrl, handler: handler}
}

// post builds a GitHub-shaped POST and drives it through the real Receive.
// Empty header values are omitted (GitHub always sends all three; tests drop
// one to pin the rejection).
func (h *receiverHarness) post(t *testing.T, deliveryID, event, signature string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	if deliveryID != "" {
		req.Header.Set("X-GitHub-Delivery", deliveryID)
	}
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	if signature != "" {
		req.Header.Set("X-Hub-Signature-256", signature)
	}
	rec := httptest.NewRecorder()
	h.ctrl.Receive(rec, req)
	return rec
}

func (h *receiverHarness) loadDelivery(t *testing.T, deliveryID string) sourcecontrol.WebhookDelivery {
	t.Helper()
	var row sourcecontrol.WebhookDelivery
	if err := h.db.Where("delivery_id = ?", deliveryID).First(&row).Error; err != nil {
		t.Fatalf("delivery row %s must exist: %v", deliveryID, err)
	}
	return row
}

// pushBody is a routable push event: repository.full_name is the routing key
// for "push" (routing_key.go), and it maps to org-acme in the fake lookup.
var pushBody = []byte(`{"repository":{"full_name":"acme/web"},"ref":"refs/heads/main"}`)

func TestReceiver_ValidSignature_DispatchesAndAcks200(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)

	rec := h.post(t, "delivery-ok", "push", sign(receiverSecret, pushBody), pushBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("valid signed event must ack 200, got %d (%s)", rec.Code, rec.Body)
	}
	if len(h.handler.calls) != 1 {
		t.Fatalf("dispatch must fire exactly once, got %d", len(h.handler.calls))
	}
	got := h.handler.calls[0]
	if got.event != "push" || got.action != "" || got.payload != string(pushBody) {
		t.Fatalf("handler received (event=%q action=%q payload=%q); want the raw push payload", got.event, got.action, got.payload)
	}
	// The delivery row carries the routing-resolved org and is marked processed
	// — that stamp is what turns the NEXT replay into a dedup ack.
	row := h.loadDelivery(t, "delivery-ok")
	if row.OcOrgID != "org-acme" || row.Event != "push" {
		t.Fatalf("delivery row must record the resolved org + event, got %+v", row)
	}
	if row.ProcessedAt == nil {
		t.Fatal("a successful dispatch must mark the delivery processed")
	}
}

// TestReceiver_PullRequestAndIssuesRouteToHandlers pins the two events the
// delivery domain's handlers hang off, end to end through the receiver: both
// resolve their org through repository.full_name, both dispatch with the
// payload's action parsed out. Without this, a row dropped from
// extractRoutingKey's switch would 200-noop in production and no test would
// notice.
func TestReceiver_PullRequestAndIssuesRouteToHandlers(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)

	bodies := map[string][]byte{
		"pull_request": []byte(`{"action":"opened","repository":{"full_name":"acme/web"},` +
			`"pull_request":{"number":42,"draft":false,"head":{"ref":"aep/m7-c1"}}}`),
		"issues": []byte(`{"action":"milestoned","repository":{"full_name":"acme/web"},` +
			`"issue":{"number":12},"milestone":{"number":7,"title":"v7"}}`),
	}
	for i, event := range []string{"pull_request", "issues"} {
		body := bodies[event]
		rec := h.post(t, fmt.Sprintf("delivery-%s", event), event, sign(receiverSecret, body), body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s must ack 200, got %d (%s)", event, rec.Code, rec.Body)
		}
		if len(h.handler.calls) != i+1 {
			t.Fatalf("%s must dispatch, got %d calls", event, len(h.handler.calls))
		}
		got := h.handler.calls[i]
		if got.event != event {
			t.Fatalf("dispatched event = %q, want %q", got.event, event)
		}
	}
	if action := h.handler.calls[0].action; action != "opened" {
		t.Fatalf("pull_request action = %q, want opened", action)
	}
	if action := h.handler.calls[1].action; action != "milestoned" {
		t.Fatalf("issues action = %q, want milestoned", action)
	}
}

func TestReceiver_BadSignature_401NoDispatchNoPersist(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)

	rec := h.post(t, "delivery-forged", "push", sign("wrong-secret", pushBody), pushBody)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged signature must be 401, got %d (%s)", rec.Code, rec.Body)
	}
	if len(h.handler.calls) != 0 {
		t.Fatalf("a forged event must NOT dispatch, got %d calls", len(h.handler.calls))
	}
	// Verify runs BEFORE Persist: a forged event never writes a delivery row.
	var count int64
	h.db.Model(&sourcecontrol.WebhookDelivery{}).Where("delivery_id = ?", "delivery-forged").Count(&count)
	if count != 0 {
		t.Fatal("a forged event must not be persisted")
	}
}

func TestReceiver_MissingSignature_401NoDispatch(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)

	// No X-Hub-Signature-256 at all → malformed (no sha256= prefix) → 401.
	rec := h.post(t, "delivery-unsigned", "push", "", pushBody)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature must be 401, got %d (%s)", rec.Code, rec.Body)
	}
	if len(h.handler.calls) != 0 {
		t.Fatalf("an unsigned event must NOT dispatch, got %d calls", len(h.handler.calls))
	}
}

func TestReceiver_DuplicateDelivery_DedupedSecondAck200NoRedispatch(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)
	sig := sign(receiverSecret, pushBody)

	if rec := h.post(t, "delivery-dup", "push", sig, pushBody); rec.Code != http.StatusOK {
		t.Fatalf("first delivery must ack 200, got %d", rec.Code)
	}
	if len(h.handler.calls) != 1 {
		t.Fatalf("first delivery must dispatch once, got %d", len(h.handler.calls))
	}

	// GitHub redelivers the SAME X-GitHub-Delivery after we already processed
	// it: the PK dedup row (processed_at set) turns it into a 200 ack with NO
	// second dispatch. This is the at-most-once guarantee end to end.
	rec := h.post(t, "delivery-dup", "push", sig, pushBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay of processed work must ack 200, got %d (%s)", rec.Code, rec.Body)
	}
	if len(h.handler.calls) != 1 {
		t.Fatalf("replay must NOT re-dispatch: want 1 total call, got %d", len(h.handler.calls))
	}
}

func TestReceiver_HandlerFailure_StillAcks200AndSchedulesRetry(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)
	sig := sign(receiverSecret, pushBody)

	// The ack reports only whether the delivery was ACCEPTED, so a failing
	// handler still gets 200. The handler's verdict lives on the row: the
	// error is recorded, processed_at stays null, and next_attempt_at is set
	// so RetrySweeper claims it. Acking the failure as 5xx instead is what the
	// old contract did, and it is not available any more — GitHub cancels the
	// request at 10s, which killed the work mid-flight rather than reporting it.
	h.handler.err = fmt.Errorf("downstream boom")
	rec := h.post(t, "delivery-retry", "push", sig, pushBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("an accepted delivery acks 200 even when the handler fails, got %d", rec.Code)
	}
	row := h.loadDelivery(t, "delivery-retry")
	if row.ProcessedAt != nil || row.ProcessError == "" {
		t.Fatalf("failed delivery must stay unprocessed with the error recorded, got %+v", row)
	}
	if row.Attempts != 1 {
		t.Fatalf("the receiver's own pass counts as attempt 1, got %d", row.Attempts)
	}
	if row.NextAttemptAt == nil {
		t.Fatal("a failed delivery must be scheduled for retry, got no next_attempt_at")
	}

	// Redelivery (same delivery id, handler healthy again): Persist reports
	// neither Created nor AlreadyProcessed → the handler RE-RUNS → 200.
	h.handler.err = nil
	rec = h.post(t, "delivery-retry", "push", sig, pushBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("redelivery after a fixed handler must ack 200, got %d (%s)", rec.Code, rec.Body)
	}
	if len(h.handler.calls) != 2 {
		t.Fatalf("redelivery of unprocessed work must re-dispatch: want 2 calls, got %d", len(h.handler.calls))
	}
	if row := h.loadDelivery(t, "delivery-retry"); row.ProcessedAt == nil || row.ProcessError != "" {
		t.Fatalf("successful redelivery must mark processed + clear the error, got %+v", row)
	}
}

func TestReceiver_UnroutableRepo_Acks200Noop(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)

	// A push for a repo not connected to this AEP instance: the routing lookup
	// 404s and the receiver 200-ack-noops so GitHub stops retrying. Routing
	// runs before HMAC, so no signature is needed to observe this.
	body := []byte(`{"repository":{"full_name":"stranger/repo"}}`)
	rec := h.post(t, "delivery-stranger", "push", "", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("an event for an unconnected repo must ack 200 noop, got %d (%s)", rec.Code, rec.Body)
	}
	if len(h.handler.calls) != 0 {
		t.Fatalf("an unroutable event must NOT dispatch, got %d calls", len(h.handler.calls))
	}
	var count int64
	h.db.Model(&sourcecontrol.WebhookDelivery{}).Where("delivery_id = ?", "delivery-stranger").Count(&count)
	if count != 0 {
		t.Fatal("an unroutable event must not be persisted")
	}
}

func TestReceiver_MissingGitHubHeaders_400(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)
	sig := sign(receiverSecret, pushBody)

	if rec := h.post(t, "", "push", sig, pushBody); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing X-GitHub-Delivery must be 400, got %d", rec.Code)
	}
	if rec := h.post(t, "delivery-no-event", "", sig, pushBody); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing X-GitHub-Event must be 400, got %d", rec.Code)
	}
	if len(h.handler.calls) != 0 {
		t.Fatalf("header-rejected requests must NOT dispatch, got %d calls", len(h.handler.calls))
	}
}
