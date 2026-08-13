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

package ocerr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// TestStatus pins the one shared OC-sentinel → HTTP-status classification the
// three features now route through. Both a bare sentinel and a wrapped one
// classify (errors.Is walks the chain); a non-OC error returns ok=false so the
// caller can fall back to its own opaque 500.
func TestStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"bad request", openchoreo.ErrBadRequest, http.StatusBadRequest},
		{"unauthorized", openchoreo.ErrUnauthorized, http.StatusUnauthorized},
		{"forbidden", openchoreo.ErrForbidden, http.StatusForbidden},
		{"not found", openchoreo.ErrNotFound, http.StatusNotFound},
		{"conflict", openchoreo.ErrConflict, http.StatusConflict},
		{"payment required", openchoreo.ErrPaymentRequired, http.StatusPaymentRequired},
		{"internal", openchoreo.ErrInternalServerError, http.StatusInternalServerError},
		{"wrapped not found still classifies", fmt.Errorf("get: %w", openchoreo.ErrNotFound), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Status(tc.err)
			if !ok || got != tc.want {
				t.Fatalf("Status(%v) = (%d,%v), want (%d,true)", tc.err, got, ok, tc.want)
			}
		})
	}

	if got, ok := Status(errors.New("pg: connection refused")); ok || got != 0 {
		t.Fatalf("opaque error must not classify: got (%d,%v)", got, ok)
	}
	if got, ok := Status(nil); ok || got != 0 {
		t.Fatalf("nil must not classify: got (%d,%v)", got, ok)
	}
}
