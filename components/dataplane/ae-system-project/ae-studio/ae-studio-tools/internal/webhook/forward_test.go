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

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

// newAEPClient is the generated aep-api client against a fake aep-api root
// (the /internal/v1 prefix is part of the base URL, as platform.NewAEPAPI
// builds it).
func newAEPClient(t *testing.T, base string) *aepapi.ClientWithResponses {
	t.Helper()
	c, err := aepapi.NewClientWithResponses(base + "/internal/v1")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// noBackoff keeps the retry tests fast.
func noBackoff(f *Forwarder) *Forwarder { f.backoff = 0; return f }

func TestForward_AcceptsAndRefusesPerTheReplyRule(t *testing.T) {
	body := []byte(`{"repository":{"full_name":"acme/greeter"}}`)
	for name, tc := range map[string]struct {
		upstream  int
		wantErr   bool
		wantCalls int32
	}{
		"dispatched":         {http.StatusAccepted, false, 1},
		"duplicate":          {http.StatusOK, false, 1},
		"unknown repository": {http.StatusNotFound, false, 1},
		"bad request":        {http.StatusBadRequest, false, 1},
		"down":               {http.StatusInternalServerError, true, 3},
		"lookup outage":      {http.StatusServiceUnavailable, true, 3},
		"token refused":      {http.StatusUnauthorized, true, 1},
		"forbidden":          {http.StatusForbidden, true, 1},
		"throttled":          {http.StatusTooManyRequests, true, 1},
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.upstream)
			}))
			defer srv.Close()
			f := noBackoff(NewForwarder(newAEPClient(t, srv.URL)))
			upstream, err := f.Forward(context.Background(), "d-1", "push", body)
			if tc.wantErr != (err != nil) || (err != nil && !errors.Is(err, ErrUpstreamUnavailable)) {
				t.Fatalf("err = %v, want unavailable %v", err, tc.wantErr)
			}
			if upstream != tc.upstream {
				t.Fatalf("upstream = %d, want %d", upstream, tc.upstream)
			}
			if calls.Load() != tc.wantCalls {
				t.Fatalf("%d calls, want %d", calls.Load(), tc.wantCalls)
			}
		})
	}
}

// The HMAC GitHub made is over these exact bytes, and aep-api
// hands them to its handlers; nothing in between may re-serialize them.
func TestForward_SendsExactBytes(t *testing.T) {
	body := []byte("{\"a\":  1,\n \"repository\":{\"full_name\":\"acme/greeter\"}}") // odd spacing on purpose
	var got []byte
	var delivery, event, ctype, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		delivery, event = r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event")
		ctype, path = r.Header.Get("Content-Type"), r.Method+" "+r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	if _, err := NewForwarder(newAEPClient(t, srv.URL)).Forward(context.Background(), "d-1", "push", body); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("forwarded body changed: %q", got)
	}
	if delivery != "d-1" || event != "push" || ctype != "application/json" || path != "POST /internal/v1/ae-studio/webhook-events" {
		t.Fatalf("delivery %q event %q content-type %q %s", delivery, event, ctype, path)
	}
}

func TestForward_RetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	upstream, err := noBackoff(NewForwarder(newAEPClient(t, srv.URL))).Forward(context.Background(), "d-1", "push", []byte(`{}`))
	if err != nil || upstream != http.StatusAccepted || calls.Load() != 3 {
		t.Fatalf("upstream %d err %v calls %d", upstream, err, calls.Load())
	}
}

func TestForward_UnreachableIsUnavailableWithStatusZero(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing listens there any more
	upstream, err := noBackoff(NewForwarder(newAEPClient(t, base))).Forward(context.Background(), "d-1", "push", []byte(`{}`))
	if !errors.Is(err, ErrUpstreamUnavailable) || upstream != 0 {
		t.Fatalf("upstream %d err %v", upstream, err)
	}
}

// A hung aep-api must not hold the delivery past the budget: each try gets a
// share of it, so the retries still happen inside GitHub's 10 s.
func TestForward_HungUpstreamStaysInsideTheBudget(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	f := noBackoff(NewForwarder(newAEPClient(t, srv.URL)))
	f.budget = 300 * time.Millisecond
	start := time.Now()
	upstream, err := f.Forward(context.Background(), "d-1", "push", []byte(`{}`))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v, budget 300ms", elapsed)
	}
	if !errors.Is(err, ErrUpstreamUnavailable) || upstream != 0 || calls.Load() != 3 {
		t.Fatalf("upstream %d err %v calls %d", upstream, err, calls.Load())
	}
}
