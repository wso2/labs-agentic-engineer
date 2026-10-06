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

// RequirePublisherForBuild: the POST /build gate is a read of the org's
// ae-publisher-client row and nothing else. package organization (not
// organization_test) because these tests and client_ensure_test.go share the
// unexported in-memory fixtures below and fakeThunder from idp_service_test.go.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// --- in-memory IDPRepository -------------------------------------------------

// memIDPRepo is a minimal IDPRepository fake: one profile per org, held in a
// map. UpdateProfileColumns applies the profile's columns and refuses any
// other name, so a write of a column the entity no longer has (a secret, a
// secret reference) fails here as it would against the migrated schema.
type memIDPRepo struct {
	profiles map[string]*OrganizationIDPProfile
	audits   []IDPAuditEvent
}

func newMemIDPRepo() *memIDPRepo {
	return &memIDPRepo{profiles: map[string]*OrganizationIDPProfile{}}
}

var _ IDPRepository = (*memIDPRepo)(nil)

func (r *memIDPRepo) GetProfileByOrgID(_ context.Context, orgID string) (*OrganizationIDPProfile, error) {
	row, ok := r.profiles[orgID]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}

func (r *memIDPRepo) CreateProfile(_ context.Context, profile *OrganizationIDPProfile) error {
	cp := *profile
	r.profiles[profile.OrgID] = &cp
	return nil
}

// UpdateProfileColumns applies updates onto the stored row keyed by orgID
// (mirrors the real repository's Where("org_id = ?", orgID)).
func (r *memIDPRepo) UpdateProfileColumns(_ context.Context, _ *OrganizationIDPProfile, orgID string, updates map[string]interface{}) error {
	row, ok := r.profiles[orgID]
	if !ok {
		return errors.New("memIDPRepo: no profile for org " + orgID)
	}
	for k, v := range updates {
		switch k {
		case "kind":
			row.Kind = memColStr(v)
		case "issuer":
			row.Issuer = memColStr(v)
		case "jwks_url":
			row.JWKSURL = memColStr(v)
		case "publisher_client_id":
			row.PublisherClientID = memColStr(v)
		case "publisher_thunder_app_id":
			row.PublisherThunderAppID = memColStr(v)
		case "studio_client_id":
			row.StudioClientID = memColStr(v)
		case "studio_thunder_app_id":
			row.StudioThunderAppID = memColStr(v)
		case "updated_at":
			if t, ok := v.(time.Time); ok {
				row.UpdatedAt = t
			}
		default:
			return fmt.Errorf("memIDPRepo: organization_idp_profiles has no column %q", k)
		}
	}
	return nil
}

func (r *memIDPRepo) CreateAuditEvent(_ context.Context, event *IDPAuditEvent) error {
	r.audits = append(r.audits, *event)
	return nil
}

// memColStr normalises a map[string]interface{} column value that may arrive
// as nil, string, or *string.
func memColStr(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case *string:
		if t == nil {
			return ""
		}
		return *t
	default:
		return ""
	}
}

// --- stub OrganizationRepository ---------------------------------------------

// stubOrgRepo is an OrganizationRepository that never has an org row, so the
// publisher's OU lookup (lookupOrgOUID) falls back to the default OU.
type stubOrgRepo struct{}

var _ OrganizationRepository = stubOrgRepo{}

func (stubOrgRepo) ListByNames(context.Context, []string) ([]Organization, error) { return nil, nil }
func (stubOrgRepo) GetByName(context.Context, string) (*Organization, error)      { return nil, nil }
func (stubOrgRepo) Create(context.Context, *Organization) error                   { return nil }
func (stubOrgRepo) SetThunderOrgUUID(context.Context, string, uuid.UUID) error    { return nil }

// --- RequirePublisherForBuild ------------------------------------------------

// failingOrgSecretRows fails every row read.
type failingOrgSecretRows struct{}

func (failingOrgSecretRows) Get(context.Context, string, OrgSecret) (*OrgSecretRef, error) {
	return nil, errors.New("db down")
}

// assertNoThunderCalls fails the test when the gate reached Thunder.
func assertNoThunderCalls(t *testing.T, th *fakeThunder) {
	t.Helper()
	if n := len(th.ensureCalls) + len(th.deleteCalls) + len(th.setSecretCalls); n != 0 {
		t.Fatalf("the build gate made %d Thunder calls; it only reads the row", n)
	}
}

func TestRequirePublisherForBuild_NoRowFailsClosedWithoutThunder(t *testing.T) {
	t.Parallel()
	th := &fakeThunder{}
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, th, PlatformIDPConfig{}).
		WithOrgSecretRefs(newMemOrgSecretRepo()) // no ae-publisher-client row

	err := svc.RequirePublisherForBuild(context.Background(), "acme")
	if !errors.Is(err, delivery.ErrPublisherCredentialsMissing) {
		t.Fatalf("err = %v, want ErrPublisherCredentialsMissing", err)
	}
	if !strings.Contains(err.Error(), "Reconnect GitHub to set up this organization's build credentials") {
		t.Fatalf("err = %v: the message must send the user to reconnect GitHub", err)
	}
	assertNoThunderCalls(t, th)
}

func TestRequirePublisherForBuild_RowPresentPassesWithoutThunder(t *testing.T) {
	t.Parallel()
	th := &fakeThunder{}
	rows := newMemOrgSecretRepo()
	rows.rows[memOrgSecretKey("acme", OrgSecretPublisherClient)] = OrgSecretRef{Secret: OrgSecretPublisherClient, Name: "acme-ae-publisher-client-1a2b"}
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, th, PlatformIDPConfig{}).WithOrgSecretRefs(rows)

	if err := svc.RequirePublisherForBuild(context.Background(), "acme"); err != nil {
		t.Fatalf("a recorded ae-publisher-client row passes the gate: %v", err)
	}
	assertNoThunderCalls(t, th)
}

// Another org's row does not open this org's build.
func TestRequirePublisherForBuild_AnotherOrgsRowDoesNotCount(t *testing.T) {
	t.Parallel()
	rows := newMemOrgSecretRepo()
	rows.rows[memOrgSecretKey("other", OrgSecretPublisherClient)] = OrgSecretRef{Secret: OrgSecretPublisherClient, Name: "other-ae-publisher-client-1a2b"}
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, &fakeThunder{}, PlatformIDPConfig{}).WithOrgSecretRefs(rows)

	if err := svc.RequirePublisherForBuild(context.Background(), "acme"); !errors.Is(err, delivery.ErrPublisherCredentialsMissing) {
		t.Fatalf("err = %v, want ErrPublisherCredentialsMissing", err)
	}
}

// A failed read is not a missing row: it is returned as is, so the build
// answers a server error rather than telling the user to reconnect GitHub.
func TestRequirePublisherForBuild_ReadErrorIsNotMissing(t *testing.T) {
	t.Parallel()
	th := &fakeThunder{}
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, th, PlatformIDPConfig{}).WithOrgSecretRefs(failingOrgSecretRows{})

	err := svc.RequirePublisherForBuild(context.Background(), "acme")
	if err == nil || errors.Is(err, delivery.ErrPublisherCredentialsMissing) || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want the read failure, not a missing row", err)
	}
	assertNoThunderCalls(t, th)
}

// No row reader wired fails closed: there is no other source to fall back to.
func TestRequirePublisherForBuild_NoReaderFailsClosed(t *testing.T) {
	t.Parallel()
	th := &fakeThunder{}
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, th, PlatformIDPConfig{})

	if err := svc.RequirePublisherForBuild(context.Background(), "acme"); err == nil {
		t.Fatal("an unwired row reader must fail the build")
	}
	assertNoThunderCalls(t, th)
}

func TestRequirePublisherForBuild_EmptyOrgID(t *testing.T) {
	t.Parallel()
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, &fakeThunder{}, PlatformIDPConfig{}).
		WithOrgSecretRefs(newMemOrgSecretRepo())
	if err := svc.RequirePublisherForBuild(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "orgID") {
		t.Fatalf("err = %v, want an orgID error", err)
	}
}
