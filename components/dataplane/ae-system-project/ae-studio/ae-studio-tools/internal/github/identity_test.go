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

package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testPAT = "ghp_test-not-a-real-token"

// fakeGitHub answers GET /user like api.github.com when the request carries
// the expected bearer PAT, and replies with status and headers otherwise set.
func fakeGitHub(t *testing.T, status int, headers map[string]string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/user" {
			t.Errorf("request %s %s, want GET /user", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testPAT {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWhoami_ReturnsLoginAndID(t *testing.T) {
	srv := fakeGitHub(t, http.StatusOK, nil, `{"login":"e2e-bot","id":42,"name":"ignored"}`)
	login, id, err := newClient(srv.URL, testPAT).Whoami(context.Background())
	if err != nil || login != "e2e-bot" || id != 42 {
		t.Fatalf("Whoami = %q, %d, %v", login, id, err)
	}
}

func TestWhoami_WrongPATIsStatusError(t *testing.T) {
	srv := fakeGitHub(t, http.StatusOK, nil, `{"login":"e2e-bot","id":42}`)
	_, _, err := newClient(srv.URL, "ghp_wrong").Whoami(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want StatusError 401", err)
	}
}

func TestWhoami_RateLimited(t *testing.T) {
	reset := strconv.FormatInt(time.Now().Add(90*time.Second).Unix(), 10)
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		min     time.Duration
		max     time.Duration
	}{
		{"secondary 429 Retry-After", http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, 30 * time.Second, 30 * time.Second},
		{"secondary 403 Retry-After", http.StatusForbidden, map[string]string{"Retry-After": "7"}, 7 * time.Second, 7 * time.Second},
		{"primary 403 exhausted", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": reset}, 80 * time.Second, 91 * time.Second},
		{"429 without hints", http.StatusTooManyRequests, nil, defaultRetryAfter, defaultRetryAfter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeGitHub(t, c.status, c.headers, `{"message":"rate limited"}`)
			_, _, err := newClient(srv.URL, testPAT).Whoami(context.Background())
			var rl *ErrRateLimited
			if !errors.As(err, &rl) {
				t.Fatalf("err = %v, want ErrRateLimited", err)
			}
			if rl.RetryAfter < c.min || rl.RetryAfter > c.max {
				t.Fatalf("RetryAfter = %v, want [%v, %v]", rl.RetryAfter, c.min, c.max)
			}
		})
	}
}

func TestWhoami_ForbiddenWithoutRateLimitIsStatusError(t *testing.T) {
	srv := fakeGitHub(t, http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "4999"}, `{}`)
	_, _, err := newClient(srv.URL, testPAT).Whoami(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusForbidden {
		t.Fatalf("err = %v, want StatusError 403", err)
	}
}

func TestWhoami_MalformedBody(t *testing.T) {
	for _, body := range []string{`not json`, `{"login":"","id":42}`, `{"login":"x","id":0}`} {
		srv := fakeGitHub(t, http.StatusOK, nil, body)
		_, _, err := newClient(srv.URL, testPAT).Whoami(context.Background())
		var se *StatusError
		if err == nil || errors.As(err, &se) {
			t.Fatalf("body %q: err = %v, want a decode error", body, err)
		}
	}
}

func TestWhoami_ErrorsNeverCarryThePAT(t *testing.T) {
	_, _, err := newClient("http://127.0.0.1:1", testPAT).Whoami(context.Background())
	if err == nil || strings.Contains(err.Error(), testPAT) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusErr_ResetUnderHalfASecondWaitsAtLeastOneSecond(t *testing.T) {
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	now := reset.Add(-300 * time.Millisecond) // rounds to 0 s
	resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{
		"X-Ratelimit-Remaining": {"0"},
		"X-Ratelimit-Reset":     {strconv.FormatInt(reset.Unix(), 10)},
	}}
	var rl *ErrRateLimited
	if err := statusErr(resp, now); !errors.As(err, &rl) || rl.RetryAfter < time.Second {
		t.Fatalf("err = %v, want ErrRateLimited with RetryAfter >= 1s", err)
	}
}
