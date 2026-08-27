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

// ensure.go — the build-time ensure. It reads the roles document at the tag
// being built and makes every role and test user it declares real on the
// Platform IdP.
//
// It runs with NO MODEL IN THE LOOP. A model authored roles.json and read the
// role catalog; below the version tag everything is deterministic — which is
// the single most important property of this design, because these calls mint
// credentials.
//
// Three passes, and the ORDER is load-bearing.
//
//  0. **Classify**, reading only. Each declared role is settled as one the
//     platform owns, one somebody else made, or one that does not exist yet.
//  1. **Accounts**, for the roles the platform may enrol into — and ONLY those.
//  2. **Roles**: create each absent one complete with its members, and add the
//     missing members to each owned one.
//
// Classification has to come first because of what pass 1 costs. Creating an
// account MINTS A CREDENTIAL: it seals a password, and it writes a reference row
// that the validation credential provider later serves as "the login for this
// role". Doing that for a role the platform will refuse to enrol into produces
// exactly the failure this whole design exists to close — validation signing in
// as an account that holds no role at all, and grading role-gated criteria
// against it. So a pre-existing role's accounts are not created, not sealed and
// not referenced; they are reported and skipped.
//
// Accounts still come before roles, because the IdP sets group membership only
// when a group is CREATED. Knowing the member ids up front lets a brand-new role
// be created complete, in one call, instead of created empty and then
// deleted-and-recreated to add its members — which would change the group's id
// on its very first build for no reason.
//
// Two rules carry all the safety, and both reduce to the same ownership marker
// — a row in this package's own tables:
//
//   - **The platform enrols members only into roles it created.** That is what
//     stops a design that reasonably reuses `Administrators` from getting a
//     platform-made test account into the group `setup-aep.sh` binds to
//     OpenChoreo's `admin` role. It is a rule, not a denylist, so every
//     hand-made group is protected without a list to maintain.
//   - **The platform modifies only accounts it owns.** A design naming an
//     existing username the platform has no row for is REFUSED, never adopted:
//     otherwise a design naming a real person would reset their password and
//     hand it to a validation runner.

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/rolesspec"
)

// EnsureService makes a project's declared roles and test users real.
type EnsureService struct {
	dir    Directory
	store  Store
	design DesignReader
}

// NewEnsureService builds the ensure. Every collaborator is required; a nil one
// is a wiring defect, and the composition root skips wiring the whole feature
// rather than passing a nil (see Enabled).
func NewEnsureService(dir Directory, store Store, design DesignReader) *EnsureService {
	return &EnsureService{dir: dir, store: store, design: design}
}

// Enabled reports whether the ensure can run. It is false when the Thunder
// admin client is not configured — a local stack with no IdP — and the caller
// then skips the roles gate entirely instead of failing every build.
func (s *EnsureService) Enabled() bool {
	return s != nil && s.dir != nil && s.store != nil && s.design != nil
}

// Result is what one ensure did, for the gate's closing comment and the logs.
type Result struct {
	// RolesCreated / RolesReused are the roles this run made and the ones it
	// found already on the directory.
	RolesCreated []string
	RolesReused  []string
	// RolesPreExisting are roles that exist on the directory but that the
	// platform did not create. It does not enrol members into these.
	RolesPreExisting []string
	UsersCreated     []string
	UsersReused      []string
	// UsersRefused are usernames the design named that already exist on the
	// directory as accounts the platform does not own.
	UsersRefused []string
	// UsersSkipped are accounts the design asked for whose ROLE the platform
	// does not own. They are deliberately not created: an account that can
	// never be enrolled is a standing credential for a login that holds
	// nothing, and serving it to validation would be worse than serving
	// nothing.
	UsersSkipped []string
	// Credentials are the logins for every account this project can sign in
	// as after this run — the ones created here AND the ones reused from an
	// earlier build. It is deliberately not "what changed": the validation
	// agent reads its logins from THIS version's provisioning ticket, so a
	// ticket listing only the new accounts would leave a rebuild's validation
	// unable to sign in as any role that already existed.
	//
	// This is the one field that carries a secret, and it exists to be
	// PUBLISHED (see rolesGateClosingComment). Summary() must never render it.
	Credentials []Credential
}

// Credential is one test account's login, as published to the provisioning
// ticket the validation agent reads.
//
// Password is empty when the seal could not be opened. That is reported rather
// than swallowed and rather than fatal: the account itself is fine and the
// build must not die over a publishing step, but the ticket has to say the row
// is unusable instead of printing a blank and reading as a password-less login.
type Credential struct {
	Username  string
	Password  string
	Role      string
	ColdStart bool
}

// Summary renders the result as the gate's closing comment: one line per
// outcome, only for outcomes that occurred.
func (r Result) Summary() string {
	var lines []string
	add := func(label string, names []string) {
		if len(names) > 0 {
			sort.Strings(names)
			lines = append(lines, fmt.Sprintf("- %s: %s", label, strings.Join(names, ", ")))
		}
	}
	add("Roles created", r.RolesCreated)
	add("Roles reused", r.RolesReused)
	add("Roles left alone (not created by the platform, so no members are enrolled)", r.RolesPreExisting)
	add("Test users created", r.UsersCreated)
	add("Test users reused", r.UsersReused)
	add("Test users refused (the username already belongs to an account the platform does not own)", r.UsersRefused)
	add("Test users not created (their role is not one the platform created, so nothing could be enrolled into it)", r.UsersSkipped)
	if len(lines) == 0 {
		return "Nothing to provision — the design declares no roles."
	}
	return strings.Join(lines, "\n")
}

// HasRefusals reports whether anything was refused. A refusal is not an error —
// the build continues — but it is the one outcome a human has to look at, so
// the gate comment leads with it and the caller can decide to surface it.
func (r Result) HasRefusals() bool { return len(r.UsersRefused) > 0 }

// EnsureForTag reads roles.json at tag and makes its contents real.
//
// A project whose design declares no roles document is not an error: it has no
// sign-in, and there is nothing to ensure. A roles.json that is present but
// malformed IS an error — it acquired a tag, so something upstream let a broken
// document through, and provisioning credentials from a document nobody can
// parse is the wrong kind of best effort.
// `declared` says whether the design carries a roles document at all, and it is
// reported SEPARATELY from the error on purpose. The caller mints a gate that
// holds dispatch when the ensure fails — but only for a project that actually
// declares roles. Folding the two together would make a transient git read
// failure hold the build of a project that has no sign-in at all.
func (s *EnsureService) EnsureForTag(ctx context.Context, orgID, projectID, tag string) (result Result, declared bool, err error) {
	raw, declared, err := s.readRolesAtTag(ctx, orgID, projectID, tag)
	if err != nil || !declared {
		return Result{}, declared, err
	}
	doc, err := rolesspec.Parse([]byte(raw))
	if err != nil {
		return Result{}, true, fmt.Errorf("%s at %s: %w", rolesspec.Path, tag, err)
	}
	result, err = s.ensure(ctx, orgID, projectID, rolesspec.Plan(doc))
	return result, true, err
}

// DeclaresRoles reports whether the design at tag carries a roles document at
// all. It exists so the caller can mint its gate BEFORE the work starts — the
// same shape every other provisioning gate has, where you watch a ticket open
// and then close, rather than one appearing already resolved.
//
// A read failure is returned as an error and must NOT be treated as "no roles":
// conflating the two is what let a build skip this feature entirely and say
// nothing louder than a warning.
func (s *EnsureService) DeclaresRoles(ctx context.Context, orgID, projectID, tag string) (bool, error) {
	_, declared, err := s.readRolesAtTag(ctx, orgID, projectID, tag)
	return declared, err
}

// readRolesAtTag returns the raw roles document at tag, and whether the design
// carries one. The bundle read is against the local bare mirror, so calling it
// twice in one build costs a tree read, not a fetch.
func (s *EnsureService) readRolesAtTag(ctx context.Context, orgID, projectID, tag string) (string, bool, error) {
	bundle, err := s.design.GetDesignAtTag(ctx, orgID, projectID, tag)
	if err != nil {
		return "", false, fmt.Errorf("read design at %s: %w", tag, err)
	}
	raw, ok := bundle[rolesspec.BundleKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	return raw, true, nil
}

// roleTarget is one declared role after classification.
type roleTarget struct {
	role rolesspec.Role
	// group is the live directory group, zero when the role is absent from it.
	group       DirectoryGroup
	onDirectory bool
	// recorded is the platform's own row for the role, nil when it has none.
	recorded *IdPRole
	// enrolable is the whole point of classifying: true when the platform may
	// put a member into this role — because it created it, or because it is
	// about to. False for a group somebody else made.
	enrolable bool
}

// ensure runs the three passes over a plan.
func (s *EnsureService) ensure(ctx context.Context, orgID, projectID string, plan rolesspec.EnsurePlan) (Result, error) {
	var result Result

	// ---- pass 0: classify, writing nothing --------------------------------
	targets := make([]roleTarget, 0, len(plan.Roles))
	enrolable := make(map[string]bool, len(plan.Roles))
	for _, role := range plan.Roles {
		target, err := s.classifyRole(ctx, role)
		if err != nil {
			return result, err
		}
		targets = append(targets, target)
		if target.enrolable {
			enrolable[strings.ToLower(role.Name)] = true
			continue
		}
		// Somebody else's group. Left entirely alone, and nothing is minted for
		// it — see the file header on why that includes its accounts.
		slog.InfoContext(ctx, "roles ensure: leaving a pre-existing directory group alone",
			"role", role.Name, "org", orgID, "project", projectID)
		result.RolesPreExisting = append(result.RolesPreExisting, role.Name)
	}

	// ---- pass 1: accounts, only for roles the platform may enrol into ------
	//
	// Refusal and skipping are both per account and neither stops the pass: one
	// design naming a real person's username, or reusing one hand-made group,
	// must not block the roles around it.
	membersByRole := map[string][]string{}
	var refs []TestUserRef
	for _, planned := range plan.Users {
		if !enrolable[strings.ToLower(planned.Role)] {
			result.UsersSkipped = append(result.UsersSkipped, planned.Username)
			continue
		}
		account, usable, err := s.ensureUser(ctx, planned, &result)
		if err != nil {
			return result, err
		}
		if !usable {
			continue
		}
		key := strings.ToLower(planned.Role)
		membersByRole[key] = append(membersByRole[key], account.ID)
		// A reference is the statement "this account is the login for this role
		// in this project", and the credential provider reads it as exactly
		// that. It is written only for an account that WILL be enrolled.
		refs = append(refs, TestUserRef{
			Username:  planned.Username,
			RoleName:  planned.Role,
			ColdStart: planned.ColdStart,
			Supplied:  planned.Supplied,
		})
	}

	// ---- pass 2: roles ----------------------------------------------------
	for _, target := range targets {
		if !target.enrolable {
			continue
		}
		if err := s.realiseRole(ctx, orgID, projectID, target,
			membersByRole[strings.ToLower(target.role.Name)], &result); err != nil {
			return result, err
		}
	}

	// The references are rewritten wholesale, so a role dropped from the design
	// stops being referenced by this project — while the directory object
	// itself stands, per the additive-only rule.
	if err := s.store.ReplaceProjectRefs(ctx, orgID, projectID, refs); err != nil {
		return result, err
	}

	result.Credentials = s.collectCredentials(ctx, orgID, projectID, refs)
	return result, nil
}

// collectCredentials opens the seal on every account this project can sign in
// as, so the gate can publish the logins on its ticket.
//
// It reads from `refs` — the accounts pass 1 settled — rather than from
// UsersCreated, because a rebuild creates nothing and its ticket still has to
// carry every login. The reveal runs for created and reused accounts through
// the SAME call, so there is no path on which a freshly generated password and
// a stored one are published from different sources.
//
// A reveal failure never fails the build. The account exists and is enrolled;
// only its publication is lost, and the row goes out with an empty password
// that the renderer calls out explicitly.
func (s *EnsureService) collectCredentials(ctx context.Context, orgID, projectID string, refs []TestUserRef) []Credential {
	out := make([]Credential, 0, len(refs))
	for _, ref := range refs {
		cred := Credential{Username: ref.Username, Role: ref.RoleName, ColdStart: ref.ColdStart}
		password, err := s.store.RevealTestUserPassword(ctx, ref.Username)
		if err != nil {
			slog.WarnContext(ctx, "roles ensure: could not open a test user's password to publish it",
				"org", orgID, "project", projectID, "username", ref.Username, "error", err)
		} else {
			cred.Password = password
		}
		out = append(out, cred)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// classifyRole settles what the platform may do with one role, reading only.
//
// Three outcomes, and the middle one is the safety property:
//
//   - recorded AND on the directory → the platform created it; enrolable.
//   - NOT recorded but ON the directory → somebody else made it. NOT enrolable.
//     `Administrators` is the case that matters: `setup-aep.sh` maps it to
//     OpenChoreo's `admin` role, so a design that reasonably reuses the name
//     must not get a platform-made account into it. This is a rule, not a
//     denylist — every hand-made group is protected by it, with no list to
//     maintain.
//   - absent from the directory → the platform is about to create it, whether or
//     not a stale row survived somebody deleting the group; enrolable.
func (s *EnsureService) classifyRole(ctx context.Context, role rolesspec.Role) (roleTarget, error) {
	recorded, err := s.store.GetRole(ctx, role.Name)
	if err != nil {
		return roleTarget{}, err
	}
	live, onDirectory, err := s.dir.FindGroupByName(ctx, role.Name)
	if err != nil {
		return roleTarget{}, err
	}
	target := roleTarget{role: role, onDirectory: onDirectory, recorded: recorded}
	if onDirectory {
		target.group = *live
	}
	target.enrolable = !onDirectory || recorded != nil
	return target, nil
}

// realiseRole makes one classified, enrolable role real and enrols its accounts.
//
// It re-reads nothing: pass 0 already settled whether the platform owns this
// role, and re-deciding here would let the two passes disagree — which is how
// the "leave a pre-existing group alone" rule would come to be enforced in one
// place and not the other.
func (s *EnsureService) realiseRole(ctx context.Context, orgID, projectID string, target roleTarget, memberIDs []string, result *Result) error {
	if target.onDirectory {
		group := target.group
		if len(memberIDs) > 0 {
			// AddMembers is a no-op when every id is already in the group, so an
			// unchanged re-run does not churn the group's identity.
			var err error
			if group, err = s.dir.AddMembers(ctx, group, memberIDs); err != nil {
				return err
			}
		}
		if target.recorded != nil && target.recorded.ThunderGroupID != group.ID {
			row := *target.recorded
			row.ThunderGroupID = group.ID
			if err := s.store.UpsertRole(ctx, row); err != nil {
				return err
			}
		}
		result.RolesReused = append(result.RolesReused, target.role.Name)
		return nil
	}

	// Absent from the directory: create it complete, whether or not a stale row
	// survived from a group somebody deleted out from under us.
	created, err := s.dir.CreateGroup(ctx, target.role.Name, target.role.Description, memberIDs)
	if err != nil {
		return fmt.Errorf("create role %q: %w", target.role.Name, err)
	}
	row := IdPRole{
		Name: target.role.Name, ThunderGroupID: created.ID, Description: target.role.Description,
		CreatedByOrg: orgID, CreatedByProject: projectID,
	}
	if target.recorded != nil {
		// Provenance survives a recreate: the role was first declared by whoever
		// the stale row says, not by whoever rebuilt today. And so does the
		// recorded SPELLING — `name` is the primary key while GetRole matches
		// case-insensitively, so upserting under a design that spells the role
		// `viewer` where the row says `Viewer` would INSERT a second row rather
		// than update the first. One role, one row; the directory group carries
		// the design's spelling either way, and that is the one the token claim
		// uses.
		row.CreatedByOrg, row.CreatedByProject = target.recorded.CreatedByOrg, target.recorded.CreatedByProject
		row.Name = target.recorded.Name
	}
	if err := s.store.UpsertRole(ctx, row); err != nil {
		return err
	}
	result.RolesCreated = append(result.RolesCreated, target.role.Name)
	return nil
}

// ensureUser makes one test account real, and reports whether it may be used.
//
// The refusal case is the one that matters: a username that exists on the
// directory but has no `test_users` row is an account the platform does not
// own. It is left completely untouched — not adopted, not password-reset, not
// enrolled — because the design naming `jsmith` must not hand out a real
// person's login.
func (s *EnsureService) ensureUser(ctx context.Context, planned rolesspec.PlannedUser, result *Result) (DirectoryAccount, bool, error) {
	recorded, err := s.store.GetTestUser(ctx, planned.Username)
	if err != nil {
		return DirectoryAccount{}, false, err
	}
	live, onDirectory, err := s.dir.FindUserByUsername(ctx, planned.Username)
	if err != nil {
		return DirectoryAccount{}, false, err
	}

	switch {
	case recorded != nil && onDirectory:
		// Ours, and present. Keep the password we already sealed — re-rolling it
		// every build would invalidate a credential a human is holding. The
		// facts update deliberately never reads it: revealing a password only to
		// seal it again decrypts a credential for no reason, and would fail the
		// whole build for an account whose sealed password is missing.
		if recorded.ThunderUserID != live.ID || recorded.RoleName != planned.Role {
			if err := s.store.UpdateTestUserFacts(ctx, planned.Username, live.ID, planned.Role); err != nil {
				return DirectoryAccount{}, false, err
			}
		}
		result.UsersReused = append(result.UsersReused, planned.Username)
		return *live, true, nil

	case recorded == nil && onDirectory:
		result.UsersRefused = append(result.UsersRefused, planned.Username)
		return DirectoryAccount{}, false, nil

	default:
		password, err := generatePassword()
		if err != nil {
			return DirectoryAccount{}, false, err
		}
		created, err := s.dir.CreateUser(ctx, planned.Username, testUserEmail(planned.Username), password)
		if err != nil {
			return DirectoryAccount{}, false, fmt.Errorf("create test user %q: %w", planned.Username, err)
		}
		// Seal BEFORE anything can fail after it: the directory now holds a
		// password only this process knows, and losing it here would leave an
		// account nobody can sign in as and the platform cannot rotate.
		row := TestUser{
			Username: planned.Username, ThunderUserID: created.ID,
			RoleName: planned.Role, Email: created.Email,
		}
		if err := s.store.UpsertTestUser(ctx, row, password); err != nil {
			return DirectoryAccount{}, false, err
		}
		result.UsersCreated = append(result.UsersCreated, planned.Username)
		return created, true, nil
	}
}

// testUserEmail gives an account a syntactically valid address in a domain that
// can never resolve. Thunder wants an email; a deliverable one would mean these
// accounts could receive real mail, and `.invalid` is reserved by RFC 2606
// precisely so it cannot.
func testUserEmail(username string) string { return username + "@test-users.invalid" }

// passwordAlphabet is 32 characters: lowercase letters and digits, with every
// lookalike pair broken — no `0` or `O`, no `1`, `l` or `i`. Nothing else.
//
// No uppercase and no symbols, deliberately. These logins are READ far more than
// they are guarded: an agent parses one out of a markdown table cell in the gate
// ticket and exports it into a shell, and a person reads one off the Security
// panel and types it. Mixed case invites a transcription error and a symbol
// invites a quoting one. The `Aep1!`-style prefix that used to lead these
// existed to satisfy an identity-provider password policy — probed, and Thunder
// enforces none, accepting even a one-character password — so it bought nothing
// and cost five of the characters a reader has to get right.
//
// 32 divides 256, so a byte modulo its length is uniform with no rejection loop.
const passwordAlphabet = "abcdefghjkmnopqrstuvwxyz23456789"

// passwordChars is the whole password: 10 characters, 50 bits.
//
// These are throwaway accounts whose logins are published in the gate ticket by
// design (ADR-0022), so the length is not buying secrecy and is not meant to.
// What it buys is that the account cannot be reached by GUESSING, which still
// matters: usernames are deterministic (`test-<role-slug>`) and one directory
// serves every project's real sign-in, so a guessable password would be a
// working login into every app declaring the same role.
const passwordChars = 10

// generatePassword mints a password for a new test account.
func generatePassword() (string, error) {
	buf := make([]byte, passwordChars)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	out := make([]byte, passwordChars)
	for i, b := range buf {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out), nil
}
