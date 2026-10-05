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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	orghttpapi "github.com/wso2/aep/aep-api/internal/organization/httpapi"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// studioByOrg answers each org its own AE Studio state, so a read that crossed
// orgs would show up as the wrong state.
type studioByOrg map[string]organization.AEStudioStatus

func (m studioByOrg) Status(_ context.Context, org string) (organization.AEStudioStatus, error) {
	if st, ok := m[org]; ok {
		return st, nil
	}
	return organization.AEStudioStatus{State: organization.AEStudioAbsent}, nil
}

func aeStudioHandler(t *testing.T, claims *auth.Claims) http.Handler {
	t.Helper()
	orgs, err := orghttpapi.New(organization.Deps{AEStudio: studioByOrg{
		"acme": {State: organization.AEStudioReady, URLs: &organization.AEStudioURLs{DesignAgent: "http://d", Collab: "ws://c", Tools: "http://t"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
		})
	}
	return NewHandlerForTest(Deps{Organization: orgs}, inject)
}

func getAEStudio(h http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

// A user reads their own org's AE Studio; a user of another org reads their
// own, never this one's — even when the request names acme.
func TestGetAeStudio_ReadsOnlyTheTokenOrg(t *testing.T) {
	own := getAEStudio(aeStudioHandler(t, &auth.Claims{Subject: "u", OuHandle: "acme"}), "/api/v1/ae-studio")
	if own.Code != http.StatusOK || !strings.Contains(own.Body.String(), `"state":"ready"`) {
		t.Fatalf("own org: %d %s", own.Code, own.Body)
	}
	other := aeStudioHandler(t, &auth.Claims{Subject: "v", OuHandle: "globex"})
	for _, target := range []string{"/api/v1/ae-studio", "/api/v1/ae-studio?org=acme&orgHandle=acme"} {
		w := getAEStudio(other, target)
		if strings.Contains(w.Body.String(), "ready") || strings.Contains(w.Body.String(), "http://d") {
			t.Fatalf("%s leaked acme's AE Studio to a globex user: %d %s", target, w.Code, w.Body)
		}
	}
	if w := getAEStudio(other, "/api/v1/ae-studio"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"absent"`) {
		t.Fatalf("globex user: %d %s", w.Code, w.Body)
	}
}

// An M2M bearer carries no org claim: the /api/v1 gate refuses it with 401.
func TestGetAeStudio_M2MBearerIs401(t *testing.T) {
	w := getAEStudio(aeStudioHandler(t, &auth.Claims{Subject: "sre-agent-client"}), "/api/v1/ae-studio")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", w.Code, w.Body)
	}
}
