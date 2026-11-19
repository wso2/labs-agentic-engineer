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

package organization

// UNIT tier: the REAL idpService logic that is
// reachable with NO database and NO Thunder — the pure helpers
// (coalesceActor, profileSummary) and the entry-guard /
// error-classification branches that fire BEFORE any I/O. Every mutating
// method rejects a missing orgID before touching anything, and EnsureClient
// fails before any I/O when the service has no clients record, so a nil db
// and no record are enough to prove the ordering (reaching either would
// panic).
// The SQL-shaped behavior lives in idp_dbtest_test.go; the HTTP contract in
// idp_component_test.go.

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// sortedKeys returns the sorted JSON keys of a decoded object.
func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// --- coalesceActor ----------------------------------------------------------

func TestCoalesceActor(t *testing.T) {
	t.Parallel()
	if got := coalesceActor(""); got != "system" {
		t.Fatalf("empty actor must default to %q, got %q", "system", got)
	}
	if got := coalesceActor("ada@x.io"); got != "ada@x.io" {
		t.Fatalf("named actor must pass through, got %q", got)
	}
}

// --- profileSummary ---------------------------------------------------------

func TestProfileSummary_NilIsZero(t *testing.T) {
	t.Parallel()
	got := profileSummary(nil)
	if got != (profileSummaryFields{}) {
		t.Fatalf("nil profile must summarise to the zero value, got %+v", got)
	}
}

func TestProfileSummary_ProjectsAuditFields(t *testing.T) {
	t.Parallel()
	p := &OrganizationIDPProfile{
		Kind:              "platform",
		Issuer:            "https://idp.test",
		JWKSURL:           "https://idp.test/jwks",
		PublisherClientID: "aep-publisher-acme",
	}
	got := profileSummary(p)
	if got.Kind != "platform" || got.Issuer != "https://idp.test" || got.JWKSURL != "https://idp.test/jwks" {
		t.Fatalf("summary carried the wrong config fields: %+v", got)
	}
	if got.PublisherClientID != "aep-publisher-acme" {
		t.Fatalf("summary must carry the client id: %+v", got)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	// The marshaled field set is exactly the audit projection — timestamps,
	// db id and the Thunder entity id are intentionally dropped, and the
	// profile holds no secret to summarise.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	want := []string{"issuer", "jwksUrl", "kind", "publisherClientId"}
	if got := sortedKeys(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("summary field set drifted:\n got %v\nwant %v", got, want)
	}
}

// --- entry guards: missing orgID fails before any I/O -----------------------

func TestMethods_RejectEmptyOrgIDBeforeIO(t *testing.T) {
	t.Parallel()
	// nil db + nil clients: any I/O would panic, so a clean "orgID required"
	// error proves the guard fires first on every method.
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	ctx := context.Background()

	assertOrgIDRequired := func(name string, err error) {
		if err == nil || !strings.Contains(err.Error(), "orgID required") {
			t.Fatalf("%s: want an 'orgID required' error, got %v", name, err)
		}
	}

	_, err := svc.GetProfile(ctx, "")
	assertOrgIDRequired("GetProfile", err)

	_, err = svc.GetOrCreateProfile(ctx, "")
	assertOrgIDRequired("GetOrCreateProfile", err)

	err = svc.EnsureClient(ctx, "", ClientPublisher)
	assertOrgIDRequired("EnsureClient", err)

	err = svc.RequirePublisherForBuild(ctx, "")
	assertOrgIDRequired("RequirePublisherForBuild", err)

	_, err = svc.UpdateProfile(ctx, "", "actor", UpdateProfileRequest{})
	assertOrgIDRequired("UpdateProfile", err)
}

// --- no clients record: the ensure fails before any I/O ----------------------

func TestEnsureClient_NoClientsRecordFailsBeforeIO(t *testing.T) {
	t.Parallel()
	// nil db proves the error is returned BEFORE any profile read/write.
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	err := svc.EnsureClient(context.Background(), "acme", ClientPublisher)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("EnsureClient: want a 'not configured' error, got %v", err)
	}
}

func TestEnsureClient_EmptyOrgIDBeatsTheClientsCheck(t *testing.T) {
	t.Parallel()
	// Ordering pin: orgID is validated first, so an empty org yields
	// "orgID required".
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	err := svc.EnsureClient(context.Background(), "", ClientPublisher)
	if err == nil || !strings.Contains(err.Error(), "orgID required") {
		t.Fatalf("empty orgID must beat the clients check, got %v", err)
	}
}

// --- UpdateProfile kind validation fires before any I/O ---------------------

func TestUpdateProfile_InvalidKindRejectedBeforeIO(t *testing.T) {
	t.Parallel()
	// nil db + nil clients: the kind guard runs before GetOrCreateProfile, so
	// an invalid kind returns cleanly without a panic.
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	_, err := svc.UpdateProfile(context.Background(), "acme", "actor", UpdateProfileRequest{Kind: "bogus"})
	if err == nil || !strings.Contains(err.Error(), `invalid kind "bogus"`) {
		t.Fatalf("want an invalid-kind error, got %v", err)
	}
}
