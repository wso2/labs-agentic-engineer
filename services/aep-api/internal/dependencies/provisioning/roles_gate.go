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

package provisioning

// roles_gate.go — the build-time roles gate.
//
// A project whose design declares roles gets one `provision` gate per version,
// titled "Provision roles and test users", alongside the per-dependency gates.
// The ensure that resolves it runs synchronously in this same call, so the gate
// is minted, published to and closed in one pass on the happy path.
//
// **The ticket is also the credential channel.** Before closing, the gate posts a
// comment carrying every test account's username and password: that comment is
// where the validation agent reads the login it signs in with
// (skills/aep-validation, ADR-0022). Two rules follow, and both are enforced
// below. A ticket that cannot be filed, or a comment that cannot be posted,
// FAILS THE BUILD — an account nothing can read is the silent degradation this
// gate exists to prevent. And a password reaches the issue comment and nothing
// else, so every log line and failure reason on these paths is redacted.
//
// **Why its own gate, rather than folding into the `thunder-app` dependency
// gate.** Build preflight deliberately skips a dependency whose OpenChoreo
// binding is already Ready, so on every rebuild `thunder-app` carries no input,
// `provisionResource` is never called, and `settleReadyGates` re-authors
// nothing. A role added in v2 would then never be created. The roles gate is
// therefore driven by the DESIGN at the tag, not by the drawer inputs — it is
// minted on every build that declares roles, whether or not anything else needs
// provisioning.
//
// **What failure does.** Nothing a coding agent does depends on these existing:
// it writes role-matching code from `security.json`, not from live directory state.
// The gate earns its keep when the ensure FAILS — if the IdP is down and the
// roles never appear, a full coding → build → deploy → validate cycle would end
// in a meaningless verdict, because validation cannot sign in.
//
// So a failure here is reported as a ProvisionFailure, and the caller collapses
// the failure list into one error (`app/build_adapters.go`): the run SETTLES as
// failed at planning rather than waiting on a hold. That is stronger than a
// dispatch hold, not weaker — nothing downstream starts at all. The gate issue
// is left OPEN as the durable record of why, carrying the error in its body, and
// the next build re-runs the ensure — it is idempotent — and closes it.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// rolesGate names this gate. It rides the `aep:gate/` prefix, not `aep:dep/`,
// so it can never be confused with a gate for a design dependency — including
// the unlikely one literally named `roles`. gateDepFromLabels reads nothing off
// it, which is exactly right: it is not keyed to a dependency.
const rolesGate = "roles"

// rolesGateTitle is the gate's issue title.
const rolesGateTitle = "Provision roles and test users"

// ensureRolesGate mints the roles gate and resolves it.
//
// Order: ASK, MINT, WORK, PUBLISH, CLOSE.
//
//  1. ask whether the design at the tag declares roles at all;
//  2. if it does, mint the gate OPEN;
//  3. run the ensure;
//  4. publish the logins it settled;
//  5. close the gate with what it did, or leave it open carrying the failure.
//
// The gate is minted BEFORE the work, not after, so it behaves like every other
// provisioning gate: a ticket you watch appear and then close. Minting after
// would only ever show a gate that was already resolved, which reads as noise
// rather than as the platform doing something.
//
// A read failure at step 1 is a FAILURE, never "this project has no roles".
// Conflating the two would let a build provision nothing, publish nothing and
// file no ticket while reporting success.
func (s *Service) ensureRolesGate(ctx context.Context, orgID, projectID, tag string, milestoneNumber int) *ProvisionFailure {
	if s.roles == nil || !s.roles.Enabled() {
		return nil
	}

	declared, err := s.roles.DeclaresRoles(ctx, orgID, projectID, tag)
	if err != nil {
		slog.ErrorContext(ctx, "provisioning: could not read the design to see whether it declares roles",
			"project", projectID, "tag", tag, "error", err)
		return &ProvisionFailure{Dependency: rolesGate, Reason: err.Error()}
	}
	if !declared {
		// The design genuinely carries no roles document — nothing to provision,
		// and no gate to mint. This is the ONLY silent path.
		return nil
	}

	// The agent finds this ticket by label WITHIN ITS OWN MILESTONE, so a gate
	// filed without one is a gate it cannot find — and unfiltered, its query
	// would answer with another version's ticket and another version's
	// passwords. Every run that reaches a build has a milestone (the validation
	// issue refuses to be filed without one), so this is a wiring fault, and
	// failing loudly beats publishing into an unfindable place.
	if milestoneNumber <= 0 {
		slog.ErrorContext(ctx, "provisioning: no milestone for the roles gate — the validation agent finds it BY milestone",
			"project", projectID, "tag", tag)
		return &ProvisionFailure{
			Dependency: rolesGate,
			Reason:     "no milestone for the roles gate ticket, which is where this version's test user logins are published",
		}
	}

	number, minted := s.mintRolesGate(ctx, orgID, projectID, tag, milestoneNumber)
	if !minted || number <= 0 {
		// The ticket is not bookkeeping any more: it is the channel the test
		// users' logins are delivered on. Carrying on without one would provision
		// accounts nothing can read and send validation into a run it cannot sign
		// in for — the silent degradation this gate exists to make impossible.
		slog.ErrorContext(ctx, "provisioning: roles gate could not be filed — nothing can publish the test user logins",
			"project", projectID, "tag", tag)
		return &ProvisionFailure{
			Dependency: rolesGate,
			Reason:     "could not file the roles gate ticket, which is where this version's test user logins are published",
		}
	}

	outcome, err := s.roles.EnsureRolesForBuild(ctx, orgID, projectID, tag)
	if err != nil {
		slog.ErrorContext(ctx, "provisioning: roles ensure failed — gate left open, run will settle",
			"project", projectID, "tag", tag, "gate", number, "error", err)
		s.commentGateFailure(ctx, orgID, projectID, number, err)
		return &ProvisionFailure{Dependency: rolesGate, Reason: err.Error()}
	}

	if perr := s.publishTestUserLogins(ctx, orgID, projectID, number, outcome.Credentials); perr != nil {
		return perr
	}

	if cerr := s.issues.CloseIssue(ctx, orgID, projectID, number, rolesGateClosingComment(outcome)); cerr != nil {
		// The accounts exist and their logins are published; only the close
		// failed. Leaving the gate open holds the next dispatch, and the next
		// build's ensure — idempotent — closes it. Failing the build here would
		// fail one whose credentials are already readable.
		slog.WarnContext(ctx, "provisioning: close roles gate failed",
			"project", projectID, "gate", number, "error", cerr)
	}
	if outcome.Refusals {
		slog.WarnContext(ctx, "provisioning: roles ensure refused something a human must look at",
			"project", projectID, "tag", tag, "summary", outcome.Summary)
	}
	return nil
}

// existingRolesGate finds this version's roles gate in ANY state, or 0.
//
// ANY state is the whole point: an OPEN one would have been caught by the
// DedupeKey already, and the case that needs catching is the one an earlier
// attempt of the same activity filed and then closed.
//
// A read failure answers 0 and the caller mints. That direction is deliberate:
// a duplicate ticket is noise a human can see and close, while wrongly believing
// a ticket exists would leave the accounts provisioned and their logins published
// nowhere — the silent degradation this gate exists to prevent.
func (s *Service) existingRolesGate(ctx context.Context, orgID, projectID, tag string) int {
	want := PlatformGateLabelPrefix + depSlug(rolesGate)
	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{delivery.KindProvision})
	if err != nil {
		slog.WarnContext(ctx, "provisioning: list issues for the roles gate failed — minting",
			"project", projectID, "tag", tag, "error", err)
		return 0
	}
	for _, issue := range issues {
		if !gateIsForVersion(issue.Labels, tag) {
			continue
		}
		if delivery.HasLabel(issue.Labels, want) {
			return issue.Number
		}
	}
	return 0
}

// commentGateFailure records why the ensure could not finish, on the gate that
// is now holding the run.
func (s *Service) commentGateFailure(ctx context.Context, orgID, projectID string, number int, cause error) {
	body := "**Could not provision the roles and test users, so this build failed at planning.**\n\n" +
		"Nothing was dispatched: without these accounts validation cannot sign in, and would " +
		"return a verdict that means nothing. This gate stays open as the record; the next " +
		"build re-runs the same step and closes it.\n\n" +
		fmt.Sprintf("```\n%s\n```\n", cause.Error())
	if err := s.issues.CommentIssue(ctx, orgID, projectID, number, body); err != nil {
		slog.WarnContext(ctx, "provisioning: comment roles gate failure",
			"project", projectID, "gate", number, "error", err)
	}
}

// mintRolesGate creates the gate issue, deduped per (project, tag). It returns
// the issue number and whether this call minted it; a create failure is logged
// and reported as not-minted, because the ensure itself has already run and a
// missing audit issue must not fail a build.
//
// "Deduped per (project, tag)" used to rest on the DedupeKey alone, and that was
// not enough. The key is resolved host-side against OPEN issues, and this gate is
// CLOSED by the same call that files it as soon as the accounts are provisioned
// — so a retried ProvisionGates filed a fresh one every attempt. That is worse
// here than for a dependency gate: this ticket is the channel the test users'
// logins are published on, so each duplicate republished a set of passwords into
// a new issue.
//
// The lookup below is the fix, and it looks for a gate in ANY state carrying this
// version's label. Finding one it REUSES the number rather than filing again.
func (s *Service) mintRolesGate(ctx context.Context, orgID, projectID, tag string, milestoneNumber int) (int, bool) {
	if n := s.existingRolesGate(ctx, orgID, projectID, tag); n > 0 {
		// Not minted BY THIS CALL, but the caller's question is "is there a ticket
		// to publish onto", so this answers yes with the one that exists.
		return n, true
	}
	req := sourcecontrol.CreateIssueRequest{
		Title: rolesGateTitle,
		Body:  rolesGatePendingBody(),
		// The version rides a label as well as the dedupe key, so the lookup above
		// can see a gate an earlier attempt filed and closed.
		Labels: withGateVersion(platformGateLabels(rolesGate), tag),
		// Same shape as a dependency gate's key, keyed on the gate's name rather
		// than a dependency's: one roles gate per version, idempotent across a
		// crashed re-run.
		DedupeKey: "gate:" + projectID + ":" + tag + ":" + rolesGate,
	}
	// Always set: ensureRolesGate refuses a build with no milestone, because a
	// gate outside one is a gate the validation agent's query cannot find.
	n := milestoneNumber
	req.Milestone = &n
	res, err := s.issues.CreateIssue(ctx, orgID, projectID, req)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: create roles gate failed",
			"project", projectID, "tag", tag, "error", err)
		return 0, false
	}
	if res == nil {
		return 0, false
	}

	// Re-assert the labels, because CreateIssue's label pre-creation is
	// deliberately best-effort and GitHub SILENTLY DROPS a label that does not
	// exist yet. A gate filed without `aep:gate/roles` is the worst shape this
	// feature has: the accounts are provisioned, the logins are published, the
	// build reports success — and the validation agent's label query cannot find
	// the ticket, so it concludes the design declares no roles. AddLabels is
	// idempotent, so paying for it on every mint is cheaper than a class of
	// silent failure. A failure here is logged and tolerated: the labels the
	// create already carried are the common case, and re-asserting is the belt.
	if lerr := s.issues.AddLabels(ctx, orgID, projectID, res.Number,
		withGateVersion(platformGateLabels(rolesGate), tag)); lerr != nil {
		slog.WarnContext(ctx, "provisioning: could not re-assert the roles gate labels — the validation agent finds this ticket BY LABEL",
			"project", projectID, "gate", res.Number, "error", lerr)
	}
	return res.Number, true
}

// rolesGatePendingBody is the gate's prose when it is minted, before the work
// runs — like every other gate, it says what is about to happen.
func rolesGatePendingBody() string {
	return "The roles and test users this version's design declares are created on the " +
		"platform identity provider before validation runs, so a role-gated acceptance " +
		"criterion is judged against a real sign-in.\n\n" +
		"The platform resolves this gate itself — no agent works it. Roles and test users " +
		"are SHARED across projects: a role another project already uses is reused rather " +
		"than duplicated, and one the platform did not create is left untouched.\n\n" +
		"When this gate closes it posts each test user's login as a comment — that comment is " +
		"where the validation agent reads the credentials it signs in with. The same passwords " +
		"are readable from the project's **Security → Roles & users** panel."
}

// rolesGateClosingComment is what the gate closes with: exactly what the ensure
// did, so the milestone carries a durable record. The LOGINS are not here — they
// go up as their own comment first (publishTestUserLogins).
func rolesGateClosingComment(outcome RolesEnsureOutcome) string {
	var b strings.Builder
	b.WriteString("Roles and test users provisioned.\n\n")
	b.WriteString(outcome.Summary)
	if outcome.Refusals {
		b.WriteString("\n\nA refusal is not a failure — the build continues — but it needs a " +
			"human. The platform modifies only accounts it created, so a username that " +
			"already belongs to somebody else is left untouched rather than adopted and " +
			"password-reset. Rename it in `specs/design/security.json`, or let the platform " +
			"supply the name.")
	}
	return b.String()
}

// publishTestUserLogins posts the login table as its own comment, before the
// gate closes.
//
// Its own comment, rather than a section of the closing one, for two reasons.
// The agent that reads it gets a comment whose whole content is the table, so
// nothing around it can be mistaken for a row. And a failure to publish is
// separable from a failure to close: this one is FATAL, because credentials that
// never reached the ticket mean validation cannot sign in, while a failed close
// only leaves a gate the next build closes.
//
// A project with no accounts publishes nothing and that is not a failure — every
// role it declares is one the platform does not own, which the summary says.
func (s *Service) publishTestUserLogins(ctx context.Context, orgID, projectID string, number int, creds []RolesCredential) *ProvisionFailure {
	if len(creds) == 0 {
		return nil
	}
	if err := s.issues.CommentIssue(ctx, orgID, projectID, number, renderTestUserLogins(creds)); err != nil {
		// Redacted on both paths: a client error may quote what it sent, and a
		// password must not reach a log line or a run's failure reason.
		reason := redactPasswords(err.Error(), creds)
		slog.ErrorContext(ctx, "provisioning: could not publish the test user logins — gate left open, run will settle",
			"project", projectID, "gate", number, "error", reason)
		return &ProvisionFailure{
			Dependency: rolesGate,
			Reason:     "could not publish this version's test user logins to the roles gate ticket: " + reason,
		}
	}
	return nil
}

// redactPasswords removes every published password from a string bound for a log
// or a failure reason. Cheap, and it closes the one hole this feature opens: the
// only place a password is supposed to appear is the issue comment.
func redactPasswords(msg string, creds []RolesCredential) string {
	for _, c := range creds {
		// The empty check is load-bearing, not defensive: an account whose seal
		// would not open carries an empty password, and ReplaceAll with an empty
		// needle inserts the replacement between every character — turning the
		// one message a human has to read into noise.
		if c.Password != "" {
			msg = strings.ReplaceAll(msg, c.Password, "[redacted]")
		}
	}
	return msg
}

// renderTestUserLogins renders the comment that publishes the test accounts'
// logins.
//
// ADR-0022 carries why a password is published here at all, and what bounds the
// trade. What this function owes that decision is the SHAPE the agent parses:
// the marker, then a table whose columns and cold-start values SKILL.md
// mirrors, then prose saying what these accounts are.
//
// No escaping: a username is `[a-z0-9][a-z0-9._-]*` (securityspec) and a generated
// password is drawn from an alphabet that excludes the backtick and the pipe, so
// neither can break out of its cell. The identity domain's
// TestGeneratedPasswordCarriesNoMarkdownDelimiter pins the password half.
func renderTestUserLogins(creds []RolesCredential) string {
	if len(creds) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Test user logins\n\n")
	b.WriteString(sourcecontrol.PublishedCredentialsMarker)
	b.WriteString("\n\n| Username | Password | Role | Cold start |\n| --- | --- | --- | --- |\n")
	for _, c := range creds {
		password := "`" + c.Password + "`"
		if c.Password == "" {
			// Never a blank cell: a blank reads as "this account has no
			// password", which would send an agent off to debug a login that
			// was never published in the first place.
			password = "_unavailable — read it from the Security panel_"
		}
		coldStart := "no"
		if c.ColdStart {
			coldStart = "yes"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", c.Username, password, c.Role, coldStart)
	}
	b.WriteString("\n**These are disposable test accounts for automated agents, not people.** " +
		"The validation agent signs in as one to judge a role-gated acceptance criterion, and it " +
		"reads these credentials from this comment. They hold nothing but this project's own " +
		"application roles. Never put a real person's username in " +
		"`specs/design/security.json` — the platform refuses to touch an account it did not create, " +
		"so that produces a role with no working login rather than a password reset.\n\n" +
		"The **cold start** account is the one a caller holds before anyone grants them a role; " +
		"it answers a request that names no role.")
	return b.String()
}
