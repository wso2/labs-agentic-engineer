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
// (coalesceActor, profileSummary, secretRefPath) and the entry-guard /
// error-classification branches that fire BEFORE any I/O. Every mutating
// method rejects a missing orgID before touching anything, and the three
// Thunder-backed mutations (Ensure/Revoke/Regenerate) short-circuit with
// ErrIDPThunderUnavailable when the admin client is nil — so a nil db + nil
// thunder is enough to prove the ordering (reaching either would panic).
// The SQL-shaped behavior lives in idp_dbtest_test.go; the HTTP contract in
// idp_component_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// --- shared fake Thunder admin client ---------------------------------------
//
// idpService only ever calls three of thundersvc.Client's methods
// (EnsurePublisherApp, DeletePublisherApp, RegenerateClientSecret); OUExists is
// irrelevant to this feature, so a call to it is a test bug — it panics
// (moq-style). Each fake is built per-test and driven by a single synchronous
// httptest request, so no mutex is needed for the capture slices.
type fakeThunder struct {
	// The directory half of thundersvc.Client (groups + users) belongs to the
	// identity domain, not the IDP service under test here. Embedding the
	// interface satisfies it without a wall of stubs, and any accidental call
	// panics on the nil rather than passing silently.
	thundersvc.Client

	ensureFn func(ctx context.Context, orgHandle, orgOUID string) (string, string, bool, error)
	deleteFn func(ctx context.Context, orgHandle string) (bool, error)
	regenFn  func(ctx context.Context, orgHandle string) (string, error)

	ensureCalls []ensureCall
	deleteCalls []string
	regenCalls  []string
}

type ensureCall struct{ orgHandle, orgOUID string }

var _ thundersvc.Client = (*fakeThunder)(nil)

func (f *fakeThunder) EnsurePublisherApp(ctx context.Context, orgHandle, orgOUID string) (string, string, bool, error) {
	f.ensureCalls = append(f.ensureCalls, ensureCall{orgHandle, orgOUID})
	if f.ensureFn == nil {
		return "cid-" + orgHandle, "secret-" + orgHandle, true, nil
	}
	return f.ensureFn(ctx, orgHandle, orgOUID)
}

func (f *fakeThunder) DeletePublisherApp(ctx context.Context, orgHandle string) (bool, error) {
	f.deleteCalls = append(f.deleteCalls, orgHandle)
	if f.deleteFn == nil {
		return true, nil
	}
	return f.deleteFn(ctx, orgHandle)
}

func (f *fakeThunder) RegenerateClientSecret(ctx context.Context, orgHandle string) (string, error) {
	f.regenCalls = append(f.regenCalls, orgHandle)
	if f.regenFn == nil {
		return "rotated-" + orgHandle, nil
	}
	return f.regenFn(ctx, orgHandle)
}

func (f *fakeThunder) OUExists(context.Context, string) (bool, error) {
	panic("fakeThunder: OUExists is not part of the idp feature")
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

// --- secretRefPath ----------------------------------------------------------

func TestSecretRefPath(t *testing.T) {
	t.Parallel()
	// The logical OpenBao path is persisted on the profile row; the golden
	// (testdata/harvest/golden/get_org_idp.json) shows exactly this shape.
	if got := secretRefPath("acme"); got != "secret/aep/acme/idp/publisher" {
		t.Fatalf("secretRefPath drifted: %q", got)
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

func TestProfileSummary_ProjectsAuditFieldsAndNeverLeaksSecret(t *testing.T) {
	t.Parallel()
	p := &OrganizationIDPProfile{
		Kind:                  "platform",
		Issuer:                "https://idp.test",
		JWKSURL:               "https://idp.test/jwks",
		PublisherClientID:     "aep-publisher-acme",
		PublisherClientSecret: "super-secret-value",
	}
	got := profileSummary(p)
	if got.Kind != "platform" || got.Issuer != "https://idp.test" || got.JWKSURL != "https://idp.test/jwks" {
		t.Fatalf("summary carried the wrong config fields: %+v", got)
	}
	if got.PublisherClientID != "aep-publisher-acme" {
		t.Fatalf("summary must carry the client id: %+v", got)
	}
	// The secret is projected to a bool — the raw value must NEVER reach the
	// audit trail (the whole point of profileSummary).
	if !got.HasClientSecret {
		t.Fatalf("hasClientSecret must be true when a secret is present")
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(raw), "super-secret-value") {
		t.Fatalf("audit summary leaked the raw secret: %s", raw)
	}

	// The marshaled field set is exactly the audit projection — timestamps,
	// db id and the SM-API triplet are intentionally dropped.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	want := []string{"hasClientSecret", "issuer", "jwksUrl", "kind", "publisherClientId"}
	if got := sortedKeys(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("summary field set drifted:\n got %v\nwant %v", got, want)
	}
}

func TestProfileSummary_HasClientSecretFalseWhenEmpty(t *testing.T) {
	t.Parallel()
	got := profileSummary(&OrganizationIDPProfile{Kind: "platform"})
	if got.HasClientSecret {
		t.Fatalf("hasClientSecret must be false when no secret is stored")
	}
}

// --- entry guards: missing orgID fails before any I/O -----------------------

func TestMethods_RejectEmptyOrgIDBeforeIO(t *testing.T) {
	t.Parallel()
	// nil db + nil thunder: any I/O would panic, so a clean "orgID required"
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

	_, _, _, err = svc.EnsureOrgPublisher(ctx, "", "actor")
	assertOrgIDRequired("EnsureOrgPublisher", err)

	_, err = svc.RevokeOrgPublisher(ctx, "", "actor")
	assertOrgIDRequired("RevokeOrgPublisher", err)

	_, err = svc.RegenerateClientSecret(ctx, "", "actor")
	assertOrgIDRequired("RegenerateClientSecret", err)

	_, err = svc.UpdateProfile(ctx, "", "actor", UpdateProfileRequest{})
	assertOrgIDRequired("UpdateProfile", err)
}

// --- ErrIDPThunderUnavailable: nil admin client short-circuits mutations ----

func TestMutations_ThunderNilYieldsSentinel(t *testing.T) {
	t.Parallel()
	// nil db proves the sentinel is returned BEFORE any profile read/write:
	// each Thunder-backed mutation checks s.thunder == nil first.
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	ctx := context.Background()

	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "actor"); !errors.Is(err, ErrIDPThunderUnavailable) {
		t.Fatalf("EnsureOrgPublisher: want ErrIDPThunderUnavailable, got %v", err)
	}
	if _, err := svc.RevokeOrgPublisher(ctx, "acme", "actor"); !errors.Is(err, ErrIDPThunderUnavailable) {
		t.Fatalf("RevokeOrgPublisher: want ErrIDPThunderUnavailable, got %v", err)
	}
	if _, err := svc.RegenerateClientSecret(ctx, "acme", "actor"); !errors.Is(err, ErrIDPThunderUnavailable) {
		t.Fatalf("RegenerateClientSecret: want ErrIDPThunderUnavailable, got %v", err)
	}
}

func TestEnsure_EmptyOrgIDBeatsThunderNilCheck(t *testing.T) {
	t.Parallel()
	// Ordering pin: orgID is validated before the thunder-nil check, so an
	// empty org yields "orgID required", NOT the thunder sentinel.
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	_, _, _, err := svc.EnsureOrgPublisher(context.Background(), "", "actor")
	if err == nil || errors.Is(err, ErrIDPThunderUnavailable) || !strings.Contains(err.Error(), "orgID required") {
		t.Fatalf("empty orgID must beat the thunder-nil check, got %v", err)
	}
}

// --- UpdateProfile kind validation fires before any I/O ---------------------

func TestUpdateProfile_InvalidKindRejectedBeforeIO(t *testing.T) {
	t.Parallel()
	// nil db + nil thunder: the kind guard runs before GetOrCreateProfile, so
	// an invalid kind returns cleanly without a panic.
	svc := NewIDPService(nil, nil, nil, PlatformIDPConfig{})
	_, err := svc.UpdateProfile(context.Background(), "acme", "actor", UpdateProfileRequest{Kind: "bogus"})
	if err == nil || !strings.Contains(err.Error(), `invalid kind "bogus"`) {
		t.Fatalf("want an invalid-kind error, got %v", err)
	}
}
