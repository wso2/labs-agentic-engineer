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

// teardown.go — what a PROJECT DELETE removes from the environment's identity
// provider, and what it deliberately leaves standing.
//
// It is the other half of the ownership rule ensure.go converges to, and the
// line runs through the middle of this domain:
//
//   - The resource server, its resources and actions, the `<project>/<Role>`
//     roles and the platform's rows about them are PROJECT-OWNED. Exactly one
//     project creates them, they carry its name, and they are removed with it.
//   - Org groups and accounts are SHARED and additive (ADR-0022). A group two
//     projects named is exactly the intended reuse, and an account may be a
//     login somebody still uses. NOTHING here deletes either one — not the
//     group a role was assigned to, and not the test users the ensure minted.
//     Un-assigning a role touches the binding, never the principal.
//
// Every step is BEST-EFFORT and no step returns early. The project delete that
// calls this has other systems to tear down after it, and a directory that is
// unreachable must not strand the run supervisors, the repo row and the
// executions rows behind it. So each failure is recorded on the report, the
// next step still runs, and the caller logs the joined problems.
//
// The report matters because this is the one teardown whose leftovers are
// invisible: an orphaned resource server on a directory is not listed by
// anything this platform shows, so the log line naming it is the only trace a
// human gets.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// TeardownService removes one project's authorization objects from the identity
// provider of the environment its builds were validated in.
//
// It takes the same two ports the ensure and the catalog take, and no design
// reader: a delete reads the platform's OWN record of what it created, never
// the design, because the design is the thing that is going away. That is also
// why the directory listing is only a backstop — see Teardown.
type TeardownService struct {
	targets TargetResolver
	store   Store
}

// NewTeardownService builds the teardown. Either collaborator may be nil — the
// composition root wires the identity feature optionally, and a stack with no
// reachable identity provider has no targets — in which case Enabled() is false
// and every call is a no-op that reports nothing.
func NewTeardownService(targets TargetResolver, store Store) *TeardownService {
	return &TeardownService{targets: targets, store: store}
}

// Enabled reports whether the teardown can run at all. A nil receiver answers
// false, so the consumer port stays safe even when the composition root hands
// an unwired service across it.
func (s *TeardownService) Enabled() bool {
	return s != nil && s.targets != nil && s.store != nil
}

// TeardownReport is what one teardown did and what it could not do. It is a
// value, not an error, because "three roles gone, the resource server left
// behind" is the normal shape of a best-effort cleanup and a single error
// cannot say it.
type TeardownReport struct {
	// Scope is the (org, environment) whose identity provider was worked on. It
	// is filled in even when the directory could not be reached, because the
	// scope is pure (TargetResolver.Scope) and the store rows are keyed by it.
	Scope Scope
	// ProjectID is the project whose objects these were.
	ProjectID string
	// RolesDeleted are the directory role names (`<project>/Approver`) actually
	// removed, in the order they were removed.
	RolesDeleted []string
	// AssignmentsRemoved counts the bindings taken off those roles before they
	// were deleted. The principals — groups and accounts — are untouched.
	AssignmentsRemoved int
	// ResourceServerDeleted is the identifier of the resource server removed,
	// empty when there was none or when its delete failed. It is the `aud` of
	// every token the project's app ever minted, so it is the one name worth
	// printing.
	ResourceServerDeleted string
	// Problems are the failures, in the order they happened, each naming its
	// step. A non-empty Problems with a non-empty RolesDeleted is normal.
	Problems []error
}

// Err joins every problem into one error for a caller that only needs to know
// whether the cleanup was complete. Nil when nothing failed.
func (r TeardownReport) Err() error { return errors.Join(r.Problems...) }

// Summary renders the report for a log line: what went, and how much did not.
func (r TeardownReport) Summary() string {
	parts := []string{fmt.Sprintf("roles deleted: %d", len(r.RolesDeleted))}
	if len(r.RolesDeleted) > 0 {
		parts[0] += " (" + strings.Join(r.RolesDeleted, ", ") + ")"
	}
	parts = append(parts, fmt.Sprintf("assignments removed: %d", r.AssignmentsRemoved))
	if r.ResourceServerDeleted != "" {
		parts = append(parts, "resource server deleted: "+r.ResourceServerDeleted)
	}
	if len(r.Problems) > 0 {
		parts = append(parts, fmt.Sprintf("problems: %d", len(r.Problems)))
	}
	return strings.Join(parts, "; ")
}

// fail records one step's failure and logs it. The log is here rather than at
// the call site so a problem is never appended without being said out loud.
func (r *TeardownReport) fail(ctx context.Context, step string, err error) {
	r.Problems = append(r.Problems, fmt.Errorf("%s: %w", step, err))
	slog.ErrorContext(ctx, "identity teardown: step failed, continuing",
		"scope", r.Scope.String(), "project", r.ProjectID, "step", step, "error", err)
}

// TeardownProject is the consumer-port shape: it runs the teardown and answers
// the joined problems, so `internal/projects` can hold a one-method interface
// and never name this package's types.
//
// It is deliberately the ERROR-returning half. The detail belongs in this
// domain's own log line (Teardown writes it); what the project delete needs is
// one value to decide "log and continue", which is the only decision it makes.
func (s *TeardownService) TeardownProject(ctx context.Context, orgID, projectID string) error {
	return s.Teardown(ctx, orgID, projectID).Err()
}

// Teardown removes the project's authorization objects, in this order:
//
//  1. **Roles** — for each one, its assignments are removed and then the role
//     itself. The worklist is the platform's own `idp_role_bindings` rows PLUS,
//     as a backstop, every role on the directory whose name starts with this
//     project's prefix. The backstop exists because the rows are written after
//     the directory objects: an ensure that died between the two leaves roles
//     with no row, and the prefix is the ownership marker that makes finding
//     them safe (no other project can create a `<project>/` role).
//  2. **The resource server**, which takes its resources and actions with it —
//     the adapter owns the leaf-first order the directory demands (see
//     Directory.DeleteResourceServer). Roles are not what blocks this delete,
//     but they go first anyway so that a run which fails halfway has removed
//     the grants rather than the catalog they point at.
//  3. **The platform's rows** — the bindings and the resource-server row.
//
// Step 3 runs even when step 1 or 2 failed, and even when the directory could
// not be resolved at all. The rows are keyed to a project that is being deleted:
// leaving them would let a project recreated under the same name adopt stale
// directory ids, which is a worse failure than an orphaned directory object that
// the report has named.
//
// Groups and users appear nowhere in the above. That is the invariant, not an
// omission — see the file header and README.md.
func (s *TeardownService) Teardown(ctx context.Context, orgID, projectID string) TeardownReport {
	report := TeardownReport{ProjectID: projectID}
	if !s.Enabled() {
		return report
	}
	// Scope is pure and cannot fail, so the rows are addressable even when the
	// directory is not. That split is the whole reason TargetResolver has two
	// methods.
	report.Scope = s.targets.Scope(orgID)

	if target, err := s.targets.Resolve(ctx, orgID); err != nil {
		// No directory: the objects stand, and the report says so. The rows
		// below still go.
		report.fail(ctx, "resolve the environment's identity provider", err)
	} else {
		s.teardownDirectory(ctx, target, projectID, &report)
	}

	if err := s.store.DeleteRoleBindings(ctx, report.Scope, projectID); err != nil {
		report.fail(ctx, "forget the project's role bindings", err)
	}
	if err := s.store.DeleteResourceServer(ctx, report.Scope, projectID); err != nil {
		report.fail(ctx, "forget the project's resource server row", err)
	}

	slog.InfoContext(ctx, "identity teardown complete",
		"scope", report.Scope.String(), "project", projectID, "result", report.Summary())
	return report
}

// teardownDirectory is steps 1 and 2 — everything that touches the identity
// provider. Split out so the store-row cleanup above cannot accidentally end up
// inside a branch that the directory's availability decides.
func (s *TeardownService) teardownDirectory(ctx context.Context, target Target, projectID string, report *TeardownReport) {
	dir := target.Directory
	for _, role := range s.roleWorklist(ctx, target, projectID, report) {
		s.deleteRole(ctx, dir, role, report)
	}
	rs, identifier, found := s.resourceServer(ctx, target, projectID, report)
	if !found {
		return
	}
	if err := dir.DeleteResourceServer(ctx, rs); err != nil {
		report.fail(ctx, fmt.Sprintf("delete resource server %q", identifier), err)
		return
	}
	report.ResourceServerDeleted = identifier
}

// roleWorklist is every role this project owns on the directory, from the two
// sources that can know: the platform's bindings, then the directory's own
// listing filtered by the project prefix.
//
// The rows come first because they are authoritative about what the platform
// created, and the listing second because it catches what the rows missed. A
// role reached by both is worked once — it is keyed by directory id, since one
// role assigned to three groups is three rows and one object.
//
// Either source failing is recorded and the other still runs: a store read that
// fails must not save a directory role from deletion, and vice versa.
func (s *TeardownService) roleWorklist(ctx context.Context, target Target, projectID string, report *TeardownReport) []RoleRef {
	var out []RoleRef
	seen := map[DirectoryID]bool{}
	add := func(ref RoleRef) {
		if ref.ID == "" || seen[ref.ID] {
			return
		}
		seen[ref.ID] = true
		out = append(out, ref)
	}

	bindings, err := s.store.ListRoleBindings(ctx, target.Scope(), projectID)
	if err != nil {
		report.fail(ctx, "read the project's role bindings", err)
	}
	for _, b := range bindings {
		// The row carries the design's role name (`Approver`); the directory
		// carries the prefixed one. Deriving it here keeps both sources naming
		// the same string in the report.
		add(RoleRef{ID: DirectoryID(b.DirectoryRoleID), Name: RoleName(projectID, b.Role)})
	}

	listed, err := target.Directory.ListRoles(ctx)
	if err != nil {
		report.fail(ctx, "list the directory's roles", err)
		return out
	}
	prefix := RoleNamePrefix(projectID)
	backstop := make([]RoleRef, 0, len(listed))
	for _, ref := range listed {
		if strings.HasPrefix(strings.ToLower(ref.Name), prefix) {
			backstop = append(backstop, ref)
		}
	}
	// Name order, so a teardown of the same directory twice does the same thing
	// in the same sequence and a failure reads the same way.
	sort.Slice(backstop, func(i, j int) bool { return backstop[i].Name < backstop[j].Name })
	for _, ref := range backstop {
		add(ref)
	}
	return out
}

// deleteRole takes one role off the directory: its assignments first, then the
// role.
//
// The unassign pass is not bookkeeping the directory needs — deleting a role
// takes its assignments with it — it is what makes the removal VISIBLE as a
// removal of grants rather than a disappearance, and it is the step that proves
// the principals are left alone: the only write aimed at a group here is
// UnassignRole, which never touches the group itself.
//
// A failed listing does not skip the delete. A role that cannot be read is
// still a role holding permissions on a project that no longer exists.
func (s *TeardownService) deleteRole(ctx context.Context, dir Directory, role RoleRef, report *TeardownReport) {
	principals, err := dir.ListRoleAssignments(ctx, role.ID)
	if err != nil {
		report.fail(ctx, fmt.Sprintf("list the assignments of role %q", role.Name), err)
	}
	for _, principal := range principals {
		if err := dir.UnassignRole(ctx, role.ID, principal); err != nil {
			report.fail(ctx, fmt.Sprintf("unassign %s from role %q",
				principalName(principal), role.Name), err)
			continue
		}
		report.AssignmentsRemoved++
	}
	if err := dir.DeleteRole(ctx, role.ID); err != nil {
		report.fail(ctx, fmt.Sprintf("delete role %q", role.Name), err)
		return
	}
	report.RolesDeleted = append(report.RolesDeleted, role.Name)
}

// principalName is how a principal reads in a problem — its display name when
// the directory supplied one, its id otherwise. An assignment can outlive the
// group id it names, so the id alone is often unresolvable by the time a human
// reads the log.
func principalName(p Principal) string {
	if p.Display != "" {
		return string(p.Kind) + " " + p.Display
	}
	return string(p.Kind) + " " + string(p.ID)
}

// resourceServer answers which resource server to delete, from the row if there
// is one and from the derived identifier if there is not.
//
// The backstop matters for the same reason the role one does, and for a NARROWER
// window: the ensure creates the resource server before it writes the row, so a
// pass that died between the two leaves the object with no record of it at all —
// and the identifier, being derived from (org, project), can belong to no other
// project.
//
// It asks the directory through FindResourceServer, which is a READ. The Ensure
// verb would answer the same question by creating one when it is absent, so a
// project that never built would have a resource server minted purely so the
// next line could delete it again. A teardown that writes to reach the thing it
// is removing is a teardown that can leave a project worse than it found it.
//
// Absent, and with no row, there is simply nothing to delete: the caller skips
// the step rather than reporting a problem. If the delete that follows a FOUND
// one fails, the report names the identifier that was left behind.
func (s *TeardownService) resourceServer(ctx context.Context, target Target, projectID string, report *TeardownReport) (DirectoryID, string, bool) {
	row, err := s.store.GetResourceServer(ctx, target.Scope(), projectID)
	if err != nil {
		report.fail(ctx, "read the project's resource-server row", err)
	}
	if row != nil {
		return DirectoryID(row.DirectoryID), row.Identifier, true
	}
	identifier := ResourceServerIdentifier(target.OrgID, projectID)
	id, found, err := target.Directory.FindResourceServer(ctx, identifier)
	if err != nil {
		report.fail(ctx, fmt.Sprintf("find resource server %q", identifier), err)
		return "", "", false
	}
	if !found {
		return "", "", false
	}
	return id, identifier, true
}
