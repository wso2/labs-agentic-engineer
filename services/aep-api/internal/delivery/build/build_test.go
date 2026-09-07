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

// COMPONENT tier: the REAL build service behind
// the REAL production handler chain — faked auth at the claims seam →
// contract validation → the deny-by-default tenant gate in ENFORCE → the
// strict handlers (handlers_build.go) — driven in-process via the
// componenttest harness with the service's out-of-process ports faked. The
// non-HTTP StartProjectBuild trigger keeps its direct service-level tests.
//
// External test package: the harness imports api, which imports build — an
// in-package test file would be an import cycle.
package build_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/build"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ----- fakes -----------------------------------------------------------------

type fakeRepos struct{ err error }

func (f fakeRepos) RepoFullName(context.Context, string, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "acme/shop", nil
}

type fakeTagger struct {
	res    *spec.SpecSaveResult
	err    error
	called int
	seq    *[]string
}

func (f *fakeTagger) BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (spec.BuildScope, error) {
	return spec.BuildScope{Tag: tag}, nil
}

func (f *fakeTagger) TagSpec(context.Context, string, string) (*spec.SpecSaveResult, error) {
	f.called++
	if f.seq != nil {
		*f.seq = append(*f.seq, "tag")
	}
	return f.res, f.err
}

// planSpy is the whole milestone plan path as one recording fake: the GitHub
// milestone surface, the run store, the planner, the gates and the supervisor.
// The plan path's own wire behaviour is proven at the service tier
// (milestone_plan_test.go, real IssueService on a gittest.Stub); here it only
// has to show that the HTTP click reaches it and that its conflict reaches the
// edge as a 409.
type planSpy struct {
	mu sync.Mutex

	createdMilestones  []string
	reopenedMilestones []int
	nextNumber         int
	activeRun          *delivery.MilestoneRun
	judgingRun         *delivery.MilestoneRun
	refuseAdmit        bool
	// rows is what ListByProject answers — the version ledger the list read is
	// built from, and the supersede lookup's input.
	rows    []delivery.MilestoneRun
	listErr error
	// milestoneIssues is what the claimed milestone HOLDS. It decides whether the
	// rebuild branch tells the run to skip planning: `unchanged` alone does not,
	// because a milestone whose planning phase never finished holds no work and
	// must be planned. Empty (the default) is that case.
	milestoneIssues []sourcecontrol.IssueInfo

	admitted []delivery.MilestoneRun
	planned  chan int
	started  []delivery.StartRunRequest
	// mintedIssues records anything the plan path files. It must stay empty:
	// the click mints a MILESTONE, and the version's issues are the planning
	// turn's and the gate resolver's to file.
	mintedIssues []string
}

func newPlanSpy() *planSpy {
	return &planSpy{nextNumber: 9, planned: make(chan int, 8)}
}

func (p *planSpy) CreateMilestone(_ context.Context, _, _ string, req sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.createdMilestones = append(p.createdMilestones, req.Title)
	n := p.nextNumber
	p.nextNumber++
	return &sourcecontrol.MilestoneResult{Number: n, Created: true}, nil
}
func (p *planSpy) CloseMilestone(context.Context, string, string, int) error { return nil }
func (p *planSpy) ReopenMilestone(_ context.Context, _, _ string, number int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reopenedMilestones = append(p.reopenedMilestones, number)
	return nil
}
func (p *planSpy) ListMilestoneIssues(context.Context, string, string, sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]sourcecontrol.IssueInfo(nil), p.milestoneIssues...), nil
}

// The issue-write surface. Only CloseIssue is on the plan path (supersede); the
// rest complete delivery.IssueOps so the spy can wear the domain's writer, and
// a plan path that started minting would show up here rather than compiling.
func (p *planSpy) CloseIssue(context.Context, string, string, int, string) error { return nil }
func (p *planSpy) ReopenIssue(context.Context, string, string, int) error        { return nil }
func (p *planSpy) CommentIssue(context.Context, string, string, int, string) error {
	return nil
}
func (p *planSpy) AddLabels(context.Context, string, string, int, []string) error { return nil }
func (p *planSpy) RemoveLabel(context.Context, string, string, int, string) error { return nil }
func (p *planSpy) SetIssueMilestone(context.Context, string, string, int, int) error {
	return nil
}
func (p *planSpy) CreateIssue(_ context.Context, _, _ string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mintedIssues = append(p.mintedIssues, req.Title)
	return &sourcecontrol.IssueResult{Number: 1}, nil
}

func (p *planSpy) ActiveDevRunByProject(context.Context, string, string) (*delivery.MilestoneRun, error) {
	return p.activeRun, nil
}
func (p *planSpy) ActiveValidationRunByProject(context.Context, string, string) (*delivery.MilestoneRun, error) {
	return p.judgingRun, nil
}
func (p *planSpy) TryAdmit(_ context.Context, run *delivery.MilestoneRun) (bool, *delivery.MilestoneRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.refuseAdmit {
		return false, nil, nil
	}
	run.ID = "run-1"
	p.admitted = append(p.admitted, *run)
	return true, run, nil
}
func (p *planSpy) Settle(context.Context, string, string, string) (*delivery.MilestoneRun, error) {
	return nil, nil
}
func (p *planSpy) ListByProject(context.Context, string, string) ([]delivery.MilestoneRun, error) {
	return p.rows, p.listErr
}

// PlanIntoMilestone is still satisfied so the spy fits the port set, but the
// click never calls it now — planning is the run workflow's first phase.
func (p *planSpy) PlanIntoMilestone(_ context.Context, _, _ string, milestoneNumber int) error {
	p.planned <- milestoneNumber
	return nil
}
func (p *planSpy) ProvisionForBuild(context.Context, string, string, string, int, []delivery.ProvisionInput) error {
	return nil
}
func (p *planSpy) StartRun(_ context.Context, req delivery.StartRunRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = append(p.started, req)
	return nil
}

// awaitStart returns the milestone the click handed to the supervisor. No
// synchronisation: the click starts the run synchronously and the supervisor
// fills the milestone as the run's own first phase, so by the time the click has
// returned, the start either happened or failed the request.
func (p *planSpy) awaitStart(t *testing.T) int {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.started) != 1 {
		t.Fatalf("started %d runs, want exactly 1", len(p.started))
	}
	return p.started[0].MilestoneNumber
}

// reopened is the milestones the click put back, read safely.
func (p *planSpy) reopened() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.reopenedMilestones...)
}

// startedRuns is what the click handed the supervisor, read safely.
func (p *planSpy) startedRuns() []delivery.StartRunRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]delivery.StartRunRequest(nil), p.started...)
}

func (p *planSpy) milestones() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.createdMilestones...)
}

func (p *planSpy) admittedRuns() []delivery.MilestoneRun {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]delivery.MilestoneRun(nil), p.admitted...)
}

// withPlanPath wires the spy as the service's plan path.
func withPlanPath(svc *build.Service, spy *planSpy) *build.Service {
	svc.SetPlanPath(build.PlanPathDeps{
		Milestones: spy, Issues: delivery.NewIssueWriter(spy),
		Runs: spy, Planner: spy, Gates: spy, Starter: spy,
	})
	return svc
}

func newSvc(repos fakeRepos, tagger *fakeTagger) *build.Service {
	return build.NewService(build.Deps{Repos: repos, Tagger: tagger})
}

// newHarness assembles the real handler chain around the real build service.
func newHarness(t *testing.T, svc *build.Service) *componenttest.Harness {
	t.Helper()
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{BuildSvc: svc})),
	}})
}

// mustDelivery assembles the delivery domain aggregator for the harness (New
// never errors today; panic keeps the wiring honest if that changes).
func mustDelivery(h *deliveryhttpapi.Handlers, err error) *deliveryhttpapi.Handlers {
	if err != nil {
		panic(err)
	}
	return h
}

type provisionSpy struct {
	orgs []string
	err  error
	seq  *[]string
}

func (p *provisionSpy) ProvisionPublisherForBuild(_ context.Context, orgID string) error {
	if p.seq != nil {
		*p.seq = append(*p.seq, "provision")
	}
	p.orgs = append(p.orgs, orgID)
	return p.err
}

func newHarnessWithPublisher(t *testing.T, svc *build.Service, p build.PublisherProvisioner) *componenttest.Harness {
	t.Helper()
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{
			BuildSvc: svc, PublisherProvisioner: p,
		})),
	}})
}

func postBuild(t *testing.T, svc *build.Service, project string) (int, string) {
	t.Helper()
	resp := newHarness(t, svc).AsOrg("acme").Post("/api/v1/projects/"+project+"/build", `{}`)
	return resp.Code, resp.Body.String()
}

func listBuilds(t *testing.T, svc *build.Service, project string) (int, string) {
	t.Helper()
	resp := newHarness(t, svc).AsOrg("acme").Get("/api/v1/projects/" + project + "/builds")
	return resp.Code, resp.Body.String()
}

func decodeBody[T any](t *testing.T, body string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode body: %v\n%s", err, body)
	}
	return out
}

// ----- POST /build ------------------------------------------------------------

// One click: the whole-spec gate cuts the tag, the version is claimed
// (milestone minted + run row admitted) BEFORE the response, and the plan turn
// runs detached — the POST must not hold open for an LLM turn.
func TestBuild_CutsTheTagAndClaimsTheVersion(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: "approved", Tag: "v1", Version: 1}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	if out := decodeBody[gen.BuildResponse](t, body); out.Tag != "v1" {
		t.Errorf("tag = %q, want v1", out.Tag)
	}
	// The milestone is titled after the tag — the spec pin IS the title, and
	// the run row records its number.
	if got := spy.milestones(); len(got) != 1 || got[0] != "v1" {
		t.Fatalf("milestones created = %v, want [v1]", got)
	}
	runs := spy.admittedRuns()
	if len(runs) != 1 {
		t.Fatalf("admitted %d run rows, want 1 — the mutex must be armed before the response", len(runs))
	}
	row := runs[0]
	if row.OrgID != "acme" || row.ProjectID != "shop" || row.MilestoneNumber != 9 ||
		row.MilestoneTitle != "v1" || row.Origin != delivery.RunOriginSpecBuild ||
		row.State != delivery.RunStatePlanning {
		t.Errorf("admitted run = %+v", row)
	}
	if n := spy.awaitStart(t); n != 9 {
		t.Errorf("planned into milestone %d, want 9", n)
	}
}

// THE SPEC-SAVE STATUS PICKS THE BRANCH, and this is the pair that pins it: what
// the click does after a cancel is decided by whether the person edited the spec.
//
// `unchanged` is the SAME tag, so the same milestone: reopen it and exactly the
// issues a cancel closed inside it, and — when that milestone HOLDS the version's
// work — tell the run not to plan. The run must be told, because a re-plan over a
// milestone whose issues were all closed would recognise every title slug, mint
// nothing, and hand the loop an empty working set to read as "delivered".
func TestBuild_UnchangedSpec_ReopensTheIncrementAndDoesNotReplan(t *testing.T) {
	spy := newPlanSpy()
	// The milestone was filled by the build this one is recovering from.
	spy.milestoneIssues = []sourcecontrol.IssueInfo{
		{Number: 31, State: "closed", Labels: []string{"aep", "development", "aep:cancelled"}},
	}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: spec.SpecSaveUnchanged, Tag: "v2", Version: 2}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	if out := decodeBody[gen.BuildResponse](t, body); out.Tag != "v2" {
		t.Errorf("tag = %q, want the existing v2", out.Tag)
	}
	// CreateMilestone is idempotent, so an unchanged spec adopts the same
	// milestone — and the click puts it, and what the cancel closed in it, back.
	spy.awaitStart(t)
	if got := spy.reopened(); len(got) != 1 || got[0] != 9 {
		t.Errorf("reopened milestones = %v, want the claimed one (9) reopened exactly once", got)
	}
	if req := spy.startedRuns()[0]; !req.Rebuild {
		t.Errorf("started %+v, want Rebuild set so the run mints its gates and skips the planning turn", req)
	}
}

// AN UNCHANGED SPEC IS NOT EVIDENCE THE MILESTONE WAS FILLED, and this is the
// case that proves the second question has to be asked.
//
// A run that died in its PLANNING phase — `plan-failed`, or a cancel that landed
// before the planning turn — leaves the milestone holding its gates and no work
// at all. The spec is unchanged, so the click takes the rebuild branch; but
// telling the run to skip planning there hands the loop an empty working set,
// which it reads as "planning produced nothing to work" and settles the version
// SUCCEEDED having built none of it. The milestone still has to be reopened —
// this build IS that version being worked — but the plan has to run.
func TestBuild_UnchangedSpecOverAnUnplannedMilestone_StillPlansIt(t *testing.T) {
	spy := newPlanSpy()
	// Gates and nothing else: the shape a planning phase that never finished leaves.
	spy.milestoneIssues = []sourcecontrol.IssueInfo{
		{Number: 12, State: "open", Labels: []string{"provision", "aep:dep/orders-db"}},
	}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: spec.SpecSaveUnchanged, Tag: "v2", Version: 2}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	spy.awaitStart(t)
	if got := spy.reopened(); len(got) != 1 || got[0] != 9 {
		t.Errorf("reopened milestones = %v, want the claimed one reopened — the version is being worked", got)
	}
	if req := spy.startedRuns()[0]; req.Rebuild {
		t.Errorf("started %+v, want Rebuild CLEAR: skipping the plan over a milestone with no work "+
			"would settle an unbuilt version as delivered", req)
	}
}

// `approved` is a NEW tag: the ordinary build path with nothing special about it.
// It plans fresh, and it must not carry the rebuild flag — a version that was
// never filled has to be.
func TestBuild_ChangedSpec_PlansTheNewVersionFresh(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: spec.SpecSaveApproved, Tag: "v3", Version: 3}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	spy.awaitStart(t)
	if got := spy.reopened(); len(got) != 0 {
		t.Errorf("a new version reopens nothing, got %v", got)
	}
	if req := spy.startedRuns()[0]; req.Rebuild {
		t.Errorf("started %+v, want Rebuild clear — a new version has to be planned", req)
	}
}

// The build mutex: a second click while a dev run is live is a 409, and it
// never reaches the tagger — a rejected build claims no version.
func TestBuild_SpecRunAlreadyLive_409_TaggerUntouched(t *testing.T) {
	spy := newPlanSpy()
	spy.activeRun = &delivery.MilestoneRun{ID: "run-1", MilestoneNumber: 9, State: delivery.RunStateWaiting}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v2"}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 409 {
		t.Fatalf("status = %d, want 409 (body=%s)", code, body)
	}
	if e := componenttest.DecodeEnvelope(t, body); e.Code != "conflict" {
		t.Fatalf("409 envelope = %+v", e)
	}
	if tagger.called != 0 {
		t.Errorf("tagger called %d times behind the mutex, want 0", tagger.called)
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a rejected click minted a milestone: %v", spy.milestones())
	}
}

// The build's OTHER refusal: this project's deployed version is being judged.
//
// A delivery run merging and promoting while validation asserts against the
// deployment would be judging a moving target — the verdict would name criteria
// that were true of neither the old release nor the new one. A validation run
// deliberately sits outside the build mutex (it re-judges a version that already
// shipped, so no database index refuses this), which is why the guard is an
// explicit read and this test is the only thing enforcing it.
//
// Refused rather than queued, and the way past it is one click: cancel the
// validation.
func TestBuild_ValidationRunLive_409_TaggerUntouched(t *testing.T) {
	spy := newPlanSpy()
	spy.judgingRun = &delivery.MilestoneRun{
		ID: "run-v", MilestoneNumber: 9, Kind: delivery.RunKindValidation,
		State: delivery.RunStateRunning,
	}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v2"}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 409 {
		t.Fatalf("status = %d, want 409 (body=%s)", code, body)
	}
	e := componenttest.DecodeEnvelope(t, body)
	if e.Code != "conflict" {
		t.Fatalf("409 envelope = %+v", e)
	}
	// Its own message: "wait for a build" and "cancel the validation" send a user
	// to different places.
	if !strings.Contains(e.Message, "validated") {
		t.Errorf("409 message = %q, want it to name the validation run", e.Message)
	}
	// Checked BEFORE the tag is cut, so a refused build claims no version.
	if tagger.called != 0 {
		t.Errorf("tagger called %d times behind the validation guard, want 0", tagger.called)
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a rejected click minted a milestone: %v", spy.milestones())
	}
}

// And it is only a refusal while the validation is LIVE: a settled one holds
// nothing, so the next build proceeds normally.
func TestBuild_NoLiveValidationRun_Proceeds(t *testing.T) {
	spy := newPlanSpy()
	spy.judgingRun = nil // the validation settled
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: "changed", Tag: "v3", Version: 3}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("status = %d, want 200 (body=%s)", code, body)
	}
	spy.awaitStart(t)
}

// The DB index is the mutex's authority. When the pre-check passes but the
// admission INSERT loses the race, the click still answers 409 — never a 500,
// and never a second live run.
func TestBuild_AdmissionRaceLost_409(t *testing.T) {
	spy := newPlanSpy()
	spy.refuseAdmit = true
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v2"}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 409 {
		t.Fatalf("status = %d, want 409 (body=%s)", code, body)
	}
	if len(spy.admittedRuns()) != 0 {
		t.Errorf("a lost race must admit nothing, got %+v", spy.admittedRuns())
	}
}

// The spec gate's failure was a Huma 422 problem; on the contract-first edge
// it is a 400 validation_failed whose details keep the per-file path +
// code:message (the error-model break).
func TestBuild_SpecValidationFails_400_NoVersionClaimed(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{err: &spec.SpecValidationError{Files: []spec.FileValidationError{
		{Path: "specs/requirements/prd.md", Code: "MISSING_REQUIREMENTS", Message: "missing"},
		{Path: "specs/design/design.cell", Code: "MISSING_DESIGN", Message: "missing"},
	}}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 400 {
		t.Fatalf("status = %d, want 400 (was Huma's 422)", code)
	}
	e := componenttest.DecodeEnvelope(t, body)
	if e.Code != "validation_failed" || e.Message != "spec validation failed" {
		t.Fatalf("envelope = %+v, want validation_failed / spec validation failed", e)
	}
	if len(e.Details) != 2 ||
		e.Details[0].Field != "specs/requirements/prd.md" ||
		!strings.Contains(e.Details[0].Message, "MISSING_REQUIREMENTS") {
		t.Fatalf("details = %+v, want the per-file locations + code:message", e.Details)
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a version was claimed despite a failed spec gate: %v", spy.milestones())
	}
}

func TestBuild_NoRepo_404(t *testing.T) {
	svc := newSvc(fakeRepos{err: sourcecontrol.ErrRepoNotFound}, &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}})
	code, body := postBuild(t, svc, "shop")
	if code != 404 {
		t.Fatalf("status = %d, want 404 (body=%s)", code, body)
	}
	if e := componenttest.DecodeEnvelope(t, body); e.Code != "not_found" ||
		e.Message != "project repository not found" {
		t.Fatalf("404 envelope = %+v", e)
	}
}

func TestBuild_RepoNotReady_409(t *testing.T) {
	tagger := &fakeTagger{err: sourcecontrol.ErrRepoNotReady}
	svc := newSvc(fakeRepos{}, tagger)
	code, body := postBuild(t, svc, "shop")
	if code != 409 {
		t.Fatalf("status = %d, want 409 (body=%s)", code, body)
	}
}

// The gate outranks the handler: a claimless build request is the tenant
// gate's ENFORCE 401 — the service is never reached.
func TestBuild_NoClaims401(t *testing.T) {
	spy := newPlanSpy()
	svc := withPlanPath(newSvc(fakeRepos{}, &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}), spy)
	resp := newHarness(t, svc).NoAuth().Post("/api/v1/projects/shop/build", `{}`)
	if resp.Code != 401 {
		t.Fatalf("no-claims build: want 401, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("claimless request must never reach the service, but it claimed %v", spy.milestones())
	}
}

// ----- POST /build publisher provisioning ------------------------------------

// The publisher provisioner runs before Run cuts the tag — the
// handler, not the service, calls it, since only the handler still has the
// console JWT that ProvisionPublisherForBuild needs.
func TestBuild_PublisherProvisionerRunsBeforeTag(t *testing.T) {
	var seq []string
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1", Status: "approved"}, seq: &seq}
	svc := newSvc(fakeRepos{}, tagger)
	spy := &provisionSpy{seq: &seq}
	resp := newHarnessWithPublisher(t, svc, spy).AsOrg("acme").Post("/api/v1/projects/shop/build", `{}`)
	if resp.Code != 200 {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if len(spy.orgs) != 1 || spy.orgs[0] != "acme" {
		t.Fatalf("provisioner orgs=%v", spy.orgs)
	}
	if tagger.called != 1 {
		t.Fatalf("Run must still tag after provision, called=%d", tagger.called)
	}
	if len(seq) != 2 || seq[0] != "provision" || seq[1] != "tag" {
		t.Fatalf("order=%v want provision then tag", seq)
	}
}

// A provision failure (e.g. no JWT on ctx, SM-API down) answers 503 and never
// reaches Run — no tag is cut on unprovisioned publisher credentials.
func TestBuild_PublisherProvisionErrorDoesNotTag(t *testing.T) {
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1", Status: "approved"}}
	svc := newSvc(fakeRepos{}, tagger)
	spy := &provisionSpy{err: errors.New("sm-api: no JWT in context")}
	resp := newHarnessWithPublisher(t, svc, spy).AsOrg("acme").Post("/api/v1/projects/shop/build", `{}`)
	if resp.Code != 503 {
		t.Fatalf("status=%d want 503 body=%s", resp.Code, resp.Body.String())
	}
	if tagger.called != 0 {
		t.Fatalf("failed provision must not cut a tag, called=%d", tagger.called)
	}
	if strings.Contains(resp.Body.String(), "sm-api") || strings.Contains(resp.Body.String(), "JWT") {
		t.Fatalf("client body must not echo the provisioner error, got %s", resp.Body.String())
	}
}

// ----- StartProjectBuild (non-HTTP provider-build trigger) --------------------

func TestStartProjectBuild_HappyPath_ClaimsTheVersion(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: "approved", Tag: "v1", Version: 1}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	if err := svc.StartProjectBuild(context.Background(), "acme", "shop"); err != nil {
		t.Fatalf("StartProjectBuild: %v", err)
	}
	if got := spy.milestones(); len(got) != 1 || got[0] != "v1" {
		t.Errorf("milestones = %v, want [v1]", got)
	}
	if len(spy.admittedRuns()) != 1 {
		t.Errorf("admitted %d run rows, want 1", len(spy.admittedRuns()))
	}
	spy.awaitStart(t)
}

// StartProjectBuild is the non-HTTP auto-kick trigger, which never has a
// console JWT — it must not see the handler's publisher provisioner, and
// must keep going through Run exactly as before.
func TestStartProjectBuild_DoesNotUseHandlerProvisioner(t *testing.T) {
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1", Status: "approved"}}
	svc := newSvc(fakeRepos{}, tagger)
	if err := svc.StartProjectBuild(context.Background(), "acme", "shop"); err != nil {
		t.Fatalf("StartProjectBuild: %v", err)
	}
	if tagger.called != 1 {
		t.Fatalf("auto-kick still uses Run, called=%d", tagger.called)
	}
}

// ----- GET /builds (the version ledger) ---------------------------------------

// The ledger is one entry per spec version, newest first, each carrying the
// state of the NEWEST milestone run that has worked it — a milestone sees
// sequential runs (the spec build that created it, then any incident adoption
// into it), and only the newest describes the version now.
func TestListBuilds_NewestFirstOneEntryPerVersion(t *testing.T) {
	t0 := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	ended := t0.Add(90 * time.Minute)
	spy := newPlanSpy()
	spy.rows = []delivery.MilestoneRun{
		{MilestoneNumber: 12, MilestoneTitle: "v2", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
			State: delivery.RunStateRunning, CreatedAt: t0.Add(2 * time.Hour)},
		{MilestoneNumber: 11, MilestoneTitle: "v1", Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
			State: delivery.RunStateSucceeded, CreatedAt: t0.Add(time.Hour), EndedAt: &ended},
		{MilestoneNumber: 11, MilestoneTitle: "v1", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
			State: delivery.RunStateFailed, TerminalReason: delivery.RunReasonCycleCeiling, CreatedAt: t0},
	}
	svc := withPlanPath(newSvc(fakeRepos{}, &fakeTagger{}), spy)

	code, rawBody := listBuilds(t, svc, "shop")
	if code != 200 {
		t.Fatalf("list: got %d body=%s", code, rawBody)
	}
	builds := decodeBody[gen.BuildList](t, rawBody).Builds
	if len(builds) != 2 {
		t.Fatalf("builds = %+v, want 2 (v1's older run folded into its newest)", builds)
	}
	v2 := builds[0]
	if v2.Tag != "v2" || v2.Status != "in_progress" || v2.MilestoneNumber != 12 {
		t.Errorf("builds[0] = %+v, want the live v2 first, keyed to milestone 12", v2)
	}
	if !v2.StartedAt.Equal(t0.Add(2*time.Hour)) || v2.CompletedAt != nil {
		t.Errorf("v2 times = %v/%v, want startedAt=+2h and no completedAt while live", v2.StartedAt, v2.CompletedAt)
	}
	v1 := builds[1]
	if v1.Tag != "v1" || v1.Status != "completed" {
		t.Errorf("builds[1] = %+v, want the newest v1 run (succeeded), not its failed predecessor", v1)
	}
	if v1.Reason != "" {
		t.Errorf("v1 reason = %q, want empty — the failed predecessor's reason must not leak", v1.Reason)
	}
	if v1.CompletedAt == nil || !v1.CompletedAt.Equal(ended) {
		t.Errorf("v1 completedAt = %v, want the run's endedAt", v1.CompletedAt)
	}
}

// A failed version carries its run's terminal reason — the one string that
// names why it stopped.
func TestListBuilds_FailedVersionCarriesItsTerminalReason(t *testing.T) {
	spy := newPlanSpy()
	spy.rows = []delivery.MilestoneRun{{
		MilestoneNumber: 3, MilestoneTitle: "v1", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
		State: delivery.RunStateFailed, TerminalReason: delivery.RunReasonNoProgress,
	}}
	svc := withPlanPath(newSvc(fakeRepos{}, &fakeTagger{}), spy)

	_, rawBody := listBuilds(t, svc, "shop")
	got := decodeBody[gen.BuildList](t, rawBody).Builds[0]
	if got.Status != "failed" || got.Reason != delivery.RunReasonNoProgress {
		t.Errorf("failed version = %+v, want failed / %s", got, delivery.RunReasonNoProgress)
	}
}

// A version parked at the deploy gate (ADR-0023) says so on the ledger row.
// `status` alone is `in_progress` for a waiting run as much as a running one,
// so without the reason the row reads as a coding agent still working when the
// run has stopped and is waiting on a PERSON. The run row is already in hand,
// so this costs the ledger no extra read.
func TestListBuilds_ParkedVersionCarriesItsWaitingReason(t *testing.T) {
	spy := newPlanSpy()
	spy.rows = []delivery.MilestoneRun{
		{MilestoneNumber: 12, MilestoneTitle: "v2", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
			State: delivery.RunStateWaiting, WaitingReason: delivery.RunWaitingOnExternalValues,
			BlockingDependencies: delivery.DependencyNames{"stripe", "twilio"}},
		{MilestoneNumber: 11, MilestoneTitle: "v1", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
			State: delivery.RunStateRunning},
	}
	svc := withPlanPath(newSvc(fakeRepos{}, &fakeTagger{}), spy)

	_, rawBody := listBuilds(t, svc, "shop")
	builds := decodeBody[gen.BuildList](t, rawBody).Builds

	parked := builds[0]
	if parked.Status != "in_progress" {
		t.Fatalf("parked status = %q, want in_progress — the version is still in flight", parked.Status)
	}
	if parked.WaitingReason != gen.BuildSummaryWaitingReasonExternalValues {
		t.Errorf("parked waitingReason = %q, want %q — the row must say it waits on a person",
			parked.WaitingReason, gen.BuildSummaryWaitingReasonExternalValues)
	}
	if running := builds[1]; running.WaitingReason != "" {
		t.Errorf("running waitingReason = %q, want empty", running.WaitingReason)
	}
	// The NAMES stay off the ledger — the row has no space for them, and the
	// build page has already made the run read that carries them.
	if strings.Contains(rawBody, "blockingDependencies") {
		t.Errorf("ledger body names the blocking dependencies: %s", rawBody)
	}
}

// A reason left behind on a run that has moved on must not surface: it would
// read as a hang that is not happening.
func TestListBuilds_StaleWaitingReasonIsNotSurfaced(t *testing.T) {
	spy := newPlanSpy()
	spy.rows = []delivery.MilestoneRun{{
		MilestoneNumber: 3, MilestoneTitle: "v1", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
		State: delivery.RunStateSucceeded, WaitingReason: delivery.RunWaitingOnExternalValues,
	}}
	svc := withPlanPath(newSvc(fakeRepos{}, &fakeTagger{}), spy)

	_, rawBody := listBuilds(t, svc, "shop")
	got := decodeBody[gen.BuildList](t, rawBody).Builds[0]
	if got.Status != "completed" || got.WaitingReason != "" {
		t.Errorf("settled version = %+v, want completed with no waiting reason", got)
	}
}

// No runs → an empty list serialized as [] (not null), whether the plan path is
// wired or not.
func TestListBuilds_EmptyList(t *testing.T) {
	for name, svc := range map[string]*build.Service{
		"wired":   withPlanPath(newSvc(fakeRepos{}, &fakeTagger{}), newPlanSpy()),
		"unwired": newSvc(fakeRepos{}, &fakeTagger{}),
	} {
		code, rawBody := listBuilds(t, svc, "shop")
		if code != 200 {
			t.Fatalf("%s: got %d body=%s", name, code, rawBody)
		}
		if !strings.Contains(rawBody, `"builds":[]`) {
			t.Errorf("%s: body = %s, want an empty non-null builds array", name, rawBody)
		}
	}
}

func TestListBuilds_StoreError_500(t *testing.T) {
	spy := newPlanSpy()
	spy.listErr = errors.New("db down")
	svc := withPlanPath(newSvc(fakeRepos{}, &fakeTagger{}), spy)

	code, body := listBuilds(t, svc, "shop")
	if code != 500 {
		t.Fatalf("status = %d, want 500 (body=%s)", code, body)
	}
	e := componenttest.DecodeEnvelope(t, body)
	if e.Code != "internal_error" || e.Message != "list builds" {
		t.Fatalf("500 envelope = %+v (must not leak the store error)", e)
	}
}

// ----- GET /build/preflight -----------------------------------------------------

// pfDesign / pfStatus are the preflight ports' fakes for the HTTP-surface
// wiring test (the filtering rules themselves are unit-proven in
// preflight_test.go).
type pfDesign struct{ comps []spec.DesignComponent }

func (f pfDesign) ReadDesignComponents(context.Context, string, string) ([]spec.DesignComponent, error) {
	return f.comps, nil
}

type pfStatus struct{}

func (pfStatus) Ready(context.Context, string, string, string) (bool, error) { return false, nil }

func TestGetPreflight_WiredThroughRealService(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg", Parameters: map[string]any{"instances": 1}},
		}}}
	pfSvc := build.NewPreflightService(build.PreflightDeps{Design: pfDesign{comps: comps}, Status: pfStatus{}})
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{PreflightSvc: pfSvc})),
	}})

	resp := h.AsOrg("acme").Get("/api/v1/projects/shop/build/preflight")
	if resp.Code != 200 {
		t.Fatalf("preflight: got %d body=%s", resp.Code, resp.Body.String())
	}
	pf := decodeBody[gen.BuildPreflight](t, resp.Body.String())
	if !pf.NeedsInput || len(pf.Items) != 1 {
		t.Fatalf("preflight = %+v, want one platform-resource item", pf)
	}
	item := pf.Items[0]
	if item.Dependency != "orders-db" || item.Kind != "platform-resource" || item.ResourceType != "postgres-cnpg" {
		t.Errorf("item = %+v", item)
	}
}

// ----- Dependency hard gate (#252 Task 10 — restores the gate Task 1 orphaned) --

// gateDesign is a mutable ReadDesignComponents fake: resolvingSpec.CollectSpec
// flips a dependency's Status in place, simulating the committed-truth write
// InputsCoordinator.ApplyPreTag triggers in production — so the gate's fresh
// re-read AFTER ApplyPreTag sees the resolution a legitimate drawer submission
// (pasted spec content, THIS build request) just supplied.
type gateDesign struct{ comps []spec.DesignComponent }

func (f *gateDesign) ReadDesignComponents(context.Context, string, string) ([]spec.DesignComponent, error) {
	return f.comps, nil
}

func (f *gateDesign) resolve(depName string) {
	for i := range f.comps {
		for j := range f.comps[i].Dependencies {
			d := &f.comps[i].Dependencies[j]
			if d.Name == depName {
				d.Status = spec.DependencyStatusResolved
				d.Reason = ""
			}
		}
	}
}

type resolvingSpec struct{ design *gateDesign }

func (r *resolvingSpec) CollectSpec(_ context.Context, _, _, _, dep string, _ []byte, _ string) (string, error) {
	r.design.resolve(dep)
	return "specs/design/components/o/dependencies/" + dep + ".openapi.yaml", nil
}

type noopAuth struct{}

func (noopAuth) DerivePlatformResourceFactsAtHead(context.Context, string, string) error { return nil }

type errAuth struct{ err error }

func (e errAuth) DerivePlatformResourceFactsAtHead(context.Context, string, string) error {
	return e.err
}

// An uninstalled resourceType is refused at the click: HTTP 409, no tag, no
// Temporal run. The design is unsatisfiable on this cluster; retrying provision
// cannot invent a ClusterResourceType.
func TestBuild_UnknownResourceType_409_NoTagNoWorkflow(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	coord := build.NewInputsCoordinator(nil, errAuth{err: build.ErrUnknownResourceType}, nil)
	svc := withPlanPath(build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Coord: coord,
	}), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 409 {
		t.Fatalf("status = %d, want 409 (body=%s)", code, body)
	}
	e := componenttest.DecodeEnvelope(t, body)
	if e.Code != "conflict" {
		t.Fatalf("409 envelope = %+v", e)
	}
	if !strings.Contains(e.Message, "resourceType is not installed") {
		t.Errorf("409 message = %q, want it to name the missing resourceType", e.Message)
	}
	if tagger.called != 0 {
		t.Errorf("tagger called %d times, want 0 — unknown type must block before the tag-cut", tagger.called)
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a rejected click minted a milestone: %v", spy.milestones())
	}
	if len(spy.startedRuns()) != 0 {
		t.Errorf("a rejected click started a run: %v", spy.startedRuns())
	}
}

// A doctored client (no inputs at all) cannot skip the drawer: an ambiguous
// external dependency blocks with a failure, no tag is cut, and no workflow
// starts.
func TestBuild_DependencyGate_AmbiguousExternal_BlocksNoTagNoWorkflow(t *testing.T) {
	design := &gateDesign{comps: []spec.DesignComponent{{Name: "o", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "salesforce", Status: spec.DependencyStatusAmbiguous},
		}}}}
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	svc := withPlanPath(build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Design: design,
	}), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	out := decodeBody[gen.BuildResponse](t, body)
	if len(out.Failures) != 1 {
		t.Fatalf("failures = %+v, want 1", out.Failures)
	}
	if f := out.Failures[0]; f.Dependency != "salesforce" || f.Kind != "external-ambiguous" {
		t.Errorf("failure = %+v, want {salesforce, external-ambiguous}", f)
	}
	if out.Tag != "" {
		t.Errorf("tag = %q, want empty — no tag on a gated build", out.Tag)
	}
	if tagger.called != 0 {
		t.Errorf("tagger called %d times, want 0 — the gate must block before the tag-cut", tagger.called)
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a version was claimed despite the dependency gate blocking: %v", spy.milestones())
	}
}

// A web-application's ambiguous external dependency blocks the build exactly
// like a service's would (#252 Task 14 — lifting the ComponentType != service
// guard dependencyGateFailures used to apply here). Task 9 already shows this
// dependency's status chip and the coding-agent wiring already emits
// consumed-spec instructions for it regardless of component kind, so the
// build-time hard gate must not be the one surface that still lets it through.
func TestBuild_DependencyGate_WebApplication_AmbiguousExternal_Blocks(t *testing.T) {
	design := &gateDesign{comps: []spec.DesignComponent{{Name: "web", ComponentType: spec.ComponentTypeWebApplication,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "salesforce", Status: spec.DependencyStatusAmbiguous},
		}}}}
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	svc := withPlanPath(build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Design: design,
	}), spy)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	out := decodeBody[gen.BuildResponse](t, body)
	if len(out.Failures) != 1 {
		t.Fatalf("failures = %+v, want 1", out.Failures)
	}
	if f := out.Failures[0]; f.Component != "web" || f.Dependency != "salesforce" || f.Kind != "external-ambiguous" {
		t.Errorf("failure = %+v, want {web, salesforce, external-ambiguous}", f)
	}
	if out.Tag != "" {
		t.Errorf("tag = %q, want empty — no tag on a gated build", out.Tag)
	}
	if tagger.called != 0 {
		t.Errorf("tagger called %d times, want 0 — the gate must block before the tag-cut", tagger.called)
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a version was claimed despite the dependency gate blocking: %v", spy.milestones())
	}
}

// An unresolved (needs-input) external dependency maps to "external-unresolved".
func TestBuild_DependencyGate_NeedsInput_Blocks(t *testing.T) {
	design := &gateDesign{comps: []spec.DesignComponent{{Name: "o", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "weather-api",
				Status: spec.DependencyStatusUnresolved, Reason: spec.DependencyReasonNeedsInput},
		}}}}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	svc := build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Design: design,
	})

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	out := decodeBody[gen.BuildResponse](t, body)
	if len(out.Failures) != 1 || out.Failures[0].Kind != "external-unresolved" || out.Failures[0].Dependency != "weather-api" {
		t.Fatalf("failures = %+v, want one {weather-api, external-unresolved}", out.Failures)
	}
	if tagger.called != 0 {
		t.Errorf("tagger called despite the gate blocking")
	}
}

// A doctored client sending no external-spec input for a needs-spec dependency
// still gets gated: kind maps to the pre-existing "external-spec".
func TestBuild_DependencyGate_NeedsSpec_NoDrawerInput_Blocks(t *testing.T) {
	design := &gateDesign{comps: []spec.DesignComponent{{Name: "o", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "partner-api",
				Status: spec.DependencyStatusUnresolved, Reason: spec.DependencyReasonNeedsSpec},
		}}}}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	svc := build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Design: design,
	})

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	out := decodeBody[gen.BuildResponse](t, body)
	if len(out.Failures) != 1 || out.Failures[0].Kind != "external-spec" || out.Failures[0].Dependency != "partner-api" {
		t.Fatalf("failures = %+v, want one {partner-api, external-spec}", out.Failures)
	}
	if tagger.called != 0 {
		t.Errorf("tagger called despite the gate blocking")
	}
}

// The legitimate flow: the drawer's own external-spec local form submits a
// pasted spec with THIS build request. ApplyPreTag commits it (CollectSpec)
// BEFORE the gate re-reads — so the gate sees the now-resolved dependency and
// the build proceeds. Proves the gate runs AFTER ApplyPreTag, not before.
func TestBuild_DependencyGate_NeedsSpec_ResolvedByThisRequestsDrawerInput_Proceeds(t *testing.T) {
	design := &gateDesign{comps: []spec.DesignComponent{{Name: "o", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "partner-api",
				Status: spec.DependencyStatusUnresolved, Reason: spec.DependencyReasonNeedsSpec},
		}}}}
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	coord := build.NewInputsCoordinator(&resolvingSpec{design: design}, noopAuth{}, design)
	svc := withPlanPath(build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Coord: coord, Design: design,
	}), spy)

	resp := newHarness(t, svc).AsOrg("acme").Post("/api/v1/projects/shop/build",
		`{"inputs":[{"component":"o","dependency":"partner-api","kind":"external-spec","specContent":"openapi: 3.0.0"}]}`)
	if resp.Code != 200 {
		t.Fatalf("build: got %d body=%s", resp.Code, resp.Body.String())
	}
	out := decodeBody[gen.BuildResponse](t, resp.Body.String())
	if len(out.Failures) != 0 {
		t.Fatalf("failures = %+v, want none — the drawer-supplied spec resolved the dependency before the gate ran", out.Failures)
	}
	if out.Tag != "v1" {
		t.Errorf("tag = %q, want v1", out.Tag)
	}
	if len(spy.admittedRuns()) != 1 {
		t.Errorf("the version was not claimed despite a resolved gate")
	}
}

// org-service is NOT re-gated at build time — that dependency kind is already
// gated at design-save time (design.SaveAndProceed's firstUnresolvedDependency);
// re-gating it here would double-block the same condition through a different
// surface.
func TestBuild_DependencyGate_OrgServiceUnresolved_NotGatedHere(t *testing.T) {
	design := &gateDesign{comps: []spec.DesignComponent{{Name: "o", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindOrgService, Name: "billing", Status: spec.DependencyStatusUnresolved},
		}}}}
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	svc := build.NewService(build.Deps{
		Repos: fakeRepos{}, Tagger: tagger, Design: design,
	})

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	out := decodeBody[gen.BuildResponse](t, body)
	if out.Tag != "v1" || len(out.Failures) != 0 {
		t.Errorf("response = %+v, want a clean tag — org-service is gated at design-save time, not here", out)
	}
}

// No Design port wired (mirrors every OTHER test in this file, which never set
// one) — the gate must fail OPEN, not panic or block, so this feature never
// regresses a build whose composition root hasn't wired it.
func TestBuild_DependencyGate_NilDesign_FailsOpen(t *testing.T) {
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Tag: "v1"}}
	svc := newSvc(fakeRepos{}, tagger)

	code, body := postBuild(t, svc, "shop")
	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	if out := decodeBody[gen.BuildResponse](t, body); out.Tag != "v1" {
		t.Errorf("tag = %q, want v1 — an unwired dependency gate must fail open", out.Tag)
	}
}

func TestGetPreflight_Unconfigured503(t *testing.T) {
	// The domain is wired but its preflight service is not (an empty Deps): the
	// build handler is non-nil and answers 503 from its own nil guard, exactly as
	// the pre-migration edge did on an unset PreflightSvc.
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{})),
	}})
	resp := h.AsOrg("acme").Get("/api/v1/projects/shop/build/preflight")
	if resp.Code != 503 {
		t.Fatalf("unwired preflight: want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "service_unavailable" || e.Message != "build preflight is not configured" {
		t.Fatalf("503 envelope = %+v", e)
	}
}
