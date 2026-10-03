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

package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

// aepAPIStub is aep-api's record-turn-usage: it records the request and
// answers status.
func aepAPIStub(t *testing.T, status int) (func(context.Context, []TurnRecord) error, *http.Request, *[]byte) {
	t.Helper()
	var got http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r
		gotBody, _ = io.ReadAll(r.Body)
		if status != http.StatusAccepted {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"code":"project_unknown","message":"greeter is not the org's"}`)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	c, err := aepapi.NewClientWithResponses(srv.URL + "/internal/v1")
	if err != nil {
		t.Fatal(err)
	}
	return NewAEPAPIPost(c), &got, &gotBody
}

func TestAEPAPIPost_SendsTheBatch(t *testing.T) {
	post, req, body := aepAPIStub(t, http.StatusAccepted)
	at := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	withAuthor := record(1)
	withAuthor.Author = &aepapi.AEStudioTurnRecordAuthor{ID: "u1", Name: "Ada"}
	withAuthor.StartedAt, withAuthor.FinishedAt = at, at
	marketplace := record(2)
	marketplace.Project = ""
	if err := post(context.Background(), []TurnRecord{withAuthor, marketplace}); err != nil {
		t.Fatalf("post = %v", err)
	}
	if req.Method != http.MethodPost || req.URL.Path != "/internal/v1/ae-studio/turn-usage" || req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %s %s %s", req.Method, req.URL.Path, req.Header.Get("Content-Type"))
	}
	var sent struct {
		Records []map[string]json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(*body, &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Records) != 2 {
		t.Fatalf("sent = %s", *body)
	}
	if string(sent.Records[0]["author"]) != `{"id":"u1","name":"Ada"}` || string(sent.Records[0]["project"]) != `"greeter"` {
		t.Fatalf("first record = %s", *body)
	}
	// No author and no project: the keys are absent, not empty.
	if _, ok := sent.Records[1]["author"]; ok {
		t.Fatalf("an absent author was sent: %s", *body)
	}
	if _, ok := sent.Records[1]["project"]; ok {
		t.Fatalf("a marketplace record was sent a project: %s", *body)
	}
}

// 404 (a foreign or unknown project), 400, 413 and 422 (a batch aep-api
// will never take) are permanent: the batch is rejected.
func TestAEPAPIPost_PermanentRefusalsAreRejected(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		post, _, _ := aepAPIStub(t, status)
		err := post(context.Background(), []TurnRecord{record(1)})
		var rejected *RejectedError
		if !errors.As(err, &rejected) || rejected.Status != status {
			t.Fatalf("%d: err = %v, want a RejectedError with the status", status, err)
		}
	}
}

// Anything else is a failure to retry, naming the status, never the body.
func TestAEPAPIPost_OtherAnswersAreRetryable(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		post, _, _ := aepAPIStub(t, status)
		err := post(context.Background(), []TurnRecord{record(1)})
		var rejected *RejectedError
		if err == nil || errors.As(err, &rejected) {
			t.Fatalf("%d: err = %v, want a retryable failure", status, err)
		}
		if strings.Contains(err.Error(), "greeter") {
			t.Fatalf("%d: the error carries the body: %v", status, err)
		}
	}
}

// sequencedAEPAPI is record-turn-usage answering the queued statuses in
// order (202 once they run out) and counting the records it accepted.
func sequencedAEPAPI(t *testing.T, statuses ...int) (func(context.Context, []TurnRecord) error, func() (calls, accepted int)) {
	t.Helper()
	var mu sync.Mutex
	calls, accepted := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Records []json.RawMessage `json:"records"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		calls++
		status := http.StatusAccepted
		if len(statuses) > 0 {
			status, statuses = statuses[0], statuses[1:]
		}
		if status == http.StatusAccepted {
			accepted += len(body.Records)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	c, err := aepapi.NewClientWithResponses(srv.URL + "/internal/v1")
	if err != nil {
		t.Fatal(err)
	}
	return NewAEPAPIPost(c), func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return calls, accepted
	}
}

// I-1: a batch aep-api permanently refuses is dropped, so it cannot block
// the records behind it: one enqueued after it is delivered within the
// coalescing window.
func TestSender_PermanentRefusalDoesNotBlockLaterRecords(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		post, counts := sequencedAEPAPI(t, status)
		logs := &syncBuffer{}
		s := New(post)
		s.Log = slog.New(slog.NewJSONHandler(logs, nil))
		s.Window = 50 * time.Millisecond
		s.Retry = time.Hour
		stop := runSender(s)

		s.Enqueue(record(1))
		waitFor(t, func() bool { c, _ := counts(); return c == 1 })
		s.Enqueue(record(2))
		start := time.Now()
		waitFor(t, func() bool { _, a := counts(); return a == 1 })
		if took := time.Since(start); took > time.Second {
			t.Fatalf("%d: the later record took %v", status, took)
		}
		stop()
		if c, a := counts(); c != 2 || a != 1 {
			t.Fatalf("%d: calls %d accepted %d, want the refused batch sent once and the later record delivered", status, c, a)
		}
		out := logs.String()
		if !strings.Contains(out, `"msg":"usage.dropped"`) || !strings.Contains(out, `"reason":"rejected"`) ||
			!strings.Contains(out, fmt.Sprintf(`"status":%d`, status)) || !strings.Contains(out, `"count":1`) {
			t.Fatalf("%d: usage.dropped {count, reason, status} not logged: %s", status, out)
		}
		if strings.Contains(out, "greeter") {
			t.Fatalf("%d: a log carries a record value: %s", status, out)
		}
	}
}

// A 500 is transient: the batch is kept and the retry delivers it.
func TestSender_ServerErrorIsRetried(t *testing.T) {
	post, counts := sequencedAEPAPI(t, http.StatusInternalServerError)
	s := New(post)
	s.Log = slog.New(slog.NewJSONHandler(&syncBuffer{}, nil))
	s.Window = 10 * time.Millisecond
	s.Retry = 50 * time.Millisecond
	stop := runSender(s)
	defer stop()

	s.Enqueue(record(1))
	waitFor(t, func() bool { _, a := counts(); return a == 1 })
	if c, _ := counts(); c != 2 {
		t.Fatalf("calls = %d, want the 500 then the retry", c)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAEPAPIPost_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c, err := aepapi.NewClientWithResponses(srv.URL + "/internal/v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAEPAPIPost(c)(context.Background(), []TurnRecord{record(1)}); err == nil {
		t.Fatal("unreachable aep-api answered no error")
	}
}
