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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// writeFor runs err through the strict handler's error writer.
func writeFor(t *testing.T, err error) (int, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	writeResponseError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil), err)
	var body struct{ Code string }
	if jerr := json.Unmarshal(rec.Body.Bytes(), &body); jerr != nil {
		t.Fatalf("body: %v", jerr)
	}
	return rec.Code, body.Code, rec.Header().Get("Retry-After")
}

func TestWriteResponseError_ClassifiesAEStudio(t *testing.T) {
	t.Parallel()
	wrapped := func(e error) error { return fmt.Errorf("read: %w", e) }
	for _, tc := range []struct {
		name       string
		err        error
		status     int
		code       string
		retryAfter string
	}{
		{"absent, bare", wrapped(sourcecontrol.ErrAEStudioAbsent), 409, "github_not_connected", ""},
		{"unavailable behind a 500 fallback", apierr.WithCause(apierr.Internal("internal error"), wrapped(sourcecontrol.ErrAEStudioUnavailable)), 503, "ae_studio_unavailable", "5"},
		{"misconfigured behind a 502", apierr.WithCause(apierr.BadGateway("x"), sourcecontrol.ErrAEStudioMisconfigured), 503, "ae_studio_misconfigured", ""},
		{"owner not allowed (Q-8)", wrapped(sourcecontrol.ErrOwnerNotAllowed), 409, "owner_not_allowed", ""},
		{"rate limited, GitHub's Retry-After", wrapped(&sourcecontrol.RateLimitedError{RetryAfter: 1500 * time.Millisecond}), 429, "github_rate_limited", "2"},
		{"rate limited, GitHub said nothing", &sourcecontrol.RateLimitedError{}, 429, "github_rate_limited", ""},
		// A slice's own 4xx verdict stands even with a cause attached.
		{"a 4xx verdict stands", apierr.WithCause(apierr.NotFound("gone"), sourcecontrol.ErrAEStudioUnavailable), 404, "not_found", ""},
		{"a 500 with no AE cause", apierr.WithCause(apierr.Internal("internal error"), errors.New("db down")), 500, "internal_error", ""},
		{"unclassified", errors.New("boom"), 500, "internal_error", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, code, ra := writeFor(t, tc.err)
			if status != tc.status || code != tc.code || ra != tc.retryAfter {
				t.Fatalf("got %d %s Retry-After=%q, want %d %s %q", status, code, ra, tc.status, tc.code, tc.retryAfter)
			}
		})
	}
}
