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
// Four things here can only be told the truth by a real database, and each is
// a property the in-memory fake in ensure_test.go deliberately does not model:
// the password column is genuinely encrypted and genuinely undecryptable under
// a different key; the (org, environment) key is in the SQL and not in a
// caller, so two environments' rows genuinely cannot answer each other's reads;
// UpsertRole's on-conflict clause really does leave provenance alone; and the
// composite primary key really does admit the same role name twice, once per
// environment.

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
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

// The scopes under test. devA and devB are two orgs' default environments;
// stagingA is the SAME org's other environment — a different identity provider,
// and the case that proves the key is a key and not a filter.
var (
	devA     = identity.Scope{OrgID: orgA, Environment: "default"}
	devB     = identity.Scope{OrgID: orgB, Environment: "default"}
	stagingA = identity.Scope{OrgID: orgA, Environment: "staging"}
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

// seedUser writes one account through the store, on one scope's directory.
func seedUser(t *testing.T, ctx context.Context, s identity.Store, scope identity.Scope, username, role, password string) {
	t.Helper()
	err := s.UpsertTestUser(ctx, identity.TestUser{
		OrgID: scope.OrgID, Environment: scope.Environment,
		Username: username, ThunderUserID: "usr-" + username, RoleName: role,
		Email: username + "@test-users.invalid",
	}, password)
	if err != nil {
		t.Fatalf("UpsertTestUser(%q on %s): %v", username, scope, err)
	}
}

// sealedColumn reads one account's raw sealed password straight out of the
// table, for the assertions that must not go through the cipher.
func sealedColumn(t *testing.T, db *gorm.DB, scope identity.Scope, username string) string {
	t.Helper()
	var stored string
	err := db.Raw(`SELECT password_sealed FROM test_users
	                WHERE org_id = ? AND environment = ? AND username = ?`,
		scope.OrgID, scope.Environment, username).Scan(&stored).Error
	if err != nil {
		t.Fatalf("read column: %v", err)
	}
	return stored
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

	seedUser(t, ctx, s, devA, "test-viewer", "Viewer", plaintext)

	stored := sealedColumn(t, db, devA, "test-viewer")
	if stored == "" {
		t.Fatalf("password_sealed is empty — nothing was stored")
	}
	if stored == plaintext {
		t.Fatalf("password_sealed holds the plaintext")
	}
	if strings.Contains(stored, plaintext) {
		t.Fatalf("password_sealed contains the plaintext: %q", stored)
	}

	got, err := s.RevealTestUserPassword(ctx, devA, "test-viewer")
	if err != nil {
		t.Fatalf("RevealTestUserPassword: %v", err)
	}
	if got != plaintext {
		t.Fatalf("revealed %q, want the plaintext", got)
	}

	// The row itself never carries the sealed value off the store: json:"-" keeps
	// it off every wire shape, and GetTestUser is what the wire shapes are built
	// from.
	row, err := s.GetTestUser(ctx, devA, "test-viewer")
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
	seedUser(t, ctx, written, devA, "test-viewer", "Viewer", plaintext)

	sealed := sealedColumn(t, db, devA, "test-viewer")

	rotated := identity.NewStore(db, newCipher(t, newKey(t)))
	got, err := rotated.RevealTestUserPassword(ctx, devA, "test-viewer")
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

	if _, err := s.RevealTestUserPassword(ctx, devA, "nobody"); !errors.Is(err, identity.ErrNoPassword) {
		t.Fatalf("reveal for an unknown account = %v, want ErrNoPassword", err)
	}

	seedUser(t, ctx, s, devA, "test-viewer", "Viewer", "")
	if _, err := s.RevealTestUserPassword(ctx, devA, "test-viewer"); !errors.Is(err, identity.ErrNoPassword) {
		t.Fatalf("reveal for an account with no sealed password = %v, want ErrNoPassword", err)
	}
}

// A rotated password replaces the sealed one and stamps rotated_at, so the
// console can say when the credential a human is holding stopped working.
func TestStoreSetTestUserPasswordRotatesAndStamps(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()
	seedUser(t, ctx, s, devA, "test-viewer", "Viewer", "Aep1!first")

	if err := s.SetTestUserPassword(ctx, devA, "test-viewer", "Aep1!second"); err != nil {
		t.Fatalf("SetTestUserPassword: %v", err)
	}
	got, err := s.RevealTestUserPassword(ctx, devA, "test-viewer")
	if err != nil {
		t.Fatalf("RevealTestUserPassword: %v", err)
	}
	if got != "Aep1!second" {
		t.Fatalf("revealed %q, want the rotated password", got)
	}
	row, err := s.GetTestUser(ctx, devA, "test-viewer")
	if err != nil || row == nil {
		t.Fatalf("GetTestUser = %v, %v", row, err)
	}
	if row.RotatedAt == nil {
		t.Fatalf("rotated_at was not stamped")
	}
	// Rotating an account that does not exist is an error, not a silent no-op:
	// the caller believes it has issued a new credential.
	if err := s.SetTestUserPassword(ctx, devA, "nobody", "Aep1!x"); err == nil {
		t.Fatalf("rotating an unknown account succeeded")
	}
	// The same username on ANOTHER environment is another account, so rotating
	// it here must not reach across.
	if err := s.SetTestUserPassword(ctx, stagingA, "test-viewer", "Aep1!x"); err == nil {
		t.Fatalf("rotating an account on a different environment succeeded")
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
	seedUser(t, ctx, s, devA, "test-viewer", "Viewer", plaintext)

	before := sealedColumn(t, db, devA, "test-viewer")

	if err := s.UpdateTestUserFacts(ctx, devA, "test-viewer", "usr-recreated", "Auditor"); err != nil {
		t.Fatalf("UpdateTestUserFacts: %v", err)
	}

	row, err := s.GetTestUser(ctx, devA, "test-viewer")
	if err != nil || row == nil {
		t.Fatalf("GetTestUser = %v, %v", row, err)
	}
	if row.ThunderUserID != "usr-recreated" || row.RoleName != "Auditor" {
		t.Fatalf("facts = %q/%q, want usr-recreated/Auditor", row.ThunderUserID, row.RoleName)
	}
	if after := sealedColumn(t, db, devA, "test-viewer"); after != before {
		t.Fatalf("password_sealed was rewritten by a facts-only update")
	}
	got, err := s.RevealTestUserPassword(ctx, devA, "test-viewer")
	if err != nil {
		t.Fatalf("RevealTestUserPassword: %v", err)
	}
	if got != plaintext {
		t.Fatalf("revealed %q after a facts update, want the original password", got)
	}
	// It must also work for a row that has no sealed password at all — the case
	// the reveal-then-reseal path failed the whole build on.
	seedUser(t, ctx, s, devA, "test-legacy", "Viewer", "")
	if err := s.UpdateTestUserFacts(ctx, devA, "test-legacy", "usr-2", "Auditor"); err != nil {
		t.Fatalf("UpdateTestUserFacts on an account with no sealed password: %v", err)
	}
}

// Updating an account that does not exist is an error, not a silent no-op: the
// caller believes the directory and the platform's record now agree.
func TestStoreUpdateTestUserFactsErrorsOnAnUnknownAccount(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)

	err := s.UpdateTestUserFacts(context.Background(), devA, "nobody", "usr-1", "Viewer")
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

	role, err := s.GetRole(ctx, devA, "Administrators")
	if err != nil {
		t.Fatalf("GetRole for an absent role errored: %v", err)
	}
	if role != nil {
		t.Fatalf("GetRole = %+v, want nil", role)
	}

	user, err := s.GetTestUser(ctx, devA, "jsmith")
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
		OrgID: devA.OrgID, Environment: devA.Environment,
		Name: "Viewer", ThunderGroupID: "grp-1", Description: "first description",
		CreatedByOrg: orgA, CreatedByProject: projectA,
	}
	if err := s.UpsertRole(ctx, first); err != nil {
		t.Fatalf("first UpsertRole: %v", err)
	}

	second := identity.IdPRole{
		OrgID: devA.OrgID, Environment: devA.Environment,
		Name: "Viewer", ThunderGroupID: "grp-2", Description: "second description",
		CreatedByOrg: orgB, CreatedByProject: projectB,
	}
	if err := s.UpsertRole(ctx, second); err != nil {
		t.Fatalf("second UpsertRole: %v", err)
	}

	got, err := s.GetRole(ctx, devA, "Viewer")
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
	rows, err := s.ListRoles(ctx, devA)
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
	role := identity.IdPRole{
		OrgID: devA.OrgID, Environment: devA.Environment,
		Name: "Compliance Admin", ThunderGroupID: "grp-1",
	}
	if err := s.UpsertRole(ctx, role); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}

	for _, name := range []string{"Compliance Admin", "compliance admin", "COMPLIANCE ADMIN"} {
		got, err := s.GetRole(ctx, devA, name)
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
	if got, err := s.GetRole(ctx, devA, "no such role"); err != nil || got != nil {
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

	err := s.ReplaceProjectRefs(ctx, devA, projectA, []identity.TestUserRef{
		{Username: "test-viewer", RoleName: "Viewer", ColdStart: true},
		{Username: "test-auditor", RoleName: "Auditor"},
	})
	if err != nil {
		t.Fatalf("first ReplaceProjectRefs: %v", err)
	}

	// v2 drops Auditor.
	err = s.ReplaceProjectRefs(ctx, devA, projectA, []identity.TestUserRef{
		{Username: "test-viewer", RoleName: "Viewer", ColdStart: true},
	})
	if err != nil {
		t.Fatalf("second ReplaceProjectRefs: %v", err)
	}

	rows, err := s.ListProjectRefs(ctx, devA, projectA)
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(rows) != 1 || rows[0].Username != "test-viewer" {
		t.Fatalf("refs = %+v, want only test-viewer", rows)
	}
	if rows[0].OrgID != orgA || rows[0].Environment != devA.Environment || rows[0].ProjectID != projectA {
		t.Fatalf("ref = %+v, want it stamped with the caller's scope and project", rows[0])
	}
	if !rows[0].ColdStart {
		t.Fatalf("cold_start was not persisted")
	}
	if rows[0].UpdatedAt.IsZero() {
		t.Fatalf("updated_at was not stamped")
	}

	// An empty set clears the project's references without failing.
	if err := s.ReplaceProjectRefs(ctx, devA, projectA, nil); err != nil {
		t.Fatalf("empty ReplaceProjectRefs: %v", err)
	}
	rows, err = s.ListProjectRefs(ctx, devA, projectA)
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

	mustReplace(t, ctx, s, devA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, devA, projectB, "test-viewer", "Viewer")

	if err := s.ReplaceProjectRefs(ctx, devA, projectA, nil); err != nil {
		t.Fatalf("ReplaceProjectRefs: %v", err)
	}
	rows, err := s.ListProjectRefs(ctx, devA, projectB)
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the other project's refs = %+v, want its one ref intact", rows)
	}
}

// SECURITY: every reference read is fenced by the (org, environment) SCOPE.
// The account is shared, but only within one environment's identity provider —
// so another org's project names, and this org's OTHER environment's, are both
// out of reach. The count the panel warns with is len() of this same read now,
// which is only sound because the scope IS the disclosure boundary.
func TestStoreProjectsReferencingIsScopeFenced(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	mustReplace(t, ctx, s, devA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, devB, "proj-secret", "test-viewer", "Viewer")
	mustReplace(t, ctx, s, stagingA, "proj-staging", "test-viewer", "Viewer")

	rows, err := s.ProjectsReferencing(ctx, devA, "test-viewer")
	if err != nil {
		t.Fatalf("ProjectsReferencing: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s sees %d references, want only its own: %+v", devA, len(rows), rows)
	}
	for _, r := range rows {
		if r.OrgID != orgA || r.Environment != devA.Environment {
			t.Fatalf("%s was shown %s/%s's reference to %+v", devA, r.OrgID, r.Environment, r)
		}
		if r.ProjectID != projectA {
			t.Fatalf("%s was shown another scope's project name: %+v", devA, r)
		}
	}

	// ListProjectRefs carries the same fence: one org cannot read another org's
	// project by guessing its id, and one environment cannot read another's.
	leaked, err := s.ListProjectRefs(ctx, devA, "proj-secret")
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	if len(leaked) != 0 {
		t.Fatalf("%s read %s's project refs: %+v", devA, devB, leaked)
	}
	if crossEnv, err := s.ListProjectRefs(ctx, devA, "proj-staging"); err != nil || len(crossEnv) != 0 {
		t.Fatalf("%s read %s's project refs: %+v (%v)", devA, stagingA, crossEnv, err)
	}
}

// The composite key is a KEY, not a filter: the same role name and the same
// username exist independently on two environments, because they are two groups
// and two accounts on two directories that share nothing. Under the old
// single-column primary key the second write here was a conflict that silently
// overwrote the first environment's row.
func TestStoreSameNamesCoexistAcrossEnvironments(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	for _, scope := range []identity.Scope{devA, stagingA} {
		err := s.UpsertRole(ctx, identity.IdPRole{
			OrgID: scope.OrgID, Environment: scope.Environment,
			Name: "Viewer", ThunderGroupID: "grp-" + scope.Environment,
			CreatedByOrg: scope.OrgID, CreatedByProject: projectA,
		})
		if err != nil {
			t.Fatalf("UpsertRole on %s: %v", scope, err)
		}
		seedUser(t, ctx, s, scope, "test-viewer", "Viewer", "pw-"+scope.Environment)
	}

	for _, scope := range []identity.Scope{devA, stagingA} {
		role, err := s.GetRole(ctx, scope, "Viewer")
		if err != nil || role == nil {
			t.Fatalf("GetRole on %s = %v, %v", scope, role, err)
		}
		if role.ThunderGroupID != "grp-"+scope.Environment {
			t.Fatalf("%s resolved to %q — one environment's row answered the other's read",
				scope, role.ThunderGroupID)
		}
		pw, err := s.RevealTestUserPassword(ctx, scope, "test-viewer")
		if err != nil {
			t.Fatalf("RevealTestUserPassword on %s: %v", scope, err)
		}
		if pw != "pw-"+scope.Environment {
			t.Fatalf("%s revealed %q — the wrong environment's credential", scope, pw)
		}
	}
}

// Forgetting an account forgets every reference to it in the same transaction,
// so no project is left pointing at an account that is gone.
func TestStoreDeleteTestUserRemovesItsReferences(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()
	seedUser(t, ctx, s, devA, "test-viewer", "Viewer", "Aep1!viewer")
	mustReplace(t, ctx, s, devA, projectA, "test-viewer", "Viewer")
	mustReplace(t, ctx, s, devA, projectB, "test-viewer", "Viewer")
	// The same username on another environment is a different account, and the
	// delete must not reach it.
	seedUser(t, ctx, s, stagingA, "test-viewer", "Viewer", "Aep1!staging")
	mustReplace(t, ctx, s, stagingA, projectA, "test-viewer", "Viewer")

	if err := s.DeleteTestUser(ctx, devA, "test-viewer"); err != nil {
		t.Fatalf("DeleteTestUser: %v", err)
	}
	row, err := s.GetTestUser(ctx, devA, "test-viewer")
	if err != nil {
		t.Fatalf("GetTestUser: %v", err)
	}
	if row != nil {
		t.Fatalf("GetTestUser = %+v after delete", row)
	}
	rows, err := s.ProjectsReferencing(ctx, devA, "test-viewer")
	if err != nil {
		t.Fatalf("ProjectsReferencing: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("%d references survive the account they point at", len(rows))
	}
	survivor, err := s.GetTestUser(ctx, stagingA, "test-viewer")
	if err != nil || survivor == nil {
		t.Fatalf("the other environment's account was deleted too: %v, %v", survivor, err)
	}
}

// mustReplace writes one project reference, for the cases that only need the
// ref to exist.
func mustReplace(t *testing.T, ctx context.Context, s identity.Store, scope identity.Scope, projectID, username, role string) {
	t.Helper()
	err := s.ReplaceProjectRefs(ctx, scope, projectID, []identity.TestUserRef{
		{Username: username, RoleName: role},
	})
	if err != nil {
		t.Fatalf("ReplaceProjectRefs(%s/%s): %v", scope, projectID, err)
	}
}

// ---- project-owned directory objects ----------------------------------------
//
// These four tables' rows are the OTHER half of this package's record, and the
// half the shared rows above deliberately are not: a resource server and a
// project's own roles are created by exactly one project, so they may be
// converged and they may be deleted. Only a real database can say whether the
// project half of the key is genuinely in the SQL, whether the converge really
// leaves an unchanged row alone, and whether the count behind "reused by 2
// projects" is DISTINCT and scope-fenced.

// The project is part of the KEY, not a filter. Two orgs running a project of
// the same name have two resource servers on two directories, and neither read
// may answer the other's.
func TestStoreResourceServerIsScopeFenced(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	for _, scope := range []identity.Scope{devA, devB, stagingA} {
		err := s.UpsertResourceServer(ctx, identity.IdPResourceServer{
			OrgID: scope.OrgID, Environment: scope.Environment, ProjectID: projectA,
			Identifier:  "https://aep.wso2.com/orgs/" + scope.OrgID + "/projects/" + projectA,
			DirectoryID: "rs-" + scope.String(),
		})
		if err != nil {
			t.Fatalf("UpsertResourceServer on %s: %v", scope, err)
		}
	}

	for _, scope := range []identity.Scope{devA, devB, stagingA} {
		got, err := s.GetResourceServer(ctx, scope, projectA)
		if err != nil || got == nil {
			t.Fatalf("GetResourceServer on %s = %v, %v", scope, got, err)
		}
		if got.DirectoryID != "rs-"+scope.String() {
			t.Fatalf("%s resolved to %q — another scope's row answered its read",
				scope, got.DirectoryID)
		}
	}

	// A project with no resource server is (nil, nil), the same ownership answer
	// GetRole gives: the directory may hold one, but the platform did not make it.
	got, err := s.GetResourceServer(ctx, devA, projectB)
	if err != nil {
		t.Fatalf("GetResourceServer for an absent project errored: %v", err)
	}
	if got != nil {
		t.Fatalf("GetResourceServer = %+v, want nil", got)
	}
}

// A rebuild of the same tag upserts the same row: one row, refreshed facts, and
// created_at untouched — the ensure must be re-runnable without looking like it
// recreated the resource server.
func TestStoreUpsertResourceServerIsIdempotent(t *testing.T) {
	t.Parallel()
	s, db, _ := newStore(t)
	ctx := context.Background()

	const identifier = "https://aep.wso2.com/orgs/org-a/projects/proj-a"
	first := identity.IdPResourceServer{
		OrgID: devA.OrgID, Environment: devA.Environment, ProjectID: projectA,
		Identifier: identifier, DirectoryID: "rs-1",
	}
	if err := s.UpsertResourceServer(ctx, first); err != nil {
		t.Fatalf("first UpsertResourceServer: %v", err)
	}
	created, err := s.GetResourceServer(ctx, devA, projectA)
	if err != nil || created == nil {
		t.Fatalf("GetResourceServer = %v, %v", created, err)
	}

	second := first
	second.DirectoryID = "rs-recreated"
	if err := s.UpsertResourceServer(ctx, second); err != nil {
		t.Fatalf("second UpsertResourceServer: %v", err)
	}

	got, err := s.GetResourceServer(ctx, devA, projectA)
	if err != nil || got == nil {
		t.Fatalf("GetResourceServer = %v, %v", got, err)
	}
	if got.DirectoryID != "rs-recreated" {
		t.Fatalf("directory_id = %q, want the refreshed rs-recreated", got.DirectoryID)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at moved on an upsert: %v -> %v", created.CreatedAt, got.CreatedAt)
	}
	var rows int64
	if err := db.Raw(`SELECT count(*) FROM idp_resource_servers`).Scan(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("idp_resource_servers holds %d rows, want one", rows)
	}
}

// The identifier is the token audience, and the directory treats it as unique,
// so the table does too: a second project claiming one project's identifier on
// the same directory is a conflict, not a silent duplicate. The SAME identifier
// on another environment is fine — it is another directory.
func TestStoreResourceServerIdentifierIsUniquePerDirectory(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()
	const identifier = "https://aep.wso2.com/orgs/org-a/projects/proj-a"

	err := s.UpsertResourceServer(ctx, identity.IdPResourceServer{
		OrgID: devA.OrgID, Environment: devA.Environment, ProjectID: projectA,
		Identifier: identifier, DirectoryID: "rs-1",
	})
	if err != nil {
		t.Fatalf("UpsertResourceServer: %v", err)
	}

	err = s.UpsertResourceServer(ctx, identity.IdPResourceServer{
		OrgID: devA.OrgID, Environment: devA.Environment, ProjectID: projectB,
		Identifier: identifier, DirectoryID: "rs-2",
	})
	if err == nil {
		t.Fatalf("a second project claimed %q on the same directory", identifier)
	}

	err = s.UpsertResourceServer(ctx, identity.IdPResourceServer{
		OrgID: stagingA.OrgID, Environment: stagingA.Environment, ProjectID: projectB,
		Identifier: identifier, DirectoryID: "rs-3",
	})
	if err != nil {
		t.Fatalf("the same identifier on another environment was refused: %v", err)
	}
}

// ReplaceRoleBindings converges: what the tag still names is KEPT (created_at
// and all), what it adds is written, and what it dropped is gone. The keep is
// the reason this is not a clear-and-insert — a rebuild of the same tag must not
// make every binding look new.
func TestStoreReplaceRoleBindingsConverges(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	err := s.ReplaceRoleBindings(ctx, devA, projectA, []identity.IdPRoleBinding{
		{Role: "Approver", GroupName: "Finance", DirectoryRoleID: "role-approver"},
		{Role: "Employee", GroupName: "Employees", DirectoryRoleID: "role-employee"},
	})
	if err != nil {
		t.Fatalf("first ReplaceRoleBindings: %v", err)
	}
	before, err := s.ListRoleBindings(ctx, devA, projectA)
	if err != nil || len(before) != 2 {
		t.Fatalf("ListRoleBindings = %+v, %v", before, err)
	}
	// Ordered by role then group, so the read is stable for a console table.
	if before[0].Role != "Approver" || before[1].Role != "Employee" {
		t.Fatalf("bindings are not role-ordered: %+v", before)
	}
	if before[0].OrgID != orgA || before[0].Environment != devA.Environment || before[0].ProjectID != projectA {
		t.Fatalf("binding = %+v, want it stamped with the caller's scope and project", before[0])
	}

	// v2: Approver keeps Finance and gains Auditors, Employee is dropped, and a
	// self-service role arrives with no group at all.
	err = s.ReplaceRoleBindings(ctx, devA, projectA, []identity.IdPRoleBinding{
		{Role: "Approver", GroupName: "Finance", DirectoryRoleID: "role-approver"},
		{Role: "Approver", GroupName: "Auditors", DirectoryRoleID: "role-approver"},
		{Role: "Member", GroupName: "", DirectoryRoleID: "role-member"},
	})
	if err != nil {
		t.Fatalf("second ReplaceRoleBindings: %v", err)
	}

	after, err := s.ListRoleBindings(ctx, devA, projectA)
	if err != nil {
		t.Fatalf("ListRoleBindings: %v", err)
	}
	var got []string
	for _, b := range after {
		got = append(got, b.Role+"->"+b.GroupName)
	}
	want := []string{"Approver->Auditors", "Approver->Finance", "Member->"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bindings = %v, want %v", got, want)
	}

	// The kept binding is the SAME row, not a recreated one.
	for _, b := range after {
		if b.Role == "Approver" && b.GroupName == "Finance" {
			if !b.CreatedAt.Equal(before[0].CreatedAt) {
				t.Fatalf("an unchanged binding was recreated: created_at %v -> %v",
					before[0].CreatedAt, b.CreatedAt)
			}
		}
	}

	// An empty set clears the project's bindings without failing — the shape a
	// design that dropped every role leaves behind.
	if err := s.ReplaceRoleBindings(ctx, devA, projectA, nil); err != nil {
		t.Fatalf("empty ReplaceRoleBindings: %v", err)
	}
	rows, err := s.ListRoleBindings(ctx, devA, projectA)
	if err != nil || len(rows) != 0 {
		t.Fatalf("bindings = %+v, %v; want none", rows, err)
	}
}

// A replace touches only the calling project, and only on its own scope: the
// project id alone names nothing, exactly as with the resource server.
func TestStoreReplaceRoleBindingsIsScopeAndProjectFenced(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	mustBind(t, ctx, s, devA, projectA, "Approver", "Finance", "role-a")
	mustBind(t, ctx, s, devA, projectB, "Approver", "Finance", "role-b")
	mustBind(t, ctx, s, devB, projectA, "Approver", "Finance", "role-other-org")
	mustBind(t, ctx, s, stagingA, projectA, "Approver", "Finance", "role-staging")

	if err := s.ReplaceRoleBindings(ctx, devA, projectA, nil); err != nil {
		t.Fatalf("ReplaceRoleBindings: %v", err)
	}

	for _, tc := range []struct {
		scope   identity.Scope
		project string
		roleID  string
	}{
		{devA, projectB, "role-b"},
		{devB, projectA, "role-other-org"},
		{stagingA, projectA, "role-staging"},
	} {
		rows, err := s.ListRoleBindings(ctx, tc.scope, tc.project)
		if err != nil {
			t.Fatalf("ListRoleBindings(%s/%s): %v", tc.scope, tc.project, err)
		}
		if len(rows) != 1 || rows[0].DirectoryRoleID != tc.roleID {
			t.Fatalf("%s/%s = %+v, want its one binding %q intact",
				tc.scope, tc.project, rows, tc.roleID)
		}
	}
}

// The `projects` number beside a group in the directory panel counts DISTINCT
// projects — a project binding one group to two roles is still one project —
// and it is fenced by the scope, because a group exists on exactly one
// environment's directory.
func TestStoreCountProjectsBindingGroup(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	// projectA binds Finance twice, through two roles. That is one project.
	err := s.ReplaceRoleBindings(ctx, devA, projectA, []identity.IdPRoleBinding{
		{Role: "Approver", GroupName: "Finance", DirectoryRoleID: "role-approver"},
		{Role: "Auditor", GroupName: "Finance", DirectoryRoleID: "role-auditor"},
		{Role: "Member", GroupName: "", DirectoryRoleID: "role-member"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoleBindings(%s): %v", projectA, err)
	}
	mustBind(t, ctx, s, devA, projectB, "Reviewer", "Finance", "role-reviewer")
	// Other scopes name the same group; neither may be counted here.
	mustBind(t, ctx, s, devB, "proj-secret", "Reviewer", "Finance", "role-other-org")
	mustBind(t, ctx, s, stagingA, "proj-staging", "Reviewer", "Finance", "role-staging")

	count, err := s.CountProjectsBindingGroup(ctx, devA, "Finance")
	if err != nil {
		t.Fatalf("CountProjectsBindingGroup: %v", err)
	}
	if count != 2 {
		t.Fatalf("Finance is bound by %d projects on %s, want 2", count, devA)
	}

	// A group nobody binds is zero, not an error.
	if count, err := s.CountProjectsBindingGroup(ctx, devA, "Nobody"); err != nil || count != 0 {
		t.Fatalf("CountProjectsBindingGroup(absent) = %d, %v; want 0, nil", count, err)
	}
	// The empty name is the "assigned to nobody" marker, not a group: counting it
	// would report every project holding a self-service role as a member of a
	// group that does not exist.
	if count, err := s.CountProjectsBindingGroup(ctx, devA, ""); err != nil || count != 0 {
		t.Fatalf("CountProjectsBindingGroup(\"\") = %d, %v; want 0, nil", count, err)
	}
}

// The name is compared WITHOUT CASE, like every other group comparison in this
// domain. The rows carry the DIRECTORY's spelling (the ensure normalises them to
// it) while the caller asks with whatever it holds — the console asks with the
// directory's name, an older row may carry the design's — so a case-sensitive
// compare would report a reused group as free.
func TestStoreCountProjectsBindingGroupIgnoresCase(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	mustBind(t, ctx, s, devA, projectA, "Approver", "Finance", "role-approver")
	mustBind(t, ctx, s, devA, projectB, "Reviewer", "finance", "role-reviewer")

	for _, spelling := range []string{"Finance", "finance", "FINANCE"} {
		count, err := s.CountProjectsBindingGroup(ctx, devA, spelling)
		if err != nil {
			t.Fatalf("CountProjectsBindingGroup(%q): %v", spelling, err)
		}
		if count != 2 {
			t.Fatalf("CountProjectsBindingGroup(%q) = %d, want 2", spelling, count)
		}
	}
}

// The grouped read is the same count for every group at once — what the catalog
// asks, because asking per group costs one round trip per group on a directory
// that can hold hundreds. Same scope fence, same case fold, same treatment of
// the "assigned to nobody" marker; the key is the LOWERCASED name.
func TestStoreCountProjectsBindingGroups(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	err := s.ReplaceRoleBindings(ctx, devA, projectA, []identity.IdPRoleBinding{
		{Role: "Approver", GroupName: "Finance", DirectoryRoleID: "role-approver"},
		{Role: "Auditor", GroupName: "Finance", DirectoryRoleID: "role-auditor"},
		{Role: "Employee", GroupName: "Employees", DirectoryRoleID: "role-employee"},
		{Role: "Member", GroupName: "", DirectoryRoleID: "role-member"},
	})
	if err != nil {
		t.Fatalf("ReplaceRoleBindings(%s): %v", projectA, err)
	}
	// A second project, spelling the same group differently.
	mustBind(t, ctx, s, devA, projectB, "Reviewer", "FINANCE", "role-reviewer")
	// Other scopes name the same group; neither may be counted here.
	mustBind(t, ctx, s, devB, "proj-secret", "Reviewer", "Finance", "role-other-org")
	mustBind(t, ctx, s, stagingA, "proj-staging", "Reviewer", "Finance", "role-staging")

	counts, err := s.CountProjectsBindingGroups(ctx, devA)
	if err != nil {
		t.Fatalf("CountProjectsBindingGroups: %v", err)
	}
	want := map[string]int{"finance": 2, "employees": 1}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("CountProjectsBindingGroups(%s) = %+v, want %+v", devA, counts, want)
	}
}

// Deleting a project forgets both of its project-owned records, leaves every
// other project's alone, and is idempotent — the cleanup is best-effort and
// re-runs, so a second pass over an already-clean project must not fail it.
func TestStoreDeleteProjectOwnedRows(t *testing.T) {
	t.Parallel()
	s, _, _ := newStore(t)
	ctx := context.Background()

	seed := func(scope identity.Scope, projectID string) {
		t.Helper()
		err := s.UpsertResourceServer(ctx, identity.IdPResourceServer{
			OrgID: scope.OrgID, Environment: scope.Environment, ProjectID: projectID,
			Identifier:  "https://aep.wso2.com/orgs/" + scope.OrgID + "/" + scope.Environment + "/projects/" + projectID,
			DirectoryID: "rs-" + projectID,
		})
		if err != nil {
			t.Fatalf("UpsertResourceServer(%s/%s): %v", scope, projectID, err)
		}
		mustBind(t, ctx, s, scope, projectID, "Approver", "Finance", "role-"+projectID)
	}
	seed(devA, projectA)
	seed(devA, projectB)
	seed(stagingA, projectA)

	if err := s.DeleteRoleBindings(ctx, devA, projectA); err != nil {
		t.Fatalf("DeleteRoleBindings: %v", err)
	}
	if err := s.DeleteResourceServer(ctx, devA, projectA); err != nil {
		t.Fatalf("DeleteResourceServer: %v", err)
	}

	if rows, err := s.ListRoleBindings(ctx, devA, projectA); err != nil || len(rows) != 0 {
		t.Fatalf("bindings = %+v, %v after delete; want none", rows, err)
	}
	if rs, err := s.GetResourceServer(ctx, devA, projectA); err != nil || rs != nil {
		t.Fatalf("GetResourceServer = %+v, %v after delete; want nil, nil", rs, err)
	}

	// Deleting again is success: the cleanup is best-effort and re-runs.
	if err := s.DeleteRoleBindings(ctx, devA, projectA); err != nil {
		t.Fatalf("second DeleteRoleBindings: %v", err)
	}
	if err := s.DeleteResourceServer(ctx, devA, projectA); err != nil {
		t.Fatalf("second DeleteResourceServer: %v", err)
	}

	// Every other project's rows stand — including the same project id on the
	// org's other environment, which is a different directory's objects.
	for _, tc := range []struct {
		scope   identity.Scope
		project string
	}{{devA, projectB}, {stagingA, projectA}} {
		if rs, err := s.GetResourceServer(ctx, tc.scope, tc.project); err != nil || rs == nil {
			t.Fatalf("%s/%s's resource server was deleted too: %v, %v", tc.scope, tc.project, rs, err)
		}
		if rows, err := s.ListRoleBindings(ctx, tc.scope, tc.project); err != nil || len(rows) != 1 {
			t.Fatalf("%s/%s's bindings = %+v, %v; want its one row intact",
				tc.scope, tc.project, rows, err)
		}
	}
}

// mustBind writes one role binding, for the cases that only need it to exist.
func mustBind(t *testing.T, ctx context.Context, s identity.Store, scope identity.Scope, projectID, role, group, roleID string) {
	t.Helper()
	err := s.ReplaceRoleBindings(ctx, scope, projectID, []identity.IdPRoleBinding{
		{Role: role, GroupName: group, DirectoryRoleID: roleID},
	})
	if err != nil {
		t.Fatalf("ReplaceRoleBindings(%s/%s): %v", scope, projectID, err)
	}
}
