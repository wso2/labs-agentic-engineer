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
// identity provider of the environment this version is validated in.
//
// It runs with NO MODEL IN THE LOOP. A model authored security.json and read the
// role catalog; below the version tag everything is deterministic — which is
// the single most important property of this design, because these calls mint
// credentials.
//
// Six passes, and the ORDER is load-bearing.
//
//  0. **Classify**, reading only. Each org group the design needs — the ones
//     `groups[]` introduces and the ones its roles assign to — is settled as
//     one the platform owns, one somebody else made, or one that does not
//     exist yet. An `assignTo` naming a group that is neither declared nor
//     already on the directory FAILS here, before anything is written.
//  1. **Accounts**, and the decision of HOW each one will come to hold each of
//     its roles: through a group the platform owns, or — when no such group
//     would grant the role — bound to the account directly in pass 5.
//  2. **Groups**: create each absent one complete with its members, and add the
//     missing members to each owned one. This is also where a test account is
//     ENROLLED, in the union of the `assignTo` groups of every role it holds
//     (securityspec.PlannedUser.Groups) — membership is settable only at group
//     create, so a brand-new group is created complete and an existing one is
//     edited through the delete-and-recreate path AddMembers owns.
//  3. **Resource server**: find the project's resource server by its derived
//     identifier, create it when absent, record the row.
//  4. **Resources and actions**: converge the permission catalog to the tag.
//  5. **Roles**: converge `<project>/<Role>` to the tag, converge each role's
//     GROUP assignments to `assignTo`, ADD the test-account principals pass 1
//     settled, delete the roles the design dropped, and rewrite the binding
//     rows.
//
// There is no allowlist pass. The OAuth client's `scopes` list is written by the
// provisioning overlay onto the `thunder-app` CR (phase 1), not from here — and
// it enforces nothing either way: ThunderID 1.0.0 stores the field, reads it
// back and silently drops an unknown or ungranted scope (spike P1 §6). The write
// gate on security.json is what keeps a stale handle out.
//
// Passes 0–2 are ADDITIVE and passes 3–5 CONVERGE. That split is ownership, not
// taste: groups and accounts are shared within the (org, environment) and a
// second project naming one means the same object, while the resource server,
// its catalog and the project-prefixed roles are created by exactly one project
// and carry its name, so the ensure may delete what the design dropped. See
// README.md, "project-owned converge, shared additive".
//
// Classification has to come first because of what pass 1 costs. Creating an
// account MINTS A CREDENTIAL: it seals a password, and it writes a reference row
// that the validation credential provider later serves as "the login for this
// role". Doing that for a role the account will not end up holding produces
// exactly the failure this whole design exists to close — validation signing in
// as an account that holds no role at all, and grading role-gated criteria
// against it.
//
// So pass 1 settles, per account and per role, WHICH of the two grants applies,
// and creates the account only when at least one of them will:
//
//   - a role assigned to a group the platform owns is granted by ENROLMENT, and
//     that is the normal path;
//   - a role assigned only to groups somebody else made — the design's own
//     `Approver → Finance (reused)` — is granted by binding the role to the
//     ACCOUNT (a user principal, pass 5). The group is still left completely
//     alone, which is the whole point: the login holds the project role without
//     the platform putting a disposable account into an org group it does not
//     own.
//   - a role assigned to no group at all (self-service, service) is granted by
//     neither, and an account whose every role is of that shape is not created:
//     a standing credential for a login that holds nothing is worse than no
//     credential. It is reported and skipped.
//
// Accounts still come before groups, because the IdP sets group membership only
// when a group is CREATED. Knowing the member ids up front lets a brand-new role
// be created complete, in one call, instead of created empty and then
// deleted-and-recreated to add its members — which would change the group's id
// on its very first build for no reason.
//
// Two rules carry all the safety, and both reduce to the same ownership marker
// — a row in this package's own tables:
//
//   - **The platform enrols members only into groups it created.** That is what
//     stops a design that reasonably reuses `Administrators` from getting a
//     platform-made test account into the group `setup-aep.sh` binds to
//     OpenChoreo's `admin` role. It is a rule, not a denylist, so every
//     hand-made group is protected without a list to maintain. The account
//     still gets the PROJECT role directly (see pass 1): a `<project>/<Role>`
//     is created and owned by this project and grants only what this project's
//     catalog declares, so binding one to a test account carries none of the
//     authority membership of somebody else's group would.
//   - **The platform modifies only accounts it owns.** A design naming an
//     existing username the platform has no row for is REFUSED, never adopted:
//     otherwise a design naming a real person would reset their password and
//     hand it to a validation runner.

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// EnsureService makes a project's declared roles and test users real.
type EnsureService struct {
	targets TargetResolver
	store   Store
	design  DesignReader
}

// NewEnsureService builds the ensure. Every collaborator is required; a nil one
// is a wiring defect, and the composition root skips wiring the whole feature
// rather than passing a nil (see Enabled).
func NewEnsureService(targets TargetResolver, store Store, design DesignReader) *EnsureService {
	return &EnsureService{targets: targets, store: store, design: design}
}

// Enabled reports whether the ensure can run. It is false when no target
// resolver is wired — a local stack that cannot reach OpenChoreo or the secret
// store — and the caller then skips the roles gate entirely instead of failing
// every build. A resolver that is wired but cannot resolve THIS org's
// environment is a different thing: that is an error the gate reports, because
// the environment genuinely has no identity provider.
func (s *EnsureService) Enabled() bool {
	return s != nil && s.targets != nil && s.store != nil && s.design != nil
}

// Result is what one ensure did, for the gate's closing comment and the logs.
type Result struct {
	// GroupsCreated / GroupsReused are the org groups this run made and the ones
	// it found already on the directory. The design names them in `groups[]` and
	// in its roles' `assignTo`.
	GroupsCreated []string
	GroupsReused  []string
	// GroupsPreExisting are groups that exist on the directory but that the
	// platform did not create. It does not enrol members into these.
	GroupsPreExisting []string
	UsersCreated      []string
	UsersReused       []string
	// UsersRefused are usernames the design named that already exist on the
	// directory as accounts the platform does not own.
	UsersRefused []string
	// UsersSkipped are accounts the design asked for whose roles assign to no
	// org group at all — the self-service and service shapes, which nothing
	// here enrols. They are deliberately not created: an account that would
	// hold no role is a standing credential for a login that holds nothing, and
	// serving it to validation would be worse than serving nothing.
	//
	// A role assigned only to a group somebody else made does NOT land here: the
	// account is created and the role is bound to it directly (see the file
	// header, pass 1).
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
	// RolesConverged are the project roles this run made match the tag, and
	// RolesDeleted the ones the tag no longer declares. Unlike a group, a
	// project role is owned by exactly one project, so the second list is a
	// normal outcome rather than something a human has to look at.
	RolesConverged []string
	RolesDeleted   []string
	// ResourceIdentifier is the project's resource server — the absolute URI
	// that is the access token's `aud`, the audience the gateway checks and the
	// `resource` the generated SPA asks for. It is DERIVED, not looked up
	// (ResourceServerIdentifier), so it is filled even on a run that wrote
	// nothing, and the gate publishes it beside the logins: a scoped token
	// cannot be minted without it.
	ResourceIdentifier string
	// Issuer is the identity provider these accounts were created on, and the
	// only one their logins work at. The gate prints it beside the credentials:
	// with one identity provider per environment, a password published without
	// its issuer names no sign-in anybody can reach.
	Issuer string
	// Environment is the environment whose identity provider was used, for the
	// gate comment and the logs.
	Environment string
}

// Credential is one test account's login, as published to the provisioning
// ticket the validation agent reads.
//
// Password is empty when the seal could not be opened. That is reported rather
// than swallowed and rather than fatal: the account itself is fine and the
// build must not die over a publishing step, but the ticket has to say the row
// is unusable instead of printing a blank and reading as a password-less login.
type Credential struct {
	Username string
	Password string
	// Roles are every project role this login holds, in the order the design
	// authored them. v2 lets one account hold several — the union of their
	// grants is what its token carries — so the published column is plural and
	// the singular `RoleName` on the stored row is only the one the account
	// exists FOR.
	Roles []string
	// Scopes is the union of those roles' grants: the catalog handles this
	// login's access token will carry, SORTED so two runs of one tag publish
	// byte-identical rows. It is what lets the validation agent know, before it
	// opens a browser, which criteria this account can and cannot exercise.
	Scopes []string
	// ColdStart is a v1 leftover carried for the wire contract and is always
	// false — see TestUserRef.ColdStart in entities.go. Phase 5 removes it.
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
	add("Groups created", r.GroupsCreated)
	add("Groups reused", r.GroupsReused)
	add("Groups left alone (not created by the platform, so no members are enrolled)", r.GroupsPreExisting)
	add("Test users created", r.UsersCreated)
	add("Test users reused", r.UsersReused)
	add("Test users refused (the username already belongs to an account the platform does not own)", r.UsersRefused)
	add("Test users not created (their roles are assigned to no org group)", r.UsersSkipped)
	add("Project roles", r.RolesConverged)
	add("Project roles deleted (no longer declared at this version)", r.RolesDeleted)
	if len(lines) == 0 {
		return "Nothing to provision — the design declares no roles."
	}
	return strings.Join(lines, "\n")
}

// HasRefusals reports whether anything was refused. A refusal is not an error —
// the build continues — but it is the one outcome a human has to look at, so
// the gate comment leads with it and the caller can decide to surface it.
func (r Result) HasRefusals() bool { return len(r.UsersRefused) > 0 }

// EnsureForTag reads security.json at tag and makes its contents real.
//
// A project whose design declares no roles document is not an error: it has no
// sign-in, and there is nothing to ensure. A security.json that is present but
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
	doc, err := securityspec.Parse([]byte(raw))
	if err != nil {
		return Result{}, true, fmt.Errorf("%s at %s: %w", securityspec.Path, tag, err)
	}
	// WHICH directory, before anything is written. The resolver picks the
	// environment (see TargetResolver) and hands back a Directory already bound
	// to that environment's identity provider; an unbound environment is an
	// error here rather than a write onto the wrong tier.
	target, err := s.targets.Resolve(ctx, orgID)
	if err != nil {
		return Result{}, true, err
	}
	result, err = s.ensure(ctx, target, projectID, securityspec.Plan(doc))
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
	raw, ok := bundle[securityspec.BundleKey]
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	return raw, true, nil
}

// groupTarget is one org group the design needs, after classification.
type groupTarget struct {
	planned securityspec.PlannedGroup
	// group is the live directory group, zero when it is absent from it.
	group       DirectoryGroup
	onDirectory bool
	// recorded is the platform's own row for the group, nil when it has none.
	recorded *IdPRole
	// enrolable is the whole point of classifying: true when the platform may
	// put a member into this group — because it created it, or because it is
	// about to. False for a group somebody else made.
	enrolable bool
}

// ensure runs the passes over a plan, against ONE target's directory.
func (s *EnsureService) ensure(ctx context.Context, target Target, projectID string, plan securityspec.EnsurePlan) (Result, error) {
	result := Result{
		Issuer:      target.Issuer,
		Environment: target.Environment,
		// Derived, not read back: the identifier is agreed on by parties that
		// never speak to each other (see resource_server.go), so it is known
		// before the directory is touched and is published even by a run that
		// fails later.
		ResourceIdentifier: ResourceServerIdentifier(target.OrgID, projectID),
	}
	orgID, scope, dir := target.OrgID, target.Scope(), target.Directory

	// ---- pass 0: classify, writing nothing --------------------------------
	targets := make([]groupTarget, 0, len(plan.Groups))
	enrolable := make(map[string]bool, len(plan.Groups))
	// groups is every org group the design names, by lowercased name, as the
	// directory currently holds it. The ROLE pass reads it to turn an `assignTo`
	// name into the principal id to bind — which is why it is filled with the
	// id a group has AFTER pass 2: editing a group's membership mints a new id,
	// and assigning the old one would bind a principal that no longer exists.
	groups := make(map[string]DirectoryGroup, len(plan.Groups))
	for _, group := range plan.Groups {
		classified, err := s.classifyGroup(ctx, scope, dir, group)
		if err != nil {
			return result, err
		}
		// An `assignTo` name is a REFERENCE to a group the org already has;
		// `groups[]` is the only place a design may introduce one. A name that is
		// neither — the typo case — would otherwise be created here, quietly
		// minting an org group nobody asked for and assigning a project role to
		// it, so it fails the gate instead, by name.
		//
		// A stale row for a group somebody deleted is not that case: the platform
		// created it, so recreating it is the same additive act as the first
		// build, and the ensure says so rather than refusing a recoverable state.
		if !group.Declared && !classified.onDirectory && classified.recorded == nil {
			return result, fmt.Errorf(
				"the design assigns a role to the group %q, which %s's identity provider does not have "+
					"and `groups[]` does not declare — correct the name, or declare the group",
				group.Name, scope)
		}
		targets = append(targets, classified)
		if classified.enrolable {
			enrolable[strings.ToLower(group.Name)] = true
			continue
		}
		// Somebody else's group. Left entirely alone as far as MEMBERSHIP goes,
		// and nothing is minted for it — see the file header on why that includes
		// its accounts. A role may still be assigned to it: binding a role to a
		// group grants that group's members the role and changes nothing about
		// the group, which is exactly the reuse `assignTo` exists for.
		slog.InfoContext(ctx, "roles ensure: leaving a pre-existing directory group alone",
			"group", group.Name, "org", orgID, "environment", target.Environment, "project", projectID)
		result.GroupsPreExisting = append(result.GroupsPreExisting, group.Name)
		groups[strings.ToLower(group.Name)] = classified.group
	}

	// ---- pass 1: accounts, and how each one will come to hold its roles ----
	//
	// Refusal and skipping are both per account and neither stops the pass: one
	// design naming a real person's username, or reusing one hand-made group,
	// must not block the accounts around it.
	assignTo := make(map[string][]string, len(plan.Roles))
	for _, role := range plan.Roles {
		assignTo[role.Name] = role.AssignTo
	}
	membersByGroup := map[string][]string{}
	// directPrincipals is role name → the test accounts pass 5 binds to the role
	// DIRECTLY, as user principals, because no group the platform owns would
	// grant it. See directRoles.
	directPrincipals := map[string][]DirectoryID{}
	var refs []TestUserRef
	for _, planned := range plan.Users {
		joins := enrolableGroups(planned, enrolable)
		direct := directRoles(planned, assignTo, enrolable)
		if len(joins) == 0 && len(direct) == 0 {
			// Nothing would give this account a role: its roles assign to no
			// group at all (the self-service shape). A standing credential for a
			// login that holds nothing is worse than no credential, so it is not
			// minted.
			result.UsersSkipped = append(result.UsersSkipped, planned.Username)
			continue
		}
		account, usable, err := s.ensureUser(ctx, scope, dir, planned, &result)
		if err != nil {
			return result, err
		}
		if !usable {
			continue
		}
		for _, group := range joins {
			key := strings.ToLower(group)
			membersByGroup[key] = append(membersByGroup[key], account.ID)
		}
		for _, role := range direct {
			directPrincipals[role] = append(directPrincipals[role], DirectoryID(account.ID))
		}
		// A reference is the statement "this account is the login for this role
		// in this project", and the credential provider reads it as exactly
		// that. It is written only for an account that WILL be enrolled.
		refs = append(refs, TestUserRef{
			Username: planned.Username,
			RoleName: primaryRole(planned),
			Supplied: planned.Supplied,
		})
	}

	// ---- pass 2: groups, and the enrolment that rides on them -------------
	for _, classified := range targets {
		if !classified.enrolable {
			continue
		}
		group, err := s.realiseGroup(ctx, target, projectID, classified,
			membersByGroup[strings.ToLower(classified.planned.Name)], &result)
		if err != nil {
			return result, err
		}
		groups[strings.ToLower(classified.planned.Name)] = group
	}

	// ---- passes 3-5: the project's own authorization objects --------------
	//
	// Everything from here down CONVERGES to the tag, and everything above it is
	// additive. The boundary is exactly the ownership boundary.
	rs, err := s.ensureResourceServer(ctx, target, projectID, result.ResourceIdentifier)
	if err != nil {
		return result, err
	}
	// Actions before roles, and not by convention: the directory refuses a role
	// granting a permission no action derives (400 ROL-1012), so a handle added
	// at this version has to exist before the role that grants it is written.
	if err := convergeCatalog(ctx, dir, rs, plan.Catalog); err != nil {
		return result, err
	}
	bindings, err := s.convergeRoles(ctx, target, projectID, rs, plan, groups, directPrincipals, &result)
	if err != nil {
		return result, err
	}
	if err := s.store.ReplaceRoleBindings(ctx, scope, projectID, bindings); err != nil {
		return result, err
	}

	// The references are rewritten wholesale, so a role dropped from the design
	// stops being referenced by this project — while the GROUP behind it stands,
	// per the additive-only rule for shared objects.
	if err := s.store.ReplaceProjectRefs(ctx, scope, projectID, refs); err != nil {
		return result, err
	}

	result.Credentials = s.collectCredentials(ctx, scope, projectID, refs, plan)
	return result, nil
}

// ---- pass 3: the resource server -------------------------------------------

// ensureResourceServer makes the project's resource server real and records it.
//
// The identifier is the key on both sides — the directory is asked by identifier
// and the row is unique by it — because it is the only name that travels: it is
// the token's `aud`, the gateway's expected audience and the SPA's `resource`.
// The row is rewritten only when it would say something different, so a rebuild
// of an unchanged tag leaves `updated_at` alone and the console can still say
// how long the resource server has stood.
func (s *EnsureService) ensureResourceServer(ctx context.Context, target Target, projectID, identifier string) (DirectoryID, error) {
	rs, err := target.Directory.EnsureResourceServer(ctx, identifier, projectID)
	if err != nil {
		return "", fmt.Errorf("ensure the resource server %q: %w", identifier, err)
	}
	scope := target.Scope()
	recorded, err := s.store.GetResourceServer(ctx, scope, projectID)
	if err != nil {
		return "", err
	}
	if recorded != nil && recorded.Identifier == identifier && recorded.DirectoryID == string(rs) {
		return rs, nil
	}
	row := IdPResourceServer{
		OrgID: target.OrgID, Environment: target.Environment, ProjectID: projectID,
		Identifier: identifier, DirectoryID: string(rs),
	}
	if err := s.store.UpsertResourceServer(ctx, row); err != nil {
		return "", err
	}
	return rs, nil
}

// ---- pass 4: the permission catalog ----------------------------------------

// convergeCatalog makes the resource server's resources and actions exactly what
// the tag declares — the handles it holds AND the prose those objects carry.
//
// Creates first, then descriptions, then deletes leaf-first. Each half matters:
//
//   - handles are the IDENTITY and are immutable on the directory, so a rename
//     in the design is a delete plus a create — which is precisely what this
//     diff produces, with no rename concept anywhere;
//   - a DESCRIPTION is content, never identity. An edited sentence is one update
//     call and nothing else: turning it into a delete-and-create would take the
//     resource's actions with it and cascade the permission out of every role
//     that granted it, so a wording change would revoke access. Handles are
//     compared; descriptions are only written when they differ, and a difference
//     never influences what is created or deleted.
//   - deleting an action CASCADES it out of every role that granted it, silently
//     and with no error (P1 §7), so no pass has to strip a role's permissions
//     first. It is also why the role pass runs after this one and reads its
//     grants fresh.
//
// The NAME a resource or action is created under is its handle: a security
// document authors no display name, and inventing one here would be a second
// spelling of the handle that only this function could correct.
func convergeCatalog(ctx context.Context, dir Directory, rs DirectoryID, wanted []securityspec.PlannedResource) error {
	live, err := dir.ListResources(ctx, rs)
	if err != nil {
		return fmt.Errorf("read the permission catalog: %w", err)
	}
	liveByHandle := make(map[string]DirectoryResource, len(live))
	liveActions := make(map[string][]DirectoryAction, len(live))
	for _, resource := range live {
		liveByHandle[resource.Handle] = resource
		actions, aerr := dir.ListActions(ctx, rs, resource.ID)
		if aerr != nil {
			return fmt.Errorf("read the actions of %q: %w", resource.Handle, aerr)
		}
		liveActions[resource.Handle] = actions
	}

	for _, want := range wanted {
		resource, present := liveByHandle[want.Handle]
		if !present {
			created, cerr := dir.CreateResource(ctx, rs, want.Handle, want.Handle, want.Description)
			if cerr != nil {
				return fmt.Errorf("create resource %q: %w", want.Handle, cerr)
			}
			resource = created
		} else if resource.Description != want.Description {
			if uerr := dir.UpdateResource(ctx, rs, resource.ID, want.Handle, want.Description); uerr != nil {
				return fmt.Errorf("update resource %q: %w", want.Handle, uerr)
			}
		}
		held := make(map[string]DirectoryAction, len(liveActions[want.Handle]))
		for _, action := range liveActions[want.Handle] {
			held[action.Handle] = action
		}
		for _, action := range want.Actions {
			existing, there := held[action.Handle]
			if !there {
				if _, cerr := dir.CreateAction(ctx, rs, resource.ID, action.Handle, action.Handle, action.Description); cerr != nil {
					return fmt.Errorf("create action %q: %w", want.Handle+":"+action.Handle, cerr)
				}
				continue
			}
			if existing.Description == action.Description {
				continue
			}
			if uerr := dir.UpdateAction(ctx, rs, resource.ID, existing.ID, action.Handle, action.Description); uerr != nil {
				return fmt.Errorf("update action %q: %w", want.Handle+":"+action.Handle, uerr)
			}
		}
	}

	// The delete half. `declared` doubles as "is this resource still declared"
	// and "which of its actions still are", so a resource the tag dropped loses
	// every action here and is removed below — the leaf-first order the
	// directory demands.
	declared := make(map[string]map[string]bool, len(wanted))
	for _, want := range wanted {
		set := make(map[string]bool, len(want.Actions))
		for _, action := range want.Actions {
			set[action.Handle] = true
		}
		declared[want.Handle] = set
	}
	for _, resource := range live {
		for _, action := range liveActions[resource.Handle] {
			if declared[resource.Handle][action.Handle] {
				continue
			}
			if derr := dir.DeleteAction(ctx, rs, resource.ID, action.ID); derr != nil {
				return fmt.Errorf("delete action %q: %w", resource.Handle+":"+action.Handle, derr)
			}
		}
	}
	for _, resource := range live {
		if _, stillDeclared := declared[resource.Handle]; stillDeclared {
			continue
		}
		if derr := dir.DeleteResource(ctx, rs, resource.ID); derr != nil {
			return fmt.Errorf("delete resource %q: %w", resource.Handle, derr)
		}
	}
	return nil
}

// ---- pass 5: the project roles ---------------------------------------------

// convergeRoles makes the `<project>/<Role>` roles, and their group
// assignments, exactly what the tag declares, and returns the binding rows that
// record the result.
//
// The project PREFIX is what makes this safe to be a converge at all: roles live
// in one flat namespace per organisation unit, so the ensure selects the ones
// carrying its own prefix and may delete the ones the design dropped, knowing no
// other project could have created them.
func (s *EnsureService) convergeRoles(
	ctx context.Context, target Target, projectID string, rs DirectoryID,
	plan securityspec.EnsurePlan, groups map[string]DirectoryGroup,
	directPrincipals map[string][]DirectoryID, result *Result,
) ([]IdPRoleBinding, error) {
	dir := target.Directory
	live, err := dir.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the directory's roles: %w", err)
	}
	prefix := RoleNamePrefix(projectID)
	var owned []RoleRef
	// byName is the listing indexed the way the converge asks for it: two names
	// differing only in case are one role, here and on the directory. It is what
	// lets EnsureRole be told the id instead of listing the whole directory again
	// per declared role.
	byName := make(map[string]DirectoryID, len(live))
	for _, ref := range live {
		byName[strings.ToLower(ref.Name)] = ref.ID
		if strings.HasPrefix(strings.ToLower(ref.Name), prefix) {
			owned = append(owned, ref)
		}
	}

	declared := make(map[string]bool, len(plan.Roles))
	var bindings []IdPRoleBinding
	for _, role := range plan.Roles {
		name := RoleName(projectID, role.Name)
		declared[strings.ToLower(name)] = true
		roleID, rerr := dir.EnsureRole(ctx, byName[strings.ToLower(name)], name, role.Description, rs, plan.Grants[role.Name])
		if rerr != nil {
			return nil, fmt.Errorf("ensure role %q: %w", name, rerr)
		}
		if aerr := convergeAssignments(ctx, dir, roleID, name, role.AssignTo, groups,
			directPrincipals[role.Name]); aerr != nil {
			return nil, aerr
		}
		result.RolesConverged = append(result.RolesConverged, role.Name)
		if len(role.AssignTo) == 0 {
			// Recorded, assigned to nobody — the normal shape for a self-service
			// role (the registration flow assigns it per account) and for a
			// service one (phase 6 attaches an app principal). The row still has
			// to exist: it is what the delete path reads to find the role.
			bindings = append(bindings, IdPRoleBinding{Role: role.Name, DirectoryRoleID: string(roleID)})
			continue
		}
		for _, group := range role.AssignTo {
			bindings = append(bindings, IdPRoleBinding{
				Role: role.Name, GroupName: directoryGroupName(group, groups),
				DirectoryRoleID: string(roleID),
			})
		}
	}

	// Roles this project owns that the tag no longer declares. Assignments come
	// off first: the directory drops them with the role either way, but a role
	// that fails to delete after its assignments are gone grants nothing, while
	// the reverse leaves a live grant behind.
	for _, ref := range owned {
		if declared[strings.ToLower(ref.Name)] {
			continue
		}
		held, lerr := dir.ListRoleAssignments(ctx, ref.ID)
		if lerr != nil {
			return nil, fmt.Errorf("read the assignments of %q: %w", ref.Name, lerr)
		}
		for _, principal := range held {
			if uerr := dir.UnassignRole(ctx, ref.ID, principal); uerr != nil {
				// Reported and stepped over, not fatal — the same choice
				// teardown.go makes for the same reason. The unassign pass is
				// not bookkeeping the directory needs: DeleteRole below takes
				// the assignments with it. It is there to make the removal read
				// as a removal of grants. A principal of a kind this platform
				// never writes — Thunder also assigns to `agent` — cannot be
				// mapped onto the wire by the adapter, and failing the whole
				// BUILD because a role nobody declares any more holds one would
				// be the tail wagging the dog.
				slog.WarnContext(ctx, "roles ensure: could not unassign a principal from a role being deleted",
					"role", ref.Name, "principal", principalName(principal),
					"kind", string(principal.Kind), "error", uerr)
			}
		}
		if derr := dir.DeleteRole(ctx, ref.ID); derr != nil {
			return nil, fmt.Errorf("delete role %q: %w", ref.Name, derr)
		}
		// Reported under the name the DESIGN used, not the directory's: the
		// prefix is the platform's ownership device and means nothing to a
		// reader of the gate ticket. The prefix matched case-insensitively and
		// has the same length, so the remainder is the role half verbatim.
		result.RolesDeleted = append(result.RolesDeleted, ref.Name[len(prefix):])
	}
	return bindings, nil
}

// convergeAssignments makes a role's GROUP assignments match `assignTo`, and
// ADDS the test accounts no group of this role would grant it to (`users`).
//
// It converges groups and ONLY groups. A user principal is somebody the app's
// registration flow enrolled (phase 7), an administrator assigned by hand, or
// one of the platform's own test accounts bound here; an app principal is a
// service identity (phase 6). None of them is declared in `assignTo`, so
// treating "not in the tag" as "remove" would make every build revoke them.
// They are left exactly as they are.
//
// That "never remove a user principal" rule covers the platform's OWN test
// accounts too, deliberately: a test account that is deleted and recreated gets
// a new directory id, so the principal left behind names an account that no
// longer exists and grants nobody anything, and a teardown removes the role
// outright. Removing them would mean this converge deciding which user
// principals are the platform's — a second ownership rule, for a stale row that
// is already harmless.
//
// A group whose membership changed earlier in this build has a NEW id — the
// directory sets members only at create, so an edit is a delete-and-recreate —
// which shows up here as one removal and one addition. That is correct rather
// than churn: the assignment named an object that no longer exists.
func convergeAssignments(
	ctx context.Context, dir Directory, role DirectoryID, roleName string,
	assignTo []string, groups map[string]DirectoryGroup, users []DirectoryID,
) error {
	held, err := dir.ListRoleAssignments(ctx, role)
	if err != nil {
		return fmt.Errorf("read the assignments of %q: %w", roleName, err)
	}
	wanted := make(map[DirectoryID]bool, len(assignTo))
	for _, name := range assignTo {
		group, resolved := groups[strings.ToLower(name)]
		if !resolved {
			// Pass 0 refuses an unresolvable assignTo before anything is written,
			// so reaching this is a defect in this file rather than a bad design.
			return fmt.Errorf("role %q assigns to the group %q, which no pass resolved", roleName, name)
		}
		wanted[DirectoryID(group.ID)] = true
	}

	holds := make(map[DirectoryID]bool, len(held))
	holdsUser := make(map[DirectoryID]bool, len(held))
	for _, principal := range held {
		if principal.Kind != PrincipalGroup {
			if principal.Kind == PrincipalUser {
				holdsUser[principal.ID] = true
			}
			continue
		}
		holds[principal.ID] = true
		if wanted[principal.ID] {
			continue
		}
		if uerr := dir.UnassignRole(ctx, role, Principal{Kind: PrincipalGroup, ID: principal.ID}); uerr != nil {
			return fmt.Errorf("unassign %q from %q: %w", principal.Display, roleName, uerr)
		}
	}
	for _, name := range assignTo {
		id := DirectoryID(groups[strings.ToLower(name)].ID)
		if holds[id] {
			continue
		}
		if aerr := dir.AssignRole(ctx, role, Principal{Kind: PrincipalGroup, ID: id}); aerr != nil {
			return fmt.Errorf("assign %q to %q: %w", name, roleName, aerr)
		}
	}
	// The test accounts this role reaches no other way. Additive, and idempotent
	// because an account already holding the role is skipped here rather than
	// re-assigned — which is what keeps a rebuild of an unchanged tag at zero
	// writes.
	for _, id := range users {
		if holdsUser[id] {
			continue
		}
		if aerr := dir.AssignRole(ctx, role, Principal{Kind: PrincipalUser, ID: id}); aerr != nil {
			return fmt.Errorf("assign the test account %q to %q: %w", id, roleName, aerr)
		}
	}
	return nil
}

// directoryGroupName is the spelling a binding row records for an `assignTo`
// name: the RESOLVED directory group's own name when pass 0 found one, and the
// document's text otherwise.
//
// The rows are read back by name (CountProjectsBindingGroup, the Security
// panel) against names that come from the DIRECTORY, so recording the
// document's casing would make one group read as two. The store compares
// case-insensitively as a second line of defence; this is the first.
func directoryGroupName(name string, groups map[string]DirectoryGroup) string {
	if resolved, ok := groups[strings.ToLower(name)]; ok && resolved.Name != "" {
		return resolved.Name
	}
	return name
}

// enrolableGroups is the subset of an account's groups the platform may put it
// into, in plan order.
func enrolableGroups(planned securityspec.PlannedUser, enrolable map[string]bool) []string {
	var joins []string
	for _, group := range planned.Groups {
		if enrolable[strings.ToLower(group)] {
			joins = append(joins, group)
		}
	}
	return joins
}

// directRoles is the subset of an account's roles that pass 5 must bind to the
// account ITSELF, as a user principal, in plan order.
//
// It is the answer to the design's own `Approver → Finance (reused)`: the
// platform will not enrol a disposable account into an org group it does not
// own (ADR-0022 — that is what stops a test login joining `Administrators`),
// but the login still has to hold the role the ticket publishes it under, or
// validation signs in as an account that grants nothing and grades role-gated
// criteria against it.
//
// A role a group the platform owns already covers is NOT listed: enrolment
// through that group grants it, and a second, direct principal would be a grant
// the converge can never take back for no gain. A role assigning to no group at
// all — the self-service and service shapes — is not listed either: nothing
// here is meant to enrol it.
func directRoles(planned securityspec.PlannedUser, assignTo map[string][]string, enrolable map[string]bool) []string {
	var out []string
	for _, role := range planned.Roles {
		groups := assignTo[role]
		if len(groups) == 0 {
			continue
		}
		covered := false
		for _, group := range groups {
			if enrolable[strings.ToLower(group)] {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, role)
		}
	}
	return out
}

// primaryRole is the role an account is recorded under. v2 lets one account
// hold several — the union of their grants is what its token carries — while
// the stored row and the ticket's Role column still name one. The first is that
// one: `testUsers[].roles` is authored in the order the designer thinks of the
// account, so the first is the role it exists FOR. Phase 2 makes the record
// plural along with the project-role pass.
func primaryRole(planned securityspec.PlannedUser) string {
	if len(planned.Roles) == 0 {
		return ""
	}
	return planned.Roles[0]
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
func (s *EnsureService) collectCredentials(ctx context.Context, scope Scope, projectID string, refs []TestUserRef, plan securityspec.EnsurePlan) []Credential {
	// The plan is the source of the roles and the scopes, not the stored row:
	// the row keeps ONE role name (the account exists for it) while a v2 account
	// may hold several, and the scopes are the union of their grants, which no
	// table holds. Both are already expanded deterministically by
	// securityspec.Plan, so reading them here cannot disagree with what the role
	// pass just made real.
	planned := make(map[string]securityspec.PlannedUser, len(plan.Users))
	for _, user := range plan.Users {
		planned[user.Username] = user
	}
	out := make([]Credential, 0, len(refs))
	for _, ref := range refs {
		cred := Credential{
			Username:  ref.Username,
			Roles:     slices.Clone(planned[ref.Username].Roles),
			Scopes:    sortedScopes(planned[ref.Username].Scopes),
			ColdStart: ref.ColdStart,
		}
		password, err := s.store.RevealTestUserPassword(ctx, scope, ref.Username)
		if err != nil {
			slog.WarnContext(ctx, "roles ensure: could not open a test user's password to publish it",
				"scope", scope.String(), "project", projectID, "username", ref.Username, "error", err)
		} else {
			cred.Password = password
		}
		out = append(out, cred)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// sortedScopes is the published form of a login's handles: a copy, sorted, so
// two runs of one tag publish the same row byte for byte and a reader can find a
// handle without reading the whole cell. The plan's own order is the design's
// declaration order, which is right for the console and wrong for a ticket that
// is diffed across builds.
func sortedScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	out := slices.Clone(scopes)
	sort.Strings(out)
	return out
}

// classifyGroup settles what the platform may do with one org group, reading
// only.
//
// Three outcomes, and the middle one is the safety property:
//
//   - recorded AND on the directory → the platform created it; enrolable.
//   - NOT recorded but ON the directory → somebody else made it. NOT enrolable.
//     `Administrators` is the case that matters: `setup-aep.sh` maps it to
//     OpenChoreo's `admin` role, so a design that reasonably reuses the name
//     must not get a platform-made account into it. This is a rule, not a
//     denylist — every hand-made group is protected by it, with no list to
//     maintain. A group a project REUSES on purpose (the design's `Finance`)
//     lands here too, and that is the intended outcome: it is enrolled into
//     only if the platform made it.
//   - absent from the directory → the platform is about to create it, whether or
//     not a stale row survived somebody deleting the group; enrolable.
func (s *EnsureService) classifyGroup(ctx context.Context, scope Scope, dir Directory, group securityspec.PlannedGroup) (groupTarget, error) {
	recorded, err := s.store.GetRole(ctx, scope, group.Name)
	if err != nil {
		return groupTarget{}, err
	}
	live, onDirectory, err := dir.FindGroupByName(ctx, group.Name)
	if err != nil {
		return groupTarget{}, err
	}
	classified := groupTarget{planned: group, onDirectory: onDirectory, recorded: recorded}
	if onDirectory {
		classified.group = *live
	}
	classified.enrolable = !onDirectory || recorded != nil
	return classified, nil
}

// realiseGroup makes one classified, enrolable org group real and enrols its
// accounts.
//
// It re-reads nothing: pass 0 already settled whether the platform owns this
// group, and re-deciding here would let the two passes disagree — which is how
// the "leave a pre-existing group alone" rule would come to be enforced in one
// place and not the other.
//
// It returns the group as the directory now holds it, because its ID CHANGES on
// a membership edit and the role pass has to bind the current one.
func (s *EnsureService) realiseGroup(ctx context.Context, target Target, projectID string, classified groupTarget, memberIDs []string, result *Result) (DirectoryGroup, error) {
	if classified.onDirectory {
		group := classified.group
		if len(memberIDs) > 0 {
			// AddMembers is a no-op when every id is already in the group, so an
			// unchanged re-run does not churn the group's identity.
			var err error
			if group, err = target.Directory.AddMembers(ctx, group, memberIDs); err != nil {
				return DirectoryGroup{}, err
			}
		}
		if classified.recorded != nil && classified.recorded.ThunderGroupID != group.ID {
			row := *classified.recorded
			row.ThunderGroupID = group.ID
			if err := s.store.UpsertRole(ctx, row); err != nil {
				return DirectoryGroup{}, err
			}
		}
		result.GroupsReused = append(result.GroupsReused, classified.planned.Name)
		return group, nil
	}

	// Absent from the directory: create it complete, whether or not a stale row
	// survived from a group somebody deleted out from under us.
	created, err := target.Directory.CreateGroup(ctx, classified.planned.Name, classified.planned.Description, memberIDs)
	if err != nil {
		return DirectoryGroup{}, fmt.Errorf("create group %q: %w", classified.planned.Name, err)
	}
	row := IdPRole{
		OrgID: target.OrgID, Environment: target.Environment,
		Name: classified.planned.Name, ThunderGroupID: created.ID, Description: classified.planned.Description,
		CreatedByOrg: target.OrgID, CreatedByProject: projectID,
	}
	if classified.recorded != nil {
		// Provenance survives a recreate: the role was first declared by whoever
		// the stale row says, not by whoever rebuilt today. And so does the
		// recorded SPELLING — `name` is the primary key while GetRole matches
		// case-insensitively, so upserting under a design that spells the role
		// `viewer` where the row says `Viewer` would INSERT a second row rather
		// than update the first. One role, one row; the directory group carries
		// the design's spelling either way, and that is the one the token claim
		// uses.
		row.CreatedByOrg, row.CreatedByProject = classified.recorded.CreatedByOrg, classified.recorded.CreatedByProject
		row.Name = classified.recorded.Name
	}
	if err := s.store.UpsertRole(ctx, row); err != nil {
		return DirectoryGroup{}, err
	}
	result.GroupsCreated = append(result.GroupsCreated, classified.planned.Name)
	return created, nil
}

// ensureUser makes one test account real, and reports whether it may be used.
//
// The refusal case is the one that matters: a username that exists on the
// directory but has no `test_users` row is an account the platform does not
// own. It is left completely untouched — not adopted, not password-reset, not
// enrolled — because the design naming `jsmith` must not hand out a real
// person's login.
func (s *EnsureService) ensureUser(ctx context.Context, scope Scope, dir Directory, planned securityspec.PlannedUser, result *Result) (DirectoryAccount, bool, error) {
	recorded, err := s.store.GetTestUser(ctx, scope, planned.Username)
	if err != nil {
		return DirectoryAccount{}, false, err
	}
	live, onDirectory, err := dir.FindUserByUsername(ctx, planned.Username)
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
		if recorded.ThunderUserID != live.ID || recorded.RoleName != primaryRole(planned) {
			if err := s.store.UpdateTestUserFacts(ctx, scope, planned.Username, live.ID, primaryRole(planned)); err != nil {
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
		created, err := dir.CreateUser(ctx, planned.Username, testUserEmail(planned.Username), password)
		if err != nil {
			return DirectoryAccount{}, false, fmt.Errorf("create test user %q: %w", planned.Username, err)
		}
		// Seal BEFORE anything can fail after it: the directory now holds a
		// password only this process knows, and losing it here would leave an
		// account nobody can sign in as and the platform cannot rotate.
		row := TestUser{
			OrgID: scope.OrgID, Environment: scope.Environment,
			Username: planned.Username, ThunderUserID: created.ID,
			RoleName: primaryRole(planned), Email: created.Email,
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
