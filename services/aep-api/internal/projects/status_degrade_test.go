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

package projects

// The status poll's AE Studio degrade: when the org's AE Studio
// cannot answer for the repo, the git-derived spec facts are marked
// unavailable and the poll still answers, with the build and deploy stages
// intact. Driven over the REAL artifact service and the in-memory pod.

import (
	"context"

	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// oneRepoRow is a sourcecontrol.RepoRepository holding a single row.
type oneRepoRow struct{ row *sourcecontrol.GitRepository }

var _ sourcecontrol.RepoRepository = oneRepoRow{}

func (r oneRepoRow) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return r.row, nil
}
func (oneRepoRow) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, sourcecontrol.ErrRepoNotFound
}
func (oneRepoRow) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (oneRepoRow) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (oneRepoRow) SetWebhookIDIfReady(context.Context, string, string, int64) (bool, error) {
	panic("not used")
}
func (oneRepoRow) ClearWebhookIDs(context.Context, string) error { panic("not used") }
func (oneRepoRow) SetStatusIf(context.Context, string, string, string, string) (bool, error) {
	panic("not used")
}
func (oneRepoRow) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (oneRepoRow) Create(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (oneRepoRow) Update(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (oneRepoRow) DeleteByOrgAndProjectID(context.Context, string, string) error {
	return nil
}

// projectServiceWithFake is the status poll over the real artifact service and
// the in-memory pod, with one delivered version (v1 built and live) so the
// build and deploy stages have something to say. mut arranges the pod.
func projectServiceWithFake(t *testing.T, mut func(*aestudiotest.Fake)) *Service {
	t.Helper()
	row := &sourcecontrol.GitRepository{
		OrgID: "default", ProjectID: "p", Status: "ready",
		RepoURL: "https://github.com/acme/p", DefaultBranch: "main",
	}
	ref, err := sourcecontrol.RefForRow("default", row)
	if err != nil {
		t.Fatal(err)
	}
	pod := aestudiotest.New()
	pod.SeedRepo(ref, map[string]string{"specs/requirements/overview.md": "# p\n"})
	if mut != nil {
		mut(pod)
	}
	repoSvc := &fakeRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
		return row, nil
	}}
	svc := NewProjectService(nil, repoSvc, nil, spec.NewArtifactService(oneRepoRow{row: row}, pod), nil)
	svc.SetStageSources(
		fakeRunReader{rows: []delivery.MilestoneRun{devRun("v1", delivery.RunStateSucceeded)}},
		fakeBindingsReader{items: []openchoreo.ReleaseBindingSummary{devBinding("api", "True", "Ready")}})
	svc.SetWriteTargets(staticWriteTarget{env: "default"})
	return svc
}

func TestProjectStatus_DegradesTo200(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sentinel error
		reason   gen.SpecStageUnavailableReason
	}{
		{sourcecontrol.ErrAEStudioAbsent, gen.SpecStageUnavailableReasonGithubNotConnected},
		{sourcecontrol.ErrAEStudioUnavailable, gen.SpecStageUnavailableReasonAeStudioUnavailable},
		{sourcecontrol.ErrAEStudioMisconfigured, gen.SpecStageUnavailableReasonAeStudioMisconfigured}, // The same degrade
	} {
		sentinel := tc.sentinel
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()
			svc := projectServiceWithFake(t, func(fk *aestudiotest.Fake) { fk.FailOrg("default", sentinel) })
			st, err := svc.GetProjectStatus(context.Background(), "default", "p")
			if err != nil || st.Spec.Availability != "unavailable" || st.Build.Status == "" {
				t.Fatalf("st=%+v err=%v", st, err)
			}
			// The cause rides along, so the console can say what to do about it.
			if want := (gen.SpecStage{Availability: gen.SpecStageAvailabilityUnavailable, UnavailableReason: tc.reason}); st.Spec != want {
				t.Errorf("spec = %+v, want only availability unavailable + reason %s (no git fact may be guessed)", st.Spec, tc.reason)
			}
			// The delivery stages are intact; only the git-derived denominator
			// is unknown.
			if st.Build.Version != "v1" || st.Build.Status != buildSucceeded {
				t.Errorf("build = %s/%s, want v1/succeeded", st.Build.Version, st.Build.Status)
			}
			if st.Deploy.Version != "v1" || st.Deploy.Status != deployDeployed || st.Deploy.Components.Ready != 1 || st.Deploy.Components.Total != 0 {
				t.Errorf("deploy = %+v, want v1 deployed, 1 ready, total unknown (0)", st.Deploy)
			}
			if st.HasSpec || st.HasDesign || st.SpecStatus != "" || st.Phase != "" {
				t.Errorf("flat spec fields must stay unset: hasSpec=%v hasDesign=%v specStatus=%q phase=%q",
					st.HasSpec, st.HasDesign, st.SpecStatus, st.Phase)
			}
		})
	}
}

// The same rig with a serving pod: the facts read, and say so.
func TestProjectStatus_AvailableOverThePod(t *testing.T) {
	t.Parallel()
	st, err := projectServiceWithFake(t, nil).GetProjectStatus(context.Background(), "default", "p")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Spec.Availability != gen.SpecStageAvailabilityAvailable || st.Spec.UnavailableReason != "" || !st.Spec.Exists || st.Phase != "spec" {
		t.Fatalf("spec = %+v phase=%q, want available, exists, phase spec", st.Spec, st.Phase)
	}
}

// The design-staleness baseline shares the degrade: the snapshot read, but
// the requirements at the last design run's base did not.
func TestProjectStatus_FingerprintUnavailableDegrades(t *testing.T) {
	t.Parallel()
	svc := statusFixture{
		snap:   spec.StatusSnapshot{HasSpec: true, HasDesign: true, SpecVersion: "v1", RequirementsFingerprint: "now"},
		runs:   []delivery.MilestoneRun{devRun("v1", delivery.RunStateSucceeded)},
		counts: map[string]int{"v1": 2},
	}.service()
	svc.specTurns = &stubDesignTurns{lastDesign: &spec.AgentTurn{BaseRef: "abc"}}
	svc.artifactSvc.(*artifactstest.FakeArtifactService).RequirementsFingerprintAtFunc =
		func(context.Context, string, string, string) (string, error) {
			return "", sourcecontrol.ErrAEStudioUnavailable
		}

	st, err := svc.GetProjectStatus(context.Background(), "acme", "web")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Spec.Availability != gen.SpecStageAvailabilityUnavailable || st.Spec.UnavailableReason != gen.SpecStageUnavailableReasonAeStudioUnavailable ||
		st.Spec.DesignOutdated || st.Spec.Exists {
		t.Fatalf("spec = %+v, want unavailable with no git fact", st.Spec)
	}
	if st.Deploy.Components.Total != 2 {
		t.Errorf("deploy total = %d, want 2 (the count read)", st.Deploy.Components.Total)
	}
}

// Any other git failure still fails the poll: the degrade is for the org's AE
// Studio state, not a blanket swallow.
func TestProjectStatus_OtherGitFailureStillFails(t *testing.T) {
	t.Parallel()
	svc := projectServiceWithFake(t, func(fk *aestudiotest.Fake) {
		fk.FailOrg("default", &sourcecontrol.HTTPStatusError{StatusCode: 502})
	})
	if _, err := svc.GetProjectStatus(context.Background(), "default", "p"); err == nil {
		t.Fatal("a non-AE-Studio git failure must fail the poll")
	}
}

// The pre-ready answers (no repo, cloning, repo error) never read git, so
// their spec facts are known-empty rather than unreadable — and the required
// enum must carry a member on every answer, not "".
func TestProjectStatus_PreReadyAnswersAreAvailable(t *testing.T) {
	t.Parallel()
	for _, row := range []*sourcecontrol.GitRepository{
		nil,
		{Status: "cloning"},
		{Status: "error", ErrorMessage: "boom"},
	} {
		repoSvc := &fakeRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return row, nil
		}}
		st, err := NewProjectService(nil, repoSvc, nil, nil, nil).GetProjectStatus(context.Background(), "acme", "web")
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st.Spec.Availability != gen.SpecStageAvailabilityAvailable || st.Spec.UnavailableReason != "" {
			t.Errorf("phase %q: spec = %+v, want availability available", st.Phase, st.Spec)
		}
	}
	st, err := NewProjectService(nil, nil, nil, nil, nil).GetProjectStatus(context.Background(), "acme", "web")
	if err != nil || st.Spec.Availability != gen.SpecStageAvailabilityAvailable {
		t.Fatalf("nil repo service: spec = %+v err = %v, want availability available", st.Spec, err)
	}
}
