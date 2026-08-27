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

func TestCatalogMarksOnlyTheRolesThePlatformCreated(t *testing.T) {
	store := newFakeStore()
	store.roles["support agent"] = IdPRole{Name: "Support Agent", ThunderGroupID: "grp-support"}
	dir := newFakeDirectory()
	dir.seedGroup("Support Agent", "usr-1", "usr-2")
	// On the directory with no row of ours — somebody made it by hand.
	dir.seedGroup("Administrators", "usr-admin")

	entries, err := NewCatalogService(dir, store).List(context.Background())
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

// A role name differing only in case is the SAME role, and the ownership mark
// has to agree — otherwise a design spelling it `support agent` would be told
// the platform did not create a role it did.
func TestCatalogMatchesOwnershipCaseInsensitively(t *testing.T) {
	store := newFakeStore()
	store.roles["support agent"] = IdPRole{Name: "support agent", ThunderGroupID: "grp-support"}
	dir := newFakeDirectory()
	dir.seedGroup("SUPPORT AGENT", "usr-1")

	entries, err := NewCatalogService(dir, store).List(context.Background())
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
	store.roles["support agent"] = IdPRole{Name: "Support Agent", ThunderGroupID: "grp-support"}
	dir := newFakeDirectory()
	dir.seedGroup("Support Agent", "usr-1")
	dir.failOn = map[string]error{"GroupMembers": errors.New("thunder said no")}

	entries, err := NewCatalogService(dir, store).List(context.Background())
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

// A directory that cannot be listed at all IS an error: there is no partial
// answer to give, and an empty catalog would read as "no roles exist", which
// would send a design agent off to mint duplicates of every role there is.
func TestCatalogFailsWhenTheDirectoryCannotBeListed(t *testing.T) {
	dir := newFakeDirectory()
	dir.failOn = map[string]error{"ListGroups": errors.New("thunder is down")}

	if _, err := NewCatalogService(dir, newFakeStore()).List(context.Background()); err == nil {
		t.Fatal("List succeeded with an unreachable directory — an empty catalog reads as 'no roles exist'")
	}
}

func TestCatalogEnabledNeedsBothCollaborators(t *testing.T) {
	store := newFakeStore()
	dir := newFakeDirectory()
	if (*CatalogService)(nil).Enabled() {
		t.Error("a nil service is not enabled")
	}
	if NewCatalogService(nil, store).Enabled() {
		t.Error("no directory means no catalog to read")
	}
	if NewCatalogService(dir, nil).Enabled() {
		t.Error("no store means ownership cannot be computed")
	}
	if !NewCatalogService(dir, store).Enabled() {
		t.Error("both wired should be enabled")
	}
}
