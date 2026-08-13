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

package projects

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// statusOf casts a transport error to its wire status, failing the test if the
// mapper returned something that is not an *apierr.Error.
func statusOf(t *testing.T, err error) int {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected an *apierr.Error, got %T (%v)", err, err)
	}
	return ae.Status
}

// TestMapComponentError pins the mapper directly: every OpenChoreo sentinel
// that componentService passes through is translated to its HTTP status via
// the shared ocerr classifier, and anything that is not an OC sentinel
// collapses to a fixed-message 500 that never leaks the internal cause.
// (Moved here with the mapper when the projects HTTP handlers were extracted
// into their slices — the shared mapper lives in the domain root the slices
// import.)
func TestMapComponentError(t *testing.T) {
	t.Parallel()

	ocCases := []struct {
		err  error
		want int
	}{
		{openchoreo.ErrUnauthorized, http.StatusUnauthorized},
		{openchoreo.ErrForbidden, http.StatusForbidden},
		{openchoreo.ErrNotFound, http.StatusNotFound},
		{openchoreo.ErrConflict, http.StatusConflict},
		{openchoreo.ErrBadRequest, http.StatusBadRequest},
		{openchoreo.ErrPaymentRequired, http.StatusPaymentRequired},
	}
	for _, tc := range ocCases {
		err := MapComponentError(tc.err, "failed to do thing")
		if got := statusOf(t, err); got != tc.want {
			t.Fatalf("MapComponentError(%v) → %v, want status %d", tc.err, err, tc.want)
		}
	}

	// Anything that is not an OC sentinel → opaque 500 carrying the supplied
	// internal message, never the raw error.
	err := MapComponentError(errors.New("pg: connection refused"), "failed to list components")
	if got := statusOf(t, err); got != http.StatusInternalServerError {
		t.Fatalf("opaque error must map to 500, got %v", err)
	}
	if strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("500 must not leak internals: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to list components") {
		t.Fatalf("500 must carry the supplied internal message: %v", err)
	}
}

func TestMapProjectError_PaymentRequiredForwardsPlatformMessage(t *testing.T) {
	t.Parallel()
	platform := "Quota limit reached for projects. Upgrade your subscription to continue."
	err := MapProjectError(fmt.Errorf("%w: %s", openchoreo.ErrPaymentRequired, platform))
	var ae *apierr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("want *apierr.Error, got %T (%v)", err, err)
	}
	if ae.Status != http.StatusPaymentRequired || ae.Code != apierr.CodePaymentRequired {
		t.Fatalf("got status=%d code=%q, want 402 payment_required", ae.Status, ae.Code)
	}
	if ae.Message != platform {
		t.Fatalf("message = %q, want the platform sentence", ae.Message)
	}
}
