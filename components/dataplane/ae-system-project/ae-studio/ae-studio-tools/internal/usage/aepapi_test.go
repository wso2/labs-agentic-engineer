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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAEPAPIPost_404IsRejected(t *testing.T) {
	post, _, _ := aepAPIStub(t, http.StatusNotFound)
	err := post(context.Background(), []TurnRecord{record(1)})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want a 404 RejectedError", err)
	}
}

// Anything else is a failure to retry, naming the status, never the body.
func TestAEPAPIPost_OtherAnswersAreRetryable(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusServiceUnavailable} {
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
