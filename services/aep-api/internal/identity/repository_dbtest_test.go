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

package identity_test

// DB tier for the identity store — against a real migrated Postgres (dbtest;
// skipped under -short).
//
// Three things here can only be told the truth by a real database, and each is
// a property the in-memory fake in ensure_test.go deliberately does not model:
// the password column is genuinely encrypted and genuinely undecryptable under
// a different key; the org fence on test_user_refs is in the SQL and not in a
// caller; and UpsertRole's on-conflict clause really does leave provenance
// alone.

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/identity"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

const (
	orgA     = "org-a"
	orgB     = "org-b"
	projectA = "proj-a"
	projectB = "proj-b"
)

// newKey mints a random AES-256 key, so two ciphers in one test are genuinely
// different and no test can pass on a shared constant.
func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func newCipher(t *testing.T, key []byte) *secrets.ColumnCipher {
	t.Helper()
	c, err := secrets.NewColumnCipher(key)
	if err != nil {
		t.Fatalf("new column cipher: %v", err)
	}
	return c
}

// newStore hands back a store over a pristine migrated database, plus the DB
// handle for the raw-column assertions and the key, so a second store can be
// opened over the same data with a different key.
func newStore(t *testing.T) (identity.Store, *gorm.DB, []byte) {
	t.Helper()
	db := dbtest.New(t)
	key := newKey(t)
	return identity.NewStore(db, newCipher(t, key)), db, key
}

// seedUser writes one account through the store.
func seedUser(t *testing.T, ctx context.Context, s identity.Store, username, role, password string) {
	t.Helper()
	err := s.UpsertTestUser(ctx, identity.TestUser{
		Username: username, ThunderUserID: "usr-" + username, RoleName: role,
		Email: username + "@test-users.invalid",
	}, password)
	if err != nil {
		t.Fatalf("UpsertTestUser(%q): %v", username, err)
	}
}

// ---- the sealed password column -------------------------------------------

// The password is stored encrypted and comes back as plaintext. Thunder never
// returns a password, so this column is the only copy the platform has — and
// it must not be readable by anything holding the database alone.
func TestStorePasswordRoundTripsThroughTheSealedColumn(t *testing.T) {
	t.Parallel()
	s, db, _ := newStore(t)
	ctx := context.Background()
	const plaintext = "Aep1!nR7xk2QpZ4vLmT8yWb3d"

	seedUser(t, ctx, s, "test-viewer", "Viewer", plaintext)

	var stored string
	if err := db.Raw(`SELECT password_sealed FROM test_users WHERE username = ?`, "test-viewer").
		Scan(&stored).Error; err != nil {
		t.Fatalf("read column: %v", err)
	}
	if stored == "" {
		t.Fatalf("password_sealed is empty — nothing was stored")
	}
	if stored == plaintext {
		t.Fatalf("password_sealed holds the plaintext")
	}
	if strings.Contains(stored, plaintext) {
		t.Fatalf("password_sealed contains the plaintext: %q", stored)
	}

	got, err := s.RevealTestUserPassword(ctx, "test-viewer")
	if err != nil {
		t.Fatalf("RevealTestUserPassword: %v", err)
	}
	if got != plaintext {
		t.Fatalf("revealed %q, want the plaintext", got)
	}

	// The row itself never carries the sealed value off the store: json:"-" keeps
	// it off every wire shape, and GetTestUser is what the wire shapes are built
	// from.
	row, err := s.GetTestUser(ctx, "test-viewer")
	if err != nil || row == nil {
		t.Fatalf("GetTestUser = %v, %v", row, err)
	}
	if row.PasswordSealed == plaintext {
		t.Fatalf("GetTestUser handed back the plaintext password")
	}
}

// A reveal under a DIFFERENT credential-encryption-key must ERROR. This pins the
// deliberate choice of Open over OpenTolerant: OpenTolerant would answer a
// key rotation by handing the caller the base64 ciphertext AS the password,
// which the validation runner would then type into a login form.
func TestStoreRevealFailsUnderADifferentKeyRatherThanReturningCiphertext(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	const plaintext = "Aep1!originalSecretValue"

	written := identity.NewStore(db, newCipher(t, newKey(t)))
	seedUser(t, ctx, written, "test-viewer", "Viewer", plaintext)

	var sealed string
	if err := db.Raw(`SELECT password_sealed FROM test_users WHERE username = ?`, "test-viewer").
		Scan(&sealed).Error; err != nil {
		t.Fatalf("read column: %v", err)
	}

	rotated := identity.NewStore(db, newCipher(t, newKey(t)))
	got, err := rotated.RevealTestUserPassword(ctx, "test-viewer")
	if err == nil {
		t.Fatalf("reveal under a different key returned %q with no error", got)
	}
	if got != "" {
		t.Fatalf("reveal under a different key returned %q, want the empty string", got)
	}
	if got == sealed {
		t.Fatalf("reveal handed back the ciphertext as the password")
	}
}

// An account with no sealed password is a distinguishable outcome, not an empty
// string a caller could mistake for a password.
func TestStoreRevealReportsNoPassword(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	if _, err := s.RevealTestUserPassword(ctx, "nobody"); !errors.Is(err, identity.ErrNoPassword) {
		t.Fatalf("reveal for an unknown account = %v, want ErrNoPassword", err)
	}

	seedUser(t, ctx, s, "test-viewer", "Viewer", "")
	if _, err := s.RevealTestUserPassword(ctx, "test-viewer"); !errors.Is(err, identity.ErrNoPassword) {
		t.Fatalf("reveal for an account with no sealed password = %v, want ErrNoPassword", err)
	}
}

// A rotated password replaces the sealed one and stamps rotated_at, so the
// console can say when the credential a human is holding stopped working.
func TestStoreSetTestUserPasswordRotatesAndStamps(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()
	seedUser(t, ctx, s, "test-viewer", "Viewer", "Aep1!first")

	if err := s.SetTestUserPassword(ctx, "test-viewer", "Aep1!second"); err != nil {
		t.Fatalf("SetTestUserPassword: %v", err)
	}
	got, err := s.RevealTestUserPassword(ctx, "test-viewer")
	if err != nil {
		t.Fatalf("RevealTestUserPassword: %v", err)
	}
	if got != "Aep1!second" {
		t.Fatalf("revealed %q, want the rotated password", got)
	}
	row, err := s.GetTestUser(ctx, "test-viewer")
	if err != nil || row == nil {
		t.Fatalf("GetTestUser = %v, %v", row, err)
	}
	if row.RotatedAt == nil {
		t.Fatalf("rotated_at was not stamped")
	}
	// Rotating an account that does not exist is an error, not a silent no-op:
	// the caller believes it has issued a new credential.
	if err := s.SetTestUserPassword(ctx, "nobody", "Aep1!x"); err == nil {
		t.Fatalf("rotating an unknown account succeeded")
	}
}

// UpdateTestUserFacts moves the two metadata columns and leaves the sealed
// password byte-identical. That is the whole point of it: the alternative —
// reveal, then write the row back — decrypts a credential for no reason and
// fails outright for a row whose sealed password is absent.
func TestStoreUpdateTestUserFactsLeavesTheSealedPasswordAlone(t *testing.T) {
	t.Parallel()
	s, db, _ := newStore(t)
	ctx := context.Background()
	const plaintext = "Aep1!untouchedByAFactsUpdate"
	seedUser(t, ctx, s, "test-viewer", "Viewer", plaintext)

	var before string
	if err := db.Raw(`SELECT password_sealed FROM test_users WHERE username = ?`, "test-viewer").
		Scan(&before).Error; err != nil {
		t.Fatalf("read column: %v", err)
	}

	if err := s.UpdateTestUserFacts(ctx, "test-viewer", "usr-recreated", "Auditor"); err != nil {
		t.Fatalf("UpdateTestUserFacts: %v", err)
	}

	row, err := s.GetTestUser(ctx, "test-viewer")
	if err != nil || row == nil {
		t.Fatalf("GetTestUser = %v, %v", row, err)
	}
	if row.ThunderUserID != "usr-recreated" || row.RoleName != "Auditor" {
		t.Fatalf("facts = %q/%q, want usr-recreated/Auditor", row.ThunderUserID, row.RoleName)
	}
	var after string
	if err := db.Raw(`SELECT password_sealed FROM test_users WHERE username = ?`, "test-viewer").
		Scan(&after).Error; err != nil {
		t.Fatalf("read column: %v", err)
	}
	if after != before {
		t.Fatalf("password_sealed was rewritten by a facts-only update")
	}
	got, err := s.RevealTestUserPassword(ctx, "test-viewer")
	if err != nil {
		t.Fatalf("RevealTestUserPassword: %v", err)
	}
	if got != plaintext {
		t.Fatalf("revealed %q after a facts update, want the original password", got)
	}
	// It must also work for a row that has no sealed password at all — the case
	// the reveal-then-reseal path failed the whole build on.
	seedUser(t, ctx, s, "test-legacy", "Viewer", "")
	if err := s.UpdateTestUserFacts(ctx, "test-legacy", "usr-2", "Auditor"); err != nil {
		t.Fatalf("UpdateTestUserFacts on an account with no sealed password: %v", err)
	}
}

// Updating an account that does not exist is an error, not a silent no-op: the
// caller believes the directory and the platform's record now agree.
func TestStoreUpdateTestUserFactsErrorsOnAnUnknownAccount(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)

	err := s.UpdateTestUserFacts(context.Background(), "nobody", "usr-1", "Viewer")
	if err == nil {
		t.Fatalf("UpdateTestUserFacts on an unknown account succeeded")
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Fatalf("error %q does not name the account", err)
	}
}

// ---- absence is not an error ------------------------------------------------

// GetRole and GetTestUser answer (nil, nil) for an absent row, never an error.
// The nil IS the ownership answer the ensure branches on — "the platform did
// not create this" — so turning it into an error would fail every build that
// declares a role for the first time.
func TestStoreGetsReturnNilForAnAbsentRow(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	role, err := s.GetRole(ctx, "Administrators")
	if err != nil {
		t.Fatalf("GetRole for an absent role errored: %v", err)
	}
	if role != nil {
		t.Fatalf("GetRole = %+v, want nil", role)
	}

	user, err := s.GetTestUser(ctx, "jsmith")
	if err != nil {
		t.Fatalf("GetTestUser for an absent account errored: %v", err)
	}
	if user != nil {
		t.Fatalf("GetTestUser = %+v, want nil", user)
	}
}

// ---- roles ------------------------------------------------------------------

// UpsertRole refreshes the cached Thunder group id but must NOT take the role
// over: created_by_* records who first declared it, and a second project
// adopting a shared role does not become its owner.
func TestStoreUpsertRoleKeepsTheOriginalProvenance(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	first := identity.IdPRole{
		Name: "Viewer", ThunderGroupID: "grp-1", Description: "first description",
		CreatedByOrg: orgA, CreatedByProject: projectA,
	}
	if err := s.UpsertRole(ctx, first); err != nil {
		t.Fatalf("first UpsertRole: %v", err)
	}

	second := identity.IdPRole{
		Name: "Viewer", ThunderGroupID: "grp-2", Description: "second description",
		CreatedByOrg: orgB, CreatedByProject: projectB,
	}
	if err := s.UpsertRole(ctx, second); err != nil {
		t.Fatalf("second UpsertRole: %v", err)
	}

	got, err := s.GetRole(ctx, "Viewer")
	if err != nil || got == nil {
		t.Fatalf("GetRole = %v, %v", got, err)
	}
	if got.ThunderGroupID != "grp-2" {
		t.Fatalf("thunder_group_id = %q, want the refreshed grp-2", got.ThunderGroupID)
	}
	if got.CreatedByOrg != orgA || got.CreatedByProject != projectA {
		t.Fatalf("provenance = %s/%s, want the original %s/%s — the second upsert took the role over",
			got.CreatedByOrg, got.CreatedByProject, orgA, projectA)
	}
	// The name is the identity, so a second upsert is one row, not two.
	rows, err := s.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("roles = %d, want one", len(rows))
	}
}

// Role lookup is case-insensitive: two names differing only in case are one
// role, so a design writing `viewer` must find the `Viewer` the platform made
// rather than creating a near-duplicate beside it.
func TestStoreGetRoleIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()
	if err := s.UpsertRole(ctx, identity.IdPRole{Name: "Compliance Admin", ThunderGroupID: "grp-1"}); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}

	for _, name := range []string{"Compliance Admin", "compliance admin", "COMPLIANCE ADMIN"} {
		got, err := s.GetRole(ctx, name)
		if err != nil || got == nil {
			t.Fatalf("GetRole(%q) = %v, %v", name, got, err)
		}
		if got.Name != "Compliance Admin" {
			t.Fatalf("GetRole(%q).Name = %q", name, got.Name)
		}
	}
	// A name that differs only in case is the SAME role, so a lookup under any
	// casing finds the one row. There is deliberately no DeleteRole: nothing here
	// ever removes a role, and the panel does not offer it — see ADR-0022.
	if got, err := s.GetRole(ctx, "no such role"); err != nil || got != nil {
		t.Fatalf("GetRole(absent) = %+v, %v; want nil, nil", got, err)
	}
}

// ---- project references -----------------------------------------------------

// ReplaceProjectRefs is a true replace: the previous build's references for the
// same project are gone, so a role dropped from a design stops being referenced.
func TestStoreReplaceProjectRefsReplaces(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	err := s.ReplaceProjectRefs(ctx, orgA, projectA, []identity.TestUserRef{
		{Username: "test-viewer", RoleName: "Viewer", ColdStart: true},
		{Username: "test-auditor", RoleName: "Auditor"},
	})
	if err != nil {
		t.Fatalf("first ReplaceProjectRefs: %v", err)
	}

	// v2 drops Auditor.
	err = s.ReplaceProjectRefs(ctx, orgA, projectA, []identity.TestUserRef{
		{Username: "test-viewer", RoleName: "Viewer", ColdStart: true},
	})
	if err != nil {
		t.Fatalf("second ReplaceProjectRefs: %v", err)
	}

	rows, err := s.ListProjectRefs(ctx, orgA, projectA)
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(rows) != 1 || rows[0].Username != "test-viewer" {
		t.Fatalf("refs = %+v, want only test-viewer", rows)
	}
	if rows[0].OrgID != orgA || rows[0].ProjectID != projectA {
		t.Fatalf("ref = %+v, want it stamped with the caller's org/project", rows[0])
	}
	if !rows[0].ColdStart {
		t.Fatalf("cold_start was not persisted")
	}
	if rows[0].UpdatedAt.IsZero() {
		t.Fatalf("updated_at was not stamped")
	}

	// An empty set clears the project's references without failing.
	if err := s.ReplaceProjectRefs(ctx, orgA, projectA, nil); err != nil {
		t.Fatalf("empty ReplaceProjectRefs: %v", err)
	}
	rows, err = s.ListProjectRefs(ctx, orgA, projectA)
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("refs = %+v, want none", rows)
	}
}

// A replace touches only the calling project. Another project's references to
// the same shared account survive it.
func TestStoreReplaceProjectRefsLeavesOtherProjectsAlone(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	mustReplace(t, ctx, s, orgA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, orgA, projectB, "test-viewer", "Viewer")

	if err := s.ReplaceProjectRefs(ctx, orgA, projectA, nil); err != nil {
		t.Fatalf("ReplaceProjectRefs: %v", err)
	}
	rows, err := s.ListProjectRefs(ctx, orgA, projectB)
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the other project's refs = %+v, want its one ref intact", rows)
	}
}

// SECURITY: ProjectsReferencing is ORG-FENCED. The account is shared at the
// IdP's scope, but a project NAME is one org's data — the console panel listing
// another org's project names would be a cross-tenant disclosure the shared
// directory does not license.
func TestStoreProjectsReferencingIsOrgFenced(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	mustReplace(t, ctx, s, orgA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, orgB, "proj-secret", "test-viewer", "Viewer")

	rows, err := s.ProjectsReferencing(ctx, orgA, "test-viewer")
	if err != nil {
		t.Fatalf("ProjectsReferencing: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("org A sees %d references, want only its own: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.OrgID != orgA {
			t.Fatalf("org A was shown org %q's reference to %+v", r.OrgID, r)
		}
		if r.ProjectID == "proj-secret" {
			t.Fatalf("org A was shown another org's project name")
		}
	}

	// ListProjectRefs carries the same fence: one org cannot read another org's
	// project by guessing its id.
	leaked, err := s.ListProjectRefs(ctx, orgA, "proj-secret")
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("org A read org B's project refs: %+v", leaked)
	}
}

// CountReferencing counts across EVERY org, deliberately. It is a bare number
// and never names, and it is what makes "others may still be using this"
// truthful before a delete.
func TestStoreCountReferencingCountsAcrossOrgs(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	mustReplace(t, ctx, s, orgA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, orgB, projectB, "test-viewer", "Viewer")

	n, err := s.CountReferencing(ctx, "test-viewer")
	if err != nil {
		t.Fatalf("CountReferencing: %v", err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want both orgs' projects", n)
	}
	// An org-fenced read of the same account still sees one, so the count is
	// genuinely wider than what any one org may be shown.
	rows, err := s.ProjectsReferencing(ctx, orgA, "test-viewer")
	if err != nil {
		t.Fatalf("ProjectsReferencing: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("org A sees %d, want 1", len(rows))
	}

	zero, err := s.CountReferencing(ctx, "nobody")
	if err != nil {
		t.Fatalf("CountReferencing: %v", err)
	}
	if zero != 0 {
		t.Fatalf("count for an unreferenced account = %d", zero)
	}
}

// Forgetting an account forgets every reference to it in the same transaction,
// so no project is left pointing at an account that is gone.
func TestStoreDeleteTestUserRemovesItsReferences(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()
	seedUser(t, ctx, s, "test-viewer", "Viewer", "Aep1!viewer")
	mustReplace(t, ctx, s, orgA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, orgB, projectB, "test-viewer", "Viewer")

	if err := s.DeleteTestUser(ctx, "test-viewer"); err != nil {
		t.Fatalf("DeleteTestUser: %v", err)
	}
	row, err := s.GetTestUser(ctx, "test-viewer")
	if err != nil {
		t.Fatalf("GetTestUser: %v", err)
	}
	if row != nil {
		t.Fatalf("GetTestUser = %+v after delete", row)
	}
	n, err := s.CountReferencing(ctx, "test-viewer")
	if err != nil {
		t.Fatalf("CountReferencing: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d references survive the account they point at", n)
	}
}

// mustReplace writes one project reference, for the cases that only need the
// ref to exist.
func mustReplace(t *testing.T, ctx context.Context, s identity.Store, orgID, projectID, username, role string) {
	t.Helper()
	err := s.ReplaceProjectRefs(ctx, orgID, projectID, []identity.TestUserRef{
		{Username: username, RoleName: role},
	})
	if err != nil {
		t.Fatalf("ReplaceProjectRefs(%s/%s): %v", orgID, projectID, err)
	}
}
