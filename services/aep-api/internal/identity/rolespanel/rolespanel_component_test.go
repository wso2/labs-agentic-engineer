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

// COMPONENT tier: the console's Security panel through the REAL production
// handler chain — faked auth → contract validation → the deny-by-default tenant
// gate in ENFORCE → the strict handler — with only the identity store and the
// identity provider faked.
//
// The tier is chosen for what it can prove that a unit test cannot: the org this
// domain fences on comes from the VERIFIED TOKEN and from nowhere else (there is
// no {orgHandle} anywhere in the contract), so "project A cannot rotate project
// B's shared account" is only a real assertion when the org actually arrives the
// way production delivers it.
//
// External test package: the harness imports edge, which imports this domain —
// an in-package test file would be an import cycle.
package rolespanel_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/identity"
	identityhttpapi "github.com/wso2/aep/aep-api/internal/identity/httpapi"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
)

// newPanel assembles the real handler chain over the two fakes.
func newPanel(t *testing.T, dir identity.Directory, store identity.Store) *componenttest.Harness {
	t.Helper()
	handlers, err := identityhttpapi.New(identity.Deps{
		Panel: identity.NewPanelService(dir, store),
	})
	if err != nil {
		t.Fatalf("assemble identity domain: %v", err)
	}
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{Identity: handlers}})
}

// decodeView reads a 200 ProjectRolesView off the wire.
func decodeView(t *testing.T, body string) gen.ProjectRolesView {
	t.Helper()
	var v gen.ProjectRolesView
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("decode view: %v\n%s", err, body)
	}
	return v
}

// ── the read ────────────────────────────────────────────────────────────────

// The panel shows the WHOLE role catalog (roles are shared, so the question the
// console asks is "which existing role does this design reuse") and only THIS
// project's test users.
func TestPanel_ReadJoinsSharedCatalogWithProjectTestUsers(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withRole("Support Agent").
		withOwnedUser("support-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "support-bot", "Support Agent")
	dir := newFakeDirectory().
		withGroup("Support Agent", "Handles tickets", "usr-support-bot").
		// Administrators is on the directory but has NO platform record: the
		// panel must show it and mark it not-platform-created.
		withGroup("Administrators", "Made by hand").
		withAccount("support-bot")

	h := newPanel(t, dir, store)
	resp := h.AsOrg("acme").Get("/api/v1/projects/helpdesk/roles")
	if resp.Code != 200 {
		t.Fatalf("read: got %d body=%s", resp.Code, resp.Body.String())
	}
	view := decodeView(t, resp.Body.String())

	if !view.DirectoryAvailable {
		t.Errorf("directoryAvailable = false with a healthy directory")
	}
	if len(view.Roles) != 2 {
		t.Fatalf("roles = %d, want the whole catalog (2): %+v", len(view.Roles), view.Roles)
	}
	if view.Roles[0].Name != "Administrators" || view.Roles[0].PlatformCreated {
		t.Errorf("a hand-made group must read platformCreated=false: %+v", view.Roles[0])
	}
	if view.Roles[1].Name != "Support Agent" || !view.Roles[1].PlatformCreated {
		t.Errorf("a platform-created role must read platformCreated=true: %+v", view.Roles[1])
	}
	if view.Roles[1].MemberCount != 1 || view.Roles[1].Description != "Handles tickets" {
		t.Errorf("live directory facts not projected: %+v", view.Roles[1])
	}

	if len(view.TestUsers) != 1 {
		t.Fatalf("testUsers = %d, want 1: %+v", len(view.TestUsers), view.TestUsers)
	}
	u := view.TestUsers[0]
	if u.Username != "support-bot" || u.RoleName != "Support Agent" {
		t.Errorf("test user not projected: %+v", u)
	}
	if !u.Exists || !u.Owned {
		t.Errorf("a present, platform-owned account must read exists+owned: %+v", u)
	}
	if len(u.ReferencingProjects) != 1 || u.ReferencingProjects[0] != "helpdesk" || u.ReferencingCount != 1 {
		t.Errorf("referencing columns wrong: %+v", u)
	}
}

// A directory that cannot be reached degrades: directoryAvailable=false, the
// live half empty, the STORE-derived half still populated — so the console can
// say "unknown" instead of rendering absence as "does not exist".
func TestPanel_DirectoryUnavailableDegradesTheRead(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("support-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "support-bot", "Support Agent").
		withRef("acme", "billing", "support-bot", "Support Agent")
	dir := newFakeDirectory()
	dir.err = errors.New("thunder is down")

	h := newPanel(t, dir, store)
	resp := h.AsOrg("acme").Get("/api/v1/projects/helpdesk/roles")
	if resp.Code != 200 {
		t.Fatalf("a directory outage must NOT fail the read: got %d body=%s", resp.Code, resp.Body.String())
	}
	view := decodeView(t, resp.Body.String())

	if view.DirectoryAvailable {
		t.Errorf("directoryAvailable must be false when the identity provider errors")
	}
	if len(view.Roles) != 0 {
		t.Errorf("roles must be empty when the catalog could not be read: %+v", view.Roles)
	}
	if len(view.TestUsers) != 1 {
		t.Fatalf("store-derived test users must survive the outage: %+v", view.TestUsers)
	}
	u := view.TestUsers[0]
	if u.Username != "support-bot" || u.RoleName != "Support Agent" {
		t.Errorf("store-derived fields lost: %+v", u)
	}
	if !u.Owned {
		t.Errorf("ownership is a STORE fact and must survive the outage: %+v", u)
	}
	if u.Exists {
		t.Errorf("exists must be false (meaningless) when the directory is unavailable: %+v", u)
	}
	if u.ReferencingCount != 2 {
		t.Errorf("referencingCount is a store fact and must survive the outage: %+v", u)
	}
}

// ── the org+project fence ───────────────────────────────────────────────────

// The fence that makes a SHARED account safe: project A may not act on an
// account only project B references. Anything else and one project could rotate
// the password out from under another's validation runs.
func TestPanel_ProjectFenceRefusesAnotherProjectsAccount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func(*componenttest.Req) int
	}{
		{"reveal", func(r *componenttest.Req) int {
			return r.Post("/api/v1/projects/helpdesk/roles/test-users/billing-bot/reveal", "").Code
		}},
		{"rotate", func(r *componenttest.Req) int {
			return r.Post("/api/v1/projects/helpdesk/roles/test-users/billing-bot/rotate", "").Code
		}},
		{"delete", func(r *componenttest.Req) int {
			return r.Delete("/api/v1/projects/helpdesk/roles/test-users/billing-bot").Code
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore().
				// The account exists and the platform OWNS it — the only thing
				// missing is a reference from the calling project.
				withOwnedUser("billing-bot", "Billing Clerk", "Aep1!secret").
				withRef("acme", "billing", "billing-bot", "Billing Clerk").
				withRef("acme", "helpdesk", "support-bot", "Support Agent")
			dir := newFakeDirectory().withAccount("billing-bot")

			h := newPanel(t, dir, store)
			if code := tc.call(h.AsOrg("acme")); code != 404 {
				t.Fatalf("%s across the project fence: got %d, want 404", tc.name, code)
			}
			if _, rotated := dir.passwordsSet["usr-billing-bot"]; rotated {
				t.Errorf("%s across the fence still wrote to the directory", tc.name)
			}
			if len(dir.deleted) != 0 {
				t.Errorf("%s across the fence still deleted a directory account", tc.name)
			}
			if got := store.passwords["billing-bot"]; got != "Aep1!secret" {
				t.Errorf("the other project's sealed password changed: %q", got)
			}
		})
	}
}

// Same fence, the other axis: another ORG's project referencing the same shared
// account licenses nothing here. The org comes from the verified token, so this
// is the cross-tenant case the panel has to be safe against.
func TestPanel_OrgFenceRefusesAnotherOrgsReference(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("shared-bot", "Support Agent", "Aep1!secret").
		withRef("globex", "helpdesk", "shared-bot", "Support Agent")
	dir := newFakeDirectory().withAccount("shared-bot")

	h := newPanel(t, dir, store)
	// Same project NAME, different org. Only globex references the account.
	resp := h.AsOrg("acme").Post("/api/v1/projects/helpdesk/roles/test-users/shared-bot/rotate", "")
	if resp.Code != 404 {
		t.Fatalf("rotate across the ORG fence: got %d, want 404 body=%s", resp.Code, resp.Body.String())
	}
	if _, rotated := dir.passwordsSet["usr-shared-bot"]; rotated {
		t.Errorf("a cross-org rotate reached the directory")
	}
}

// ── the ownership fence ─────────────────────────────────────────────────────

// No `test_users` row means the platform did not create the account — the same
// rule the ensure refuses on. A design naming a real person must not hand a
// console button their login, so every mutation is refused even though the
// project genuinely references the username.
func TestPanel_UnownedAccountRefusesEveryMutation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func(*componenttest.Req) int
	}{
		{"reveal", func(r *componenttest.Req) int {
			return r.Post("/api/v1/projects/helpdesk/roles/test-users/jsmith/reveal", "").Code
		}},
		{"rotate", func(r *componenttest.Req) int {
			return r.Post("/api/v1/projects/helpdesk/roles/test-users/jsmith/rotate", "").Code
		}},
		{"delete", func(r *componenttest.Req) int {
			return r.Delete("/api/v1/projects/helpdesk/roles/test-users/jsmith").Code
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Referenced by THIS project, present on the directory — and with no
			// `test_users` row, which is the whole point.
			store := newFakeStore().withRef("acme", "helpdesk", "jsmith", "Support Agent")
			dir := newFakeDirectory().withAccount("jsmith")

			h := newPanel(t, dir, store)
			if code := tc.call(h.AsOrg("acme")); code != 404 {
				t.Fatalf("%s of an unowned account: got %d, want 404", tc.name, code)
			}
			if _, rotated := dir.passwordsSet["usr-jsmith"]; rotated {
				t.Errorf("%s reset an unowned account's password", tc.name)
			}
			if len(dir.deleted) != 0 {
				t.Errorf("%s deleted an unowned account", tc.name)
			}
			if _, still := dir.accounts["jsmith"]; !still {
				t.Errorf("%s removed an unowned account from the directory", tc.name)
			}
		})
	}
}

// The panel's read reports the same ownership answer, so the console can grey
// the actions out instead of offering a button that 404s.
func TestPanel_UnownedAccountReadsOwnedFalse(t *testing.T) {
	t.Parallel()
	store := newFakeStore().withRef("acme", "helpdesk", "jsmith", "Support Agent")
	dir := newFakeDirectory().withAccount("jsmith")

	h := newPanel(t, dir, store)
	view := decodeView(t, h.AsOrg("acme").Get("/api/v1/projects/helpdesk/roles").Body.String())
	if len(view.TestUsers) != 1 {
		t.Fatalf("testUsers = %+v", view.TestUsers)
	}
	if view.TestUsers[0].Owned {
		t.Errorf("an account with no test_users row must read owned=false: %+v", view.TestUsers[0])
	}
	if !view.TestUsers[0].Exists {
		t.Errorf("it is still PRESENT on the directory: %+v", view.TestUsers[0])
	}
}

// ── reveal ──────────────────────────────────────────────────────────────────

// Reveal serves the sealed password, and does it over POST so the credential
// never lands in a URL, a history entry, or an access log.
func TestPanel_RevealServesTheSealedPassword(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("support-bot", "Support Agent", "Aep1!sealed").
		withRef("acme", "helpdesk", "support-bot", "Support Agent")
	h := newPanel(t, newFakeDirectory().withAccount("support-bot"), store)

	resp := h.AsOrg("acme").Post("/api/v1/projects/helpdesk/roles/test-users/support-bot/reveal", "")
	if resp.Code != 200 {
		t.Fatalf("reveal: got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.TestUserPassword
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if got.Username != "support-bot" || got.Password != "Aep1!sealed" {
		t.Errorf("reveal = %+v", got)
	}

	// The same credential must NOT be reachable over GET — that is the whole
	// reason the operation is a POST.
	if code := h.AsOrg("acme").Get("/api/v1/projects/helpdesk/roles/test-users/support-bot/reveal").Code; code == 200 {
		t.Errorf("the password is served over GET; it must be POST-only")
	}
}

// ── rotate ──────────────────────────────────────────────────────────────────

// Rotate writes the directory AND re-seals the store, and the two agree.
func TestPanel_RotateWritesTheDirectoryAndReseals(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("support-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "support-bot", "Support Agent")
	dir := newFakeDirectory().withAccount("support-bot")
	h := newPanel(t, dir, store)

	resp := h.AsOrg("acme").Post("/api/v1/projects/helpdesk/roles/test-users/support-bot/rotate", "")
	if resp.Code != 200 {
		t.Fatalf("rotate: got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.TestUserPassword
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if got.Password == "" || got.Password == "Aep1!old" {
		t.Fatalf("rotate returned the old (or no) password: %+v", got)
	}
	if got.RotatedAt == nil {
		t.Errorf("rotate must stamp rotatedAt: %+v", got)
	}
	if store.passwords["support-bot"] != got.Password {
		t.Errorf("the sealed password is %q, the response says %q — they must agree",
			store.passwords["support-bot"], got.Password)
	}
	if dir.passwordsSet["usr-support-bot"] != got.Password {
		t.Errorf("the directory was written %q, the response says %q",
			dir.passwordsSet["usr-support-bot"], got.Password)
	}
	// A rotate the directory never saw would leave the platform serving a
	// password no sign-in accepts, so the directory write is the load-bearing
	// half and is asserted separately from the seal.
	if len(dir.passwordsSet) != 1 {
		t.Errorf("directory writes = %d, want exactly 1", len(dir.passwordsSet))
	}
}

// The half-applied rotate: the directory took the new password and the seal
// failed. That is not swallowed — the caller is told the password changed but
// was not recorded, and to rotate again.
func TestPanel_RotateReportsAChangeItCouldNotRecord(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("support-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "support-bot", "Support Agent")
	store.setPasswordErr = errors.New("column cipher unavailable")
	dir := newFakeDirectory().withAccount("support-bot")
	h := newPanel(t, dir, store)

	resp := h.AsOrg("acme").Post("/api/v1/projects/helpdesk/roles/test-users/support-bot/rotate", "")
	if resp.Code != 500 {
		t.Fatalf("half-applied rotate: got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if !strings.Contains(e.Message, "NOT recorded") || !strings.Contains(e.Message, "rotate again") {
		t.Errorf("the caller must be told the password changed but was not recorded, and to rotate "+
			"again; got %q", e.Message)
	}
	// The directory DID change — that is exactly why the message matters.
	if dir.passwordsSet["usr-support-bot"] == "" {
		t.Errorf("the test does not reach the hazard it claims to: the directory was never written")
	}
	if store.passwords["support-bot"] != "Aep1!old" {
		t.Errorf("the seal must be unchanged after a failed store write")
	}
}

// ── delete ──────────────────────────────────────────────────────────────────

// Delete removes the ACCOUNT and leaves the ROLE standing. Roles are shared and
// outlive the accounts in them: dropping one because a test login went away
// would take it from every other project naming it.
func TestPanel_DeleteRemovesTheAccountNotTheRole(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withRole("Support Agent").
		withOwnedUser("support-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "support-bot", "Support Agent")
	dir := newFakeDirectory().
		withGroup("Support Agent", "Handles tickets", "usr-support-bot").
		withAccount("support-bot")
	h := newPanel(t, dir, store)

	resp := h.AsOrg("acme").Delete("/api/v1/projects/helpdesk/roles/test-users/support-bot")
	if resp.Code != 200 {
		t.Fatalf("delete: got %d body=%s", resp.Code, resp.Body.String())
	}
	if _, still := dir.accounts["support-bot"]; still {
		t.Errorf("the directory account survived the delete")
	}
	if len(dir.deleted) != 1 || dir.deleted[0] != "usr-support-bot" {
		t.Errorf("directory deletes = %v, want exactly the account", dir.deleted)
	}
	if _, still := store.testUsers["support-bot"]; still {
		t.Errorf("the platform's record of the account survived the delete")
	}
	if _, gone := dir.groups["support agent"]; !gone {
		t.Fatalf("THE ROLE WAS DELETED — roles are shared and must be left standing")
	}
	if _, gone := store.roles["support agent"]; !gone {
		t.Errorf("the platform's record of the role was dropped; only the account goes")
	}
}

// The account is shared, so a delete while other projects still reference it is
// reported rather than done silently.
func TestPanel_DeleteWarnsWhenOtherProjectsStillReference(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("shared-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "shared-bot", "Support Agent").
		withRef("acme", "billing", "shared-bot", "Support Agent").
		// Another ORG's project counts toward the bare total, and its name is
		// never disclosed.
		withRef("globex", "portal", "shared-bot", "Support Agent")
	dir := newFakeDirectory().withAccount("shared-bot")
	h := newPanel(t, dir, store)

	resp := h.AsOrg("acme").Delete("/api/v1/projects/helpdesk/roles/test-users/shared-bot")
	if resp.Code != 200 {
		t.Fatalf("delete: got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.StatusMsg
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if !strings.Contains(got.Status, "2 other project") {
		t.Errorf("the status must warn about the OTHER references (2 of them): %q", got.Status)
	}
	if strings.Contains(got.Status, "portal") || strings.Contains(got.Status, "billing") {
		t.Errorf("the warning must be a bare count, never project names: %q", got.Status)
	}
}

// ── the unwired surface ─────────────────────────────────────────────────────

// A stack with no identity store leaves the routes present but unwired: 503,
// like every other nil-tolerant slice, never a panic and never a 404.
func TestPanel_UnwiredIs503(t *testing.T) {
	t.Parallel()
	h := componenttest.New(t, componenttest.Options{})
	for _, path := range []string{
		"/api/v1/projects/helpdesk/roles",
	} {
		if code := h.AsOrg("acme").Get(path).Code; code != 503 {
			t.Errorf("GET %s unwired: got %d, want 503", path, code)
		}
	}
	if code := h.AsOrg("acme").Post("/api/v1/projects/helpdesk/roles/test-users/x/reveal", "").Code; code != 503 {
		t.Errorf("reveal unwired: got %d, want 503", code)
	}
	if code := h.AsOrg("acme").Post("/api/v1/projects/helpdesk/roles/test-users/x/rotate", "").Code; code != 503 {
		t.Errorf("rotate unwired: got %d, want 503", code)
	}
	if code := h.AsOrg("acme").Delete("/api/v1/projects/helpdesk/roles/test-users/x").Code; code != 503 {
		t.Errorf("delete unwired: got %d, want 503", code)
	}
}

// The deny-by-default tenant gate applies here like everywhere: no token, no
// panel. This is the tier's ENFORCE proof for the domain.
func TestPanel_NoClaimsIs401(t *testing.T) {
	t.Parallel()
	store := newFakeStore().
		withOwnedUser("support-bot", "Support Agent", "Aep1!old").
		withRef("acme", "helpdesk", "support-bot", "Support Agent")
	h := newPanel(t, newFakeDirectory().withAccount("support-bot"), store)

	if code := h.NoAuth().Get("/api/v1/projects/helpdesk/roles").Code; code != 401 {
		t.Errorf("unauthenticated read: got %d, want 401", code)
	}
	if code := h.NoAuth().Post("/api/v1/projects/helpdesk/roles/test-users/support-bot/reveal", "").Code; code != 401 {
		t.Errorf("unauthenticated reveal: got %d, want 401", code)
	}
}
