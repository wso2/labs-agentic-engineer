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

package identity

import (
	"context"
	"errors"
	"testing"
)

// The catalog is what a design agent reads before naming a role, so the field
// that matters most is `platformCreated`: it is the difference between a role the
// build may give a test user and one it must leave alone.
//
// It is read PER ORG now: the catalog belongs to that org's environment
// directory, so `catalogOrg` and the scope it resolves to travel together.

const catalogOrg = "org-acme"

var catalogScope = Scope{OrgID: catalogOrg, Environment: testEnvironment}

func TestCatalogMarksOnlyTheRolesThePlatformCreated(t *testing.T) {
	store := newFakeStore()
	store.putRole(catalogScope, IdPRole{Name: "Support Agent", ThunderGroupID: "grp-support"})
	dir := newFakeDirectory()
	dir.seedGroup("Support Agent", "usr-1", "usr-2")
	// On the directory with no row of ours — somebody made it by hand.
	dir.seedGroup("Administrators", "usr-admin")

	entries, err := NewCatalogService(newFakeTargets(dir), store).List(context.Background(), catalogOrg)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want the whole directory: %+v", len(entries), entries)
	}
	// Name-ordered, so the assertions can be positional.
	if entries[0].Name != "Administrators" || entries[0].PlatformCreated {
		t.Errorf("a hand-made group must read platformCreated=false: %+v", entries[0])
	}
	if entries[1].Name != "Support Agent" || !entries[1].PlatformCreated {
		t.Errorf("a platform-created role must read platformCreated=true: %+v", entries[1])
	}
	if entries[1].MemberCount != 2 {
		t.Errorf("memberCount = %d, want 2", entries[1].MemberCount)
	}
}

// The catalog is the row set the design-time `list_groups` tool renders, so the
// three fields a design decision turns on are pinned together: whether the
// platform created the group, how many people are in it today, and how many
// projects already bind a role to it.
//
// `projects` is the cross-project one: it counts DISTINCT projects that assign
// a role to the group, so a group two projects lean on reads 2 however many
// roles each of them binds to it, and a group nobody has bound reads 0.
func TestCatalogRowFields(t *testing.T) {
	store := newFakeStore()
	store.putRole(catalogScope, IdPRole{Name: "Support Agent", ThunderGroupID: "grp-support"})
	store.putRole(catalogScope, IdPRole{Name: "Approver", ThunderGroupID: "grp-approver"})
	// Two projects lean on Finance, and one of them binds two of its roles to
	// it: the answer is 2 projects, not 3 bindings. `helpdesk` binds only
	// Support Agent, and nobody binds Approver or Administrators.
	store.putBinding(catalogScope, "expenses", "Approver", "Finance")
	store.putBinding(catalogScope, "expenses", "Auditor", "Finance")
	store.putBinding(catalogScope, "vendors", "Finance", "Finance")
	store.putBinding(catalogScope, "helpdesk", "Agent", "Support Agent")
	// The "assigned to nobody" marker a self-service role carries is not a
	// group, so it must not make an empty-named row countable.
	store.putBinding(catalogScope, "clinic", "Patient", "")
	dir := newFakeDirectory()
	dir.seedGroup("Support Agent", "usr-1", "usr-2")
	dir.seedGroup("Approver") // ours, nobody in it yet
	dir.seedGroup("Finance", "usr-3", "usr-4", "usr-5")
	dir.seedGroup("Administrators", "usr-admin")

	entries, err := NewCatalogService(newFakeTargets(dir), store).List(context.Background(), catalogOrg)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := make(map[string]CatalogEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}

	cases := []struct {
		name            string
		platformCreated bool
		memberCount     int
		projects        int
	}{
		{name: "Support Agent", platformCreated: true, memberCount: 2, projects: 1},
		{name: "Approver", platformCreated: true, memberCount: 0, projects: 0},
		{name: "Finance", platformCreated: false, memberCount: 3, projects: 2},
		{name: "Administrators", platformCreated: false, memberCount: 1, projects: 0},
	}
	if len(entries) != len(cases) {
		t.Fatalf("entries = %d, want the whole directory (%d): %+v", len(entries), len(cases), entries)
	}
	for _, tc := range cases {
		got, ok := byName[tc.name]
		if !ok {
			t.Errorf("%q is missing from the catalog", tc.name)
			continue
		}
		if got.PlatformCreated != tc.platformCreated {
			t.Errorf("%q platformCreated = %v, want %v", tc.name, got.PlatformCreated, tc.platformCreated)
		}
		if got.MemberCount != tc.memberCount {
			t.Errorf("%q memberCount = %d, want %d", tc.name, got.MemberCount, tc.memberCount)
		}
		if got.Projects != tc.projects {
			t.Errorf("%q projects = %d, want %d (DISTINCT projects binding a role to the group)",
				tc.name, got.Projects, tc.projects)
		}
	}
}

// A role name differing only in case is the SAME role, and the ownership mark
// has to agree — otherwise a design spelling it `support agent` would be told
// the platform did not create a role it did.
func TestCatalogMatchesOwnershipCaseInsensitively(t *testing.T) {
	store := newFakeStore()
	store.putRole(catalogScope, IdPRole{Name: "support agent", ThunderGroupID: "grp-support"})
	dir := newFakeDirectory()
	dir.seedGroup("SUPPORT AGENT", "usr-1")

	entries, err := NewCatalogService(newFakeTargets(dir), store).List(context.Background(), catalogOrg)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || !entries[0].PlatformCreated {
		t.Fatalf("ownership must match across case: %+v", entries)
	}
}

// A member count that cannot be read must not cost the caller the ROW. The
// count is a nicety; the name and the ownership mark are what a design decision
// turns on, so losing the whole catalog over a failed count would trade the
// important answer for the unimportant one.
func TestCatalogSurvivesAFailedMemberCount(t *testing.T) {
	store := newFakeStore()
	store.putRole(catalogScope, IdPRole{Name: "Support Agent", ThunderGroupID: "grp-support"})
	dir := newFakeDirectory()
	dir.seedGroup("Support Agent", "usr-1")
	dir.failOn = map[string]error{"GroupMembers": errors.New("thunder said no")}

	entries, err := NewCatalogService(newFakeTargets(dir), store).List(context.Background(), catalogOrg)
	if err != nil {
		t.Fatalf("a failed member count must not fail the catalog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want the row anyway", entries)
	}
	if entries[0].Name != "Support Agent" || !entries[0].PlatformCreated {
		t.Errorf("the fields that matter were lost: %+v", entries[0])
	}
	if entries[0].MemberCount != 0 {
		t.Errorf("memberCount = %d, want 0 when it could not be read", entries[0].MemberCount)
	}
}

// The project count is a nicety too, and it is now ONE query for the whole
// listing rather than one per group. Losing it must cost the counts and nothing
// else — the same bargain the member count strikes, for the same reason.
func TestCatalogSurvivesAFailedProjectCount(t *testing.T) {
	store := newFakeStore()
	store.putRole(catalogScope, IdPRole{Name: "Support Agent", ThunderGroupID: "grp-support"})
	store.failOn = map[string]error{"CountProjectsBindingGroups": errors.New("the database said no")}
	dir := newFakeDirectory()
	dir.seedGroup("Support Agent", "usr-1")

	entries, err := NewCatalogService(newFakeTargets(dir), store).List(context.Background(), catalogOrg)
	if err != nil {
		t.Fatalf("a failed project count must not fail the catalog: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "Support Agent" {
		t.Fatalf("entries = %+v, want the row anyway", entries)
	}
	if entries[0].Projects != 0 {
		t.Errorf("projects = %d, want 0 when it could not be read", entries[0].Projects)
	}
	if !entries[0].PlatformCreated {
		t.Errorf("the ownership mark was lost with the count: %+v", entries[0])
	}
}

// The count is joined to the directory's groups WITHOUT CASE. A binding row
// carries the name the design authored and the directory answers with its own
// spelling, so a case-sensitive join would report a reused group as free — the
// one number a design agent uses to decide whether reusing it is a decision
// about people who already hold roles.
func TestCatalogCountsProjectsAcrossCase(t *testing.T) {
	store := newFakeStore()
	store.putBinding(catalogScope, "proj-one", "Approver", "finance")
	store.putBinding(catalogScope, "proj-two", "Auditor", "FINANCE")
	dir := newFakeDirectory()
	dir.seedGroup("Finance", "usr-1")

	entries, err := NewCatalogService(newFakeTargets(dir), store).List(context.Background(), catalogOrg)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Projects != 2 {
		t.Fatalf("entries = %+v, want Finance bound by 2 projects", entries)
	}
}

// A directory that cannot be listed at all IS an error: there is no partial
// answer to give, and an empty catalog would read as "no roles exist", which
// would send a design agent off to mint duplicates of every role there is.
func TestCatalogFailsWhenTheDirectoryCannotBeListed(t *testing.T) {
	dir := newFakeDirectory()
	dir.failOn = map[string]error{"ListGroups": errors.New("thunder is down")}

	if _, err := NewCatalogService(newFakeTargets(dir), newFakeStore()).List(context.Background(), catalogOrg); err == nil {
		t.Fatal("List succeeded with an unreachable directory — an empty catalog reads as 'no roles exist'")
	}
}

func TestCatalogEnabledNeedsBothCollaborators(t *testing.T) {
	store := newFakeStore()
	targets := newFakeTargets(newFakeDirectory())
	if (*CatalogService)(nil).Enabled() {
		t.Error("a nil service is not enabled")
	}
	if NewCatalogService(nil, store).Enabled() {
		t.Error("no resolver means no directory to read")
	}
	if NewCatalogService(targets, nil).Enabled() {
		t.Error("no store means ownership cannot be computed")
	}
	if !NewCatalogService(targets, store).Enabled() {
		t.Error("both wired should be enabled")
	}
}

// An environment with no identity provider bound to it is an ERROR here, for
// the same reason an unreachable directory is: an empty catalog reads as "no
// roles exist", and a design agent would mint a duplicate of every role that
// environment already has.
func TestCatalogFailsWhenTheEnvironmentHasNoIdentityProvider(t *testing.T) {
	targets := newFakeTargets(newFakeDirectory())
	targets.err = errors.New("environment \"default\" of \"org-acme\" has no Thunder binding")

	if _, err := NewCatalogService(targets, newFakeStore()).List(context.Background(), catalogOrg); err == nil {
		t.Fatal("List succeeded for an environment with no identity provider")
	}
}
