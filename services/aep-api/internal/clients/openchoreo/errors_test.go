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

package openchoreo

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// Pure-logic coverage for handleErrorResponse — the status→sentinel classifier
// callers branch on via errors.Is. Two paths: the typed-error-field branch
// (gen produced a typed body) and the bare-status fall-through (gateway-shaped
// error / schema mismatch).

func TestHandleErrorResponse_TypedFieldWrapsSentinelAndMessage(t *testing.T) {
	t.Parallel()
	msg := "the human-readable description"
	cases := []struct {
		name     string
		errs     ErrorResponses
		sentinel error
	}{
		{"400", ErrorResponses{JSON400: &gen.ErrorResponse{Error: msg}}, ErrBadRequest},
		{"401", ErrorResponses{JSON401: &gen.ErrorResponse{Error: msg}}, ErrUnauthorized},
		{"403", ErrorResponses{JSON403: &gen.ErrorResponse{Error: msg}}, ErrForbidden},
		{"404", ErrorResponses{JSON404: &gen.ErrorResponse{Error: msg}}, ErrNotFound},
		{"409", ErrorResponses{JSON409: &gen.ErrorResponse{Error: msg}}, ErrConflict},
		{"500", ErrorResponses{JSON500: &gen.ErrorResponse{Error: msg}}, ErrInternalServerError},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// statusCode is deliberately 0 here — the typed field, not the
			// numeric code, drives classification on this branch.
			err := handleErrorResponse(0, c.errs)
			if !errors.Is(err, c.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false; want the matching sentinel", err, c.sentinel)
			}
			if !strings.Contains(err.Error(), msg) {
				t.Errorf("wrapped error should carry the human message; got %q", err.Error())
			}
		})
	}
}

// The typed field wins even when it disagrees with the numeric status — the
// switch checks the populated body first.
func TestHandleErrorResponse_TypedFieldBeatsStatusCode(t *testing.T) {
	t.Parallel()
	err := handleErrorResponse(500, ErrorResponses{JSON400: &gen.ErrorResponse{Error: "bad input"}})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("typed JSON400 must classify as ErrBadRequest even with statusCode 500; got %v", err)
	}
	if errors.Is(err, ErrInternalServerError) {
		t.Errorf("must not also classify as ErrInternalServerError: %v", err)
	}
}

func TestHandleErrorResponse_BareStatusFallThrough(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status   int
		sentinel error
	}{
		{400, ErrBadRequest},
		{401, ErrUnauthorized},
		{403, ErrForbidden},
		{404, ErrNotFound},
		{402, ErrPaymentRequired},
		{409, ErrConflict},
		{500, ErrInternalServerError},
	}
	for _, c := range cases {
		c := c
		t.Run("status-"+strconv.Itoa(c.status), func(t *testing.T) {
			t.Parallel()
			// No typed fields populated → synthesize from the bare status.
			err := handleErrorResponse(c.status, ErrorResponses{})
			if !errors.Is(err, c.sentinel) {
				t.Fatalf("bare status %d: errors.Is(%v, sentinel) = false", c.status, err)
			}
		})
	}
}

func TestHandleErrorResponse_UnexpectedStatusIsUnclassified(t *testing.T) {
	t.Parallel()
	err := handleErrorResponse(418, ErrorResponses{})
	for _, s := range []error{ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrNotFound, ErrConflict, ErrInternalServerError} {
		if errors.Is(err, s) {
			t.Fatalf("unexpected status 418 must not match sentinel %v", s)
		}
	}
	if !strings.Contains(err.Error(), "418") {
		t.Errorf("unexpected-status error should name the code; got %q", err.Error())
	}
}

func TestHumanErrorMessage_PlatformEnvelope(t *testing.T) {
	t.Parallel()
	got := humanErrorMessage(
		[]byte(`{"success":false,"error":"Quota limit reached for projects. Upgrade your subscription to continue.","correlationId":"x"}`),
		"fallback",
	)
	if got != "Quota limit reached for projects. Upgrade your subscription to continue." {
		t.Fatalf("got %q", got)
	}
	if got := humanErrorMessage(nil, "fallback"); got != "fallback" {
		t.Fatalf("empty body = %q, want fallback", got)
	}
}
