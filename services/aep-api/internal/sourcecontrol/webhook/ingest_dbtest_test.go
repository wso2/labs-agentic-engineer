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

// INGEST tier (DB lane): the Ingestor over a real DeliveryStore on a per-test
// Postgres. What is pinned: a delivery naming a repository outside the
// caller's org is refused before anything is persisted; an accepted one is
// persisted (redacted, never re-serialized otherwise), claimed, and run
// detached under the delivery org; a redelivery is a duplicate.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// reposIn answers FindInOrgByFullName from repo full names keyed by org.
type reposIn map[string][]string

func (r reposIn) FindInOrgByFullName(_ context.Context, org, fullName string) (*sourcecontrol.GitRepository, error) {
	if slices.Contains(r[org], fullName) {
		return &sourcecontrol.GitRepository{OrgID: org, RepoURL: "https://github.com/" + fullName}, nil
	}
	return nil, nil
}

// capturingHandler records the payload and the delivery org of every run.
type capturingHandler struct {
	mu       sync.Mutex
	payloads [][]byte
	orgs     []string
	release  chan struct{} // nil: return at once
}

func (h *capturingHandler) Handle(ctx context.Context, _, _ string, payload []byte) error {
	org, _ := DeliveryOrg(ctx)
	h.mu.Lock()
	h.payloads = append(h.payloads, payload)
	h.orgs = append(h.orgs, org)
	h.mu.Unlock()
	if h.release != nil {
		<-h.release
	}
	return nil
}

func (h *capturingHandler) runs() ([][]byte, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.payloads), slices.Clone(h.orgs)
}

// waitProcessed polls the delivery's processed_at: handlers run detached.
func waitProcessed(t *testing.T, db *gorm.DB, deliveryID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var row sourcecontrol.WebhookDelivery
		if err := db.Where("delivery_id = ?", deliveryID).First(&row).Error; err == nil && row.ProcessedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("delivery %s was not processed within 10s", deliveryID)
}

func countDeliveries(t *testing.T, db *gorm.DB, deliveryID string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&sourcecontrol.WebhookDelivery{}).Where("delivery_id = ?", deliveryID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func storedPayload(t *testing.T, db *gorm.DB, deliveryID string) []byte {
	t.Helper()
	var p sourcecontrol.WebhookPayload
	if err := db.Where("delivery_id = ?", deliveryID).First(&p).Error; err != nil {
		t.Fatalf("load payload %s: %v", deliveryID, err)
	}
	return p.Payload
}

// jsonEqual compares two JSON documents by value: webhook_payloads.payload is
// jsonb, which keeps the document, not its bytes.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return reflect.DeepEqual(x, y)
}

func TestIngest_RefusesRepoOutsideTokenOrg(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ing := NewIngestor(sourcecontrol.NewDeliveryStore(db), NewRouter(), reposIn{"org-a": {"acme/greeter"}})
	_, err := ing.Ingest(context.Background(), "org-b", "d-1", "push", []byte(`{"repository":{"full_name":"acme/greeter"}}`))
	if !errors.Is(err, ErrRepositoryUnknown) {
		t.Fatalf("err = %v, want ErrRepositoryUnknown", err)
	}
	if countDeliveries(t, db, "d-1") != 0 {
		t.Fatal("a refused delivery must not be persisted")
	}
}

// Every repo-scoped event must name a repository of the org; an event that
// names none, or is not repo-scoped at all (an App installation event), is
// refused the same way and never persisted.
func TestIngest_RefusesWhatNamesNoRepositoryOfTheOrg(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ing := NewIngestor(sourcecontrol.NewDeliveryStore(db), NewRouter(), reposIn{"org-a": {"acme/greeter"}})
	cases := []struct{ id, event, body string }{
		{"r-1", "pull_request", `{"action":"closed","repository":{"full_name":"acme/other"}}`},
		{"r-2", "issues", `{"action":"opened"}`},
		{"r-3", "ping", `{"zen":"Design for failure.","hook_id":1}`},
		{"r-4", "installation", `{"action":"deleted","installation":{"id":7},"repository":{"full_name":"acme/greeter"}}`},
		{"r-5", "issue_comment", `not json`},
	}
	for _, tc := range cases {
		if _, err := ing.Ingest(context.Background(), "org-a", tc.id, tc.event, []byte(tc.body)); !errors.Is(err, ErrRepositoryUnknown) {
			t.Errorf("%s %s: err = %v, want ErrRepositoryUnknown", tc.event, tc.body, err)
		}
		if countDeliveries(t, db, tc.id) != 0 {
			t.Errorf("%s: a refused delivery must not be persisted", tc.event)
		}
	}
}

func TestIngest_PersistsPingRawBytesAndSettles(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ing := NewIngestor(sourcecontrol.NewDeliveryStore(db), NewRouter(), reposIn{"org-a": {"acme/greeter"}})
	body := []byte(`{"zen":"Keep it logically awesome.","repository":{"full_name":"acme/greeter"}}`)
	res, err := ing.Ingest(context.Background(), "org-a", "d-2", "ping", body)
	if err != nil || res != IngestDispatched {
		t.Fatalf("res=%v err=%v", res, err)
	}
	waitProcessed(t, db, "d-2") // polls processed_at; handlers run detached
	var row sourcecontrol.WebhookDelivery
	db.Where("delivery_id = ?", "d-2").First(&row)
	if row.OcOrgID != "org-a" || row.Attempts != 1 || row.AbandonedAt != nil {
		t.Fatalf("row = %+v", row)
	}
	if got := storedPayload(t, db, "d-2"); !jsonEqual(t, got, body) {
		t.Fatalf("stored payload = %s, want %s", got, body)
	}
	again, _ := ing.Ingest(context.Background(), "org-a", "d-2", "ping", body)
	if again != IngestDuplicate {
		t.Fatalf("redelivery = %v, want duplicate", again)
	}
}

// The handlers get the exact bytes that arrived, under the delivery org; the
// stored copy is the redacted one (a published credential never lands in
// webhook_payloads), and a body without the marker is stored as it came.
func TestIngest_PersistsRawBytes(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	h := &capturingHandler{}
	router := NewRouter()
	router.Register("push", "", h)
	router.Register("issue_comment", "", h)
	ing := NewIngestor(sourcecontrol.NewDeliveryStore(db), router, reposIn{"org-a": {"acme/greeter"}})

	// Odd spacing and key order: any re-serialization would change these bytes.
	plain := []byte("{ \"ref\":\"refs/heads/main\",  \"repository\" : {\"full_name\":\"acme/greeter\"},\"a\":1 }")
	if res, err := ing.Ingest(context.Background(), "org-a", "p-1", "push", plain); err != nil || res != IngestDispatched {
		t.Fatalf("plain: res=%v err=%v", res, err)
	}
	waitProcessed(t, db, "p-1")
	if got := storedPayload(t, db, "p-1"); !jsonEqual(t, got, plain) {
		t.Fatalf("stored = %s, want %s", got, plain)
	}

	marked := []byte(`{"action":"created","comment":{"body":"` + credentialNeedle + ` user: x / pw: hunter2"},"repository":{"full_name":"acme/greeter"}}`)
	if res, err := ing.Ingest(context.Background(), "org-a", "p-2", "issue_comment", marked); err != nil || res != IngestDispatched {
		t.Fatalf("marked: res=%v err=%v", res, err)
	}
	waitProcessed(t, db, "p-2")
	stored := storedPayload(t, db, "p-2")
	if !jsonEqual(t, stored, redactPublishedCredentials(marked)) {
		t.Fatalf("stored = %s, want the redacted body", stored)
	}

	payloads, orgs := h.runs()
	if len(payloads) != 2 || string(payloads[0]) != string(plain) || string(payloads[1]) != string(marked) {
		t.Fatalf("handlers saw %q, want the exact bytes ingested", payloads)
	}
	if !slices.Equal(orgs, []string{"org-a", "org-a"}) {
		t.Fatalf("delivery orgs on the handler ctx = %v, want [org-a org-a]", orgs)
	}
}

// A redelivery while the first run still holds the lease is acked held and
// does not run the handlers a second time.
func TestIngest_DuplicateOfAHeldDeliveryIsHeld(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	h := &capturingHandler{release: make(chan struct{})}
	router := NewRouter()
	router.Register("push", "", h)
	ing := NewIngestor(sourcecontrol.NewDeliveryStore(db), router, reposIn{"org-a": {"acme/greeter"}})
	body := []byte(`{"repository":{"full_name":"acme/greeter"}}`)

	if res, err := ing.Ingest(context.Background(), "org-a", "h-1", "push", body); err != nil || res != IngestDispatched {
		t.Fatalf("first: res=%v err=%v", res, err)
	}
	res, err := ing.Ingest(context.Background(), "org-a", "h-1", "push", body)
	if err != nil || res != IngestHeld {
		t.Fatalf("second: res=%v err=%v, want held", res, err)
	}
	close(h.release)
	waitProcessed(t, db, "h-1")
	if payloads, _ := h.runs(); len(payloads) != 1 {
		t.Fatalf("handlers ran %d times, want 1", len(payloads))
	}
}

// The Replayer re-runs a stored delivery outside any request: its handlers
// still see the delivery's org, so a repository lookup stays in that org.
func TestReplayer_RunsUnderTheDeliveryOrg(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	db := dbtest.New(t)
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)
	h := &capturingHandler{}
	router := NewRouter()
	router.Register("pull_request", "", h)
	ctx := context.Background()
	if _, err := store.Persist(ctx, "rp-1", "org-b", "pull_request", "closed", []byte(`{"action":"closed"}`), deliveryLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "rp-1", 1, "boom", 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := NewReplayer(store, router, 0).Once(ctx); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if _, orgs := h.runs(); !slices.Equal(orgs, []string{"org-b"}) {
		t.Fatalf("replayed run's delivery org = %v, want [org-b]", orgs)
	}
}

func TestDeliveryOrg_UnsetAndEmpty(t *testing.T) {
	t.Parallel()
	if _, ok := DeliveryOrg(context.Background()); ok {
		t.Fatal("a bare context has no delivery org")
	}
	if _, ok := DeliveryOrg(WithDeliveryOrg(context.Background(), "")); ok {
		t.Fatal("an empty org is no delivery org")
	}
	if org, ok := DeliveryOrg(WithDeliveryOrg(context.Background(), "org-a")); !ok || org != "org-a" {
		t.Fatalf("DeliveryOrg = %q %v", org, ok)
	}
}
