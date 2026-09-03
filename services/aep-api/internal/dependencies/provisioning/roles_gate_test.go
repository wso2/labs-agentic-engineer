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

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeRolesEnsurer records what the gate asked it to do.
type fakeRolesEnsurer struct {
	declared    bool
	declaresErr error
	outcome     RolesEnsureOutcome
	ensureErr   error

	declaresCalls int
	ensureCalls   int
}

func (f *fakeRolesEnsurer) Enabled() bool { return true }

func (f *fakeRolesEnsurer) DeclaresRoles(context.Context, string, string, string) (bool, error) {
	f.declaresCalls++
	return f.declared, f.declaresErr
}

func (f *fakeRolesEnsurer) EnsureRolesForBuild(context.Context, string, string, string) (RolesEnsureOutcome, error) {
	f.ensureCalls++
	return f.outcome, f.ensureErr
}

// newRolesGateService wires just the two collaborators the gate touches, over
// the package's existing issue fake.
func newRolesGateService(roles RolesEnsurer, issues *fakeIssues) *Service {
	return NewService(Deps{Issues: issues, Roles: roles})
}

// allComments flattens every comment the fake recorded, in no particular order:
// the gate posts at most one per ticket, so a test asserting on "the comments"
// does not need to know the issue number.
func allComments(issues *fakeIssues) []string {
	var out []string
	for _, bodies := range issues.comments {
		out = append(out, bodies...)
	}
	return out
}

// A design that declares roles gets an OPEN ticket first, then the work, then a
// close. Minting before the work is what makes it behave like the dependency
// gates beside it — a ticket you watch open and then close, rather than one that
// appears already resolved.
func TestRolesGate_MintsOpenThenClosesWithTheOutcome(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome: RolesEnsureOutcome{
			Summary: "- Roles created: Trainer, Team Member",
		},
	}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}

	if roles.declaresCalls != 1 || roles.ensureCalls != 1 {
		t.Fatalf("declares=%d ensure=%d, want 1 each", roles.declaresCalls, roles.ensureCalls)
	}
	if len(issues.created) != 1 {
		t.Fatalf("created %d gates, want 1", len(issues.created))
	}
	got := issues.created[0]
	if got.Title != rolesGateTitle {
		t.Errorf("title = %q, want %q", got.Title, rolesGateTitle)
	}
	if got.DedupeKey != "gate:workouts:v1:roles" {
		t.Errorf("dedupe key = %q", got.DedupeKey)
	}
	if got.Milestone == nil || *got.Milestone != 7 {
		t.Errorf("milestone = %v, want 7 at creation", got.Milestone)
	}
	// The body describes what is ABOUT to happen, not the outcome — it is written
	// before the work runs.
	if strings.Contains(got.Body, "Roles created") {
		t.Errorf("the minted body must not carry the outcome: %q", got.Body)
	}
	if !strings.Contains(got.Body, "Security") {
		t.Errorf("the body should point at where a password is revealed: %q", got.Body)
	}
	gateNum := issues.created[0]
	_ = gateNum
	if len(issues.closed) != 1 {
		t.Fatalf("closed = %v, want exactly the gate it minted", issues.closed)
	}
	var closingComment string
	for _, c := range issues.closed {
		closingComment = c
	}
	if !strings.Contains(closingComment, "Roles created: Trainer, Team Member") {
		t.Errorf("closing comment lost the summary: %q", closingComment)
	}
}

// A RETRIED ProvisionGates MUST NOT FILE A SECOND ROLES TICKET, and this gate is
// the one where a duplicate does real harm.
//
// The gate is closed by the same call that files it, as soon as the accounts are
// provisioned — so its DedupeKey, which the host resolves against OPEN issues
// only, could not see the one the previous attempt left behind. The live incident
// filed eleven, and because the ticket is the channel the test users' logins are
// published on, each one republished a set of passwords into a new issue.
func TestRolesGate_ARetryReusesTheTicketItAlreadyClosed(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: true, outcome: RolesEnsureOutcome{Summary: "- Roles created: Trainer"}}
	issues := newFakeIssues(nil)
	svc := newRolesGateService(roles, issues)

	if f := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("first pass: %+v", f)
	}
	if len(issues.created) != 1 {
		t.Fatalf("setup: created %d gates, want 1", len(issues.created))
	}
	first := issues.list[0].Number
	if !strings.EqualFold(issues.list[0].State, "closed") {
		t.Fatalf("setup: the gate should have been closed by its own pass, state=%q", issues.list[0].State)
	}

	// The retry.
	issues.created = nil
	if f := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("retry: %+v", f)
	}
	if len(issues.created) != 0 {
		t.Fatalf("a retry filed %d more roles tickets — each one republishes the test users' passwords",
			len(issues.created))
	}
	if len(issues.list) != 1 || issues.list[0].Number != first {
		t.Errorf("the retry must reuse the ticket it already has, issues=%+v", issues.list)
	}
}

// The next VERSION still gets its own ticket. Its logins are its own — reusing
// v1's would publish v2's passwords onto a closed ticket for a version nobody is
// building, where the validation agent's milestone-scoped query cannot find them.
func TestRolesGate_ANewVersionFilesItsOwnTicket(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: true, outcome: RolesEnsureOutcome{}}
	issues := newFakeIssues(nil)
	svc := newRolesGateService(roles, issues)

	if f := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("v1: %+v", f)
	}
	issues.created = nil
	if f := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v2", 8); f != nil {
		t.Fatalf("v2: %+v", f)
	}
	if len(issues.created) != 1 {
		t.Fatalf("v2 must file its own roles ticket, filed %d", len(issues.created))
	}
	if got := issues.created[0].DedupeKey; got != "gate:workouts:v2:roles" {
		t.Errorf("dedupe key = %q, want v2's", got)
	}
	if m := issues.created[0].Milestone; m == nil || *m != 8 {
		t.Errorf("milestone = %v, want v2's (8) — the agent finds this ticket BY milestone", m)
	}
}

// A refusal closing comment names the live security document so a human can
// rename the colliding username where they authored it.
func TestRolesGate_RefusalClosingCommentNamesSecurityJSON(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome: RolesEnsureOutcome{
			Summary:  "- Test users refused: jsmith",
			Refusals: true,
		},
	}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	var closing string
	for _, c := range issues.closed {
		closing = c
	}
	if !strings.Contains(closing, "specs/design/security.json") {
		t.Fatalf("refusal copy should name security.json: %q", closing)
	}
}

// A build with no milestone must fail rather than file the ticket outside one:
// the agent's lookup is label-within-milestone, so an unmilestoned gate is
// invisible to it, and an unfiltered query answers with another version's
// passwords.
func TestRolesGate_NoMilestoneFailsRatherThanFilingAnUnfindableTicket(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: true}
	issues := newFakeIssues(nil)

	failure := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 0)

	if failure == nil {
		t.Fatal("a gate with no milestone must fail the build")
	}
	if !strings.Contains(failure.Reason, "milestone") {
		t.Errorf("reason = %q, want it to name the milestone", failure.Reason)
	}
	if len(issues.created) != 0 {
		t.Errorf("a ticket was filed anyway: %+v", issues.created)
	}
	if roles.ensureCalls != 0 {
		t.Errorf("the ensure ran %d times with nowhere to publish", roles.ensureCalls)
	}
}

// ---- the published logins ---------------------------------------------------
//
// The closing comment is the delivery mechanism for the test accounts'
// credentials: the validation agent finds this ticket by label within its own
// milestone and reads the table out of it. So the table's shape is a contract
// with that agent, not formatting.

func TestRolesGate_PublishesEveryLoginInItsOwnComment(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome: RolesEnsureOutcome{
			Summary: "- Roles created: Trainer, Team Member",
			Credentials: []RolesCredential{
				{Username: "test-team-member", Password: "Aep1!alpha-beta_1", Role: "Team Member", ColdStart: true},
				{Username: "test-trainer", Password: "Aep1!gamma-delta_2", Role: "Trainer"},
			},
		},
	}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}

	// The MINTED body must carry nothing: it is written before the accounts
	// exist, and a password in it would be published even on the failure path.
	for _, created := range issues.created {
		if strings.Contains(created.Body, "Aep1!") {
			t.Fatalf("the minted body carries a password: %q", created.Body)
		}
	}

	// One comment, whose whole content is the table — so nothing around it can
	// be mistaken for a row by the agent parsing it.
	comments := allComments(issues)
	if len(comments) != 1 {
		t.Fatalf("comments = %v, want exactly the login table", comments)
	}
	comment := comments[0]
	if !strings.Contains(comment, sourcecontrol.PublishedCredentialsMarker) {
		t.Errorf("the published comment lacks the anchor the agent looks for:\n%s", comment)
	}
	for _, want := range []string{
		"| `test-team-member` | `Aep1!alpha-beta_1` | Team Member | yes |",
		"| `test-trainer` | `Aep1!gamma-delta_2` | Trainer | no |",
	} {
		if !strings.Contains(comment, want) {
			t.Errorf("published comment is missing row\n  %s\ngot:\n%s", want, comment)
		}
	}
	// It must also say what these accounts are, so nobody reads the table as a
	// list of real people's logins.
	if !strings.Contains(comment, "disposable test accounts") {
		t.Errorf("credentials were published with no warning:\n%s", comment)
	}
	if !strings.Contains(comment, "specs/design/security.json") {
		t.Errorf("published copy should name security.json:\n%s", comment)
	}
	// Published BEFORE the close, and the close itself carries no password.
	if len(issues.closed) != 1 {
		t.Fatalf("closed = %v, want the gate", issues.closed)
	}
	for _, closing := range issues.closed {
		if strings.Contains(closing, "Aep1!") {
			t.Errorf("the closing comment repeats a password:\n%s", closing)
		}
	}
}

// A password the platform could not open must not render as an empty cell: a
// blank reads as "this account has no password" and sends whoever reads it off
// to debug a login that was never published.
func TestRolesGate_AnUnavailablePasswordSaysSo(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome: RolesEnsureOutcome{
			Summary:     "- Test users reused: test-trainer",
			Credentials: []RolesCredential{{Username: "test-trainer", Role: "Trainer"}},
		},
	}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}

	comments := allComments(issues)
	if len(comments) != 1 {
		t.Fatalf("comments = %v, want the login table", comments)
	}
	if !strings.Contains(comments[0], "| `test-trainer` | _unavailable") {
		t.Errorf("an unopenable seal rendered as a blank cell:\n%s", comments[0])
	}
	if strings.Contains(comments[0], "| `` |") {
		t.Errorf("empty backticked cell in:\n%s", comments[0])
	}
}

// A project with no accounts gets no table and no warning — an empty section
// under a "Test user logins" heading reads as a bug.
func TestRolesGate_NoCredentialsMeansNoLoginSection(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome:  RolesEnsureOutcome{Summary: "- Roles left alone: Administrators"},
	}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	if c := allComments(issues); len(c) != 0 {
		t.Errorf("an empty login section was published: %v", c)
	}
	for _, closing := range issues.closed {
		if strings.Contains(closing, sourcecontrol.PublishedCredentialsMarker) {
			t.Errorf("an empty login table was published:\n%s", closing)
		}
	}
}

// A failure must not publish. The gate's failure comment carries the cause, and
// the credentials the ensure may have collected before it failed stay out of it.
func TestRolesGate_AFailureCommentCarriesNoLogin(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared:  true,
		outcome:   RolesEnsureOutcome{Credentials: []RolesCredential{{Username: "test-trainer", Password: "Aep1!secret"}}},
		ensureErr: errors.New("thunder is unreachable"),
	}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f == nil {
		t.Fatal("a failed ensure must fail the build")
	}
	for _, bodies := range issues.comments {
		for _, body := range bodies {
			if strings.Contains(body, "Aep1!secret") {
				t.Fatalf("the failure comment published a password: %q", body)
			}
		}
	}
	if len(issues.closed) != 0 {
		t.Fatalf("a failed ensure closed the gate: %v", issues.closed)
	}
}

// failingComments is a gate issue surface whose COMMENT call fails. Wrapping
// rather than extending the package's shared fake keeps the injection local to
// the two tests that need it.
type failingComments struct {
	*fakeIssues
	err error
}

func (f failingComments) CommentIssue(context.Context, string, string, int, string) error {
	return f.err
}

// refuseCreate is a gate issue surface that cannot file an issue at all.
type refuseCreate struct {
	*fakeIssues
	err error
}

func (f refuseCreate) CreateIssue(context.Context, string, string, sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	return nil, f.err
}

// Credentials that never reached the ticket mean validation cannot sign in, so
// a publication failure FAILS THE BUILD and leaves the gate open — the same
// treatment as a failed ensure, for the same reason. Anything softer is the
// silent degradation this gate exists to make impossible.
func TestRolesGate_APublicationFailureFailsTheBuildAndLeavesTheGateOpen(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome: RolesEnsureOutcome{
			Summary:     "- Test users created: test-trainer",
			Credentials: []RolesCredential{{Username: "test-trainer", Password: "Aep1!secret-value", Role: "Trainer"}},
		},
	}
	issues := newFakeIssues(nil)
	svc := NewService(Deps{Issues: failingComments{fakeIssues: issues, err: errors.New("github: 502")}, Roles: roles})

	failure := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7)
	if failure == nil {
		t.Fatal("logins that were never published must fail the build")
	}
	if !strings.Contains(failure.Reason, "test user logins") {
		t.Errorf("reason = %q, want it to name what could not be published", failure.Reason)
	}
	if len(issues.closed) != 0 {
		t.Errorf("the gate was closed with no logins published: %v", issues.closed)
	}
}

// The failure reason reaches a run record and the logs. A password must not ride
// along in it, however the client below phrased its error.
func TestRolesGate_APublicationFailureReasonCarriesNoPassword(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared: true,
		outcome: RolesEnsureOutcome{
			Credentials: []RolesCredential{{Username: "test-trainer", Password: "Aep1!secret-value", Role: "Trainer"}},
		},
	}
	// A second account whose seal would not open: its empty password must not
	// turn the redaction into a per-character rewrite of the whole message.
	roles.outcome.Credentials = append(roles.outcome.Credentials,
		RolesCredential{Username: "test-viewer", Role: "Viewer"})
	// A client that quotes what it sent — exactly the case redaction is for.
	chatty := errors.New(`POST /issues/7/comments failed: body="| test-trainer | Aep1!secret-value |"`)
	svc := NewService(Deps{
		Issues: failingComments{fakeIssues: newFakeIssues(nil), err: chatty},
		Roles:  roles,
	})

	failure := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7)
	if failure == nil {
		t.Fatal("expected a failure")
	}
	if strings.Contains(failure.Reason, "Aep1!secret-value") {
		t.Fatalf("the failure reason leaked a password: %q", failure.Reason)
	}
	if !strings.Contains(failure.Reason, "[redacted]") {
		t.Errorf("reason = %q, want the password replaced rather than the message dropped", failure.Reason)
	}
	// Still readable: an empty password must be skipped, not treated as a
	// substring that matches between every character.
	if !strings.Contains(failure.Reason, "POST /issues/7/comments failed") {
		t.Errorf("reason = %q, want the client's own error still legible", failure.Reason)
	}
}

// The ticket is the delivery mechanism, so a design that declares roles and
// cannot get one filed fails rather than provisioning accounts nothing can read.
func TestRolesGate_AnUnfilableTicketFailsTheBuild(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: true, outcome: RolesEnsureOutcome{}}
	svc := NewService(Deps{
		Issues: refuseCreate{fakeIssues: newFakeIssues(nil), err: errors.New("github: 403")},
		Roles:  roles,
	})

	failure := svc.ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7)
	if failure == nil {
		t.Fatal("a gate that could not be filed must fail the build — nothing could publish the logins")
	}
	if roles.ensureCalls != 0 {
		t.Errorf("the ensure ran %d times with no ticket to publish into", roles.ensureCalls)
	}
}

// The label and the marker are HALF A CONTRACT: the other half is a `gh issue
// list --label` query and a table scan in skills/aep-validation/SKILL.md, which
// no Go test can execute. So they are asserted as LITERALS here. Asserted
// against their own constants they can never fail, and renaming either one
// silently breaks credential delivery for every project with every test green.
func TestRolesGate_TheAgentsHandlesAreLiterals(t *testing.T) {
	labels := platformGateLabels(rolesGate)
	if len(labels) != 2 || labels[1] != "aep:gate/roles" {
		t.Errorf("gate labels = %v, want the literal aep:gate/roles that SKILL.md filters on", labels)
	}
	if sourcecontrol.PublishedCredentialsMarker != "<!-- aep:test-users -->" {
		t.Errorf("marker = %q, want the literal SKILL.md scans for", sourcecontrol.PublishedCredentialsMarker)
	}
	if rolesGateTitle != "Provision roles and test users" {
		t.Errorf("title = %q — SKILL.md names it, and CONTEXT.md's lexicon pins it", rolesGateTitle)
	}
}

// The gate is filed with its labels re-asserted, because GitHub silently drops a
// label that does not exist yet and a gate the agent's label query cannot find
// is indistinguishable from a project with no roles.
func TestRolesGate_LabelsAreReassertedAfterFiling(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: true, outcome: RolesEnsureOutcome{}}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	var found bool
	for _, iss := range issues.list {
		for _, l := range iss.Labels {
			if l == "aep:gate/roles" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("the gate carries no aep:gate/roles label: %+v", issues.list)
	}
}

// THE REGRESSION THIS FILE EXISTS FOR.
//
// A build wired the ensure to a reader that refuses a `v<N>` spec tag. The read
// failed, the ensure reported "not declared", and the gate treated that as "this
// project has no roles" — so the build carried on with no roles, no test users
// and no ticket, and said nothing louder than a warning. A read failure is a
// FAILURE.
func TestRolesGate_AReadFailureFailsTheBuildRatherThanSkipping(t *testing.T) {
	roles := &fakeRolesEnsurer{declaresErr: errors.New(`invalid version tag: "v1" is not a v<N>-<M> tag`)}
	issues := newFakeIssues(nil)

	f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7)
	if f == nil {
		t.Fatal("a design that could not be read was skipped silently — the exact bug this pins")
	}
	if !strings.Contains(f.Reason, "not a v<N>-<M> tag") {
		t.Errorf("failure should carry the cause: %q", f.Reason)
	}
	if roles.ensureCalls != 0 {
		t.Errorf("the ensure ran despite an unreadable design")
	}
}

// The ONE silent path: the design really does carry no roles document. No gate,
// no failure, nothing in the milestone.
func TestRolesGate_NoRolesDocumentIsSilent(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: false}
	issues := newFakeIssues(nil)

	if f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	if len(issues.created) != 0 {
		t.Errorf("minted a gate for a project with no roles: %+v", issues.created)
	}
	if roles.ensureCalls != 0 {
		t.Errorf("ran the ensure with nothing to ensure")
	}
}

// A failed ensure leaves the gate OPEN carrying the cause, and fails the build.
func TestRolesGate_EnsureFailureLeavesTheGateOpenWithTheCause(t *testing.T) {
	roles := &fakeRolesEnsurer{
		declared:  true,
		ensureErr: errors.New("thunder is down"),
	}
	issues := newFakeIssues(nil)

	f := newRolesGateService(roles, issues).ensureRolesGate(
		context.Background(), "acme", "workouts", "v1", 7)
	if f == nil {
		t.Fatal("a failed ensure must fail the build")
	}
	if len(issues.created) != 1 {
		t.Fatalf("the gate should still be minted as the record: %+v", issues.created)
	}
	if len(issues.closed) != 0 {
		t.Errorf("the gate was closed despite the ensure failing: %v", issues.closed)
	}
	var joined string
	for _, cs := range issues.comments {
		joined += strings.Join(cs, "\n")
	}
	if !strings.Contains(joined, "thunder is down") {
		t.Errorf("the cause was not recorded on the gate: %q", joined)
	}
}

// The gate rides `aep:gate/`, never `aep:dep/`, so it can never be mistaken for
// a dependency's gate — and it carries no `aep` arming label, so nothing works it.
func TestRolesGate_LabelsAreAPlatformGateNotADependencyGate(t *testing.T) {
	roles := &fakeRolesEnsurer{declared: true, outcome: RolesEnsureOutcome{}}
	issues := newFakeIssues(nil)
	newRolesGateService(roles, issues).ensureRolesGate(context.Background(), "acme", "workouts", "v1", 7)

	labels := issues.created[0].Labels
	var hasPlatformGate, hasDepLabel, hasArming bool
	for _, l := range labels {
		switch {
		case strings.HasPrefix(l, PlatformGateLabelPrefix):
			hasPlatformGate = true
		case strings.HasPrefix(l, gateDepLabelPrefix):
			hasDepLabel = true
		case l == "aep":
			hasArming = true
		}
	}
	if !hasPlatformGate {
		t.Errorf("labels %v carry no %s label", labels, PlatformGateLabelPrefix)
	}
	if hasDepLabel {
		t.Errorf("labels %v carry a dependency label — it would be indistinguishable from a dep gate", labels)
	}
	if hasArming {
		t.Errorf("labels %v carry the arming label — an agent could pick this up", labels)
	}
	if gateDepFromLabels(labels) != "" {
		t.Errorf("gateDepFromLabels read a dependency out of %v", labels)
	}
}
