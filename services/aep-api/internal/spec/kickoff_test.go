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

package spec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// UNIT tier for the kickoff over the in-memory pod (aestudiotest.Fake): the
// `/start` turn is a pod turn now, keyed by a deterministic turn id, guarded
// by the finished-turn ledger.

const (
	kickoffOrg  = "default"
	kickoffProj = "p"
)

// kickoffRepos serves one project's repository row (nil when absent).
type kickoffRepos struct{ row *sourcecontrol.GitRepository }

func (r kickoffRepos) GetByOrgAndProjectID(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if r.row == nil || r.row.OrgID != org || r.row.ProjectID != project {
		return nil, nil
	}
	return r.row, nil
}

// kickoffLedger answers Newest from a fixed row (nil: the project never ran
// a turn).
type kickoffLedger struct{ newest *spec.AgentTurn }

func (l kickoffLedger) Newest(context.Context, string, string) (*spec.AgentTurn, error) {
	return l.newest, nil
}

func newKickoff(ledger kickoffLedger) (*spec.KickoffService, *aestudiotest.Fake) {
	f := aestudiotest.New()
	row := &sourcecontrol.GitRepository{OrgID: kickoffOrg, ProjectID: kickoffProj, RepoURL: "https://github.com/acme/greeter"}
	return spec.NewKickoffService(f, kickoffRepos{row: row}, ledger), f
}

// drain reads a started kickoff's stream to its end, as Kickoff does.
func drain(t *testing.T, events func(func(aestudiotools.TurnEvent, error) bool)) {
	t.Helper()
	for _, err := range events {
		if err != nil {
			t.Fatalf("kickoff stream: %v", err)
		}
	}
}

// The kickoff's turn id is uuidv5(org/project) under the fixed namespace, so
// a retried kickoff (a create, then the references upload it held for; a
// retried upload) reattaches to the one interview instead of starting a
// second (Review Focus 3).
func TestKickoff_EveryAttemptCarriesTheSameTurnID(t *testing.T) {
	svc, f := newKickoff(kickoffLedger{})
	ctx := t.Context()

	for range 2 {
		turnID, events, err := svc.StartKickoff(ctx, kickoffOrg, kickoffProj)
		if err != nil {
			t.Fatalf("StartKickoff: %v", err)
		}
		drain(t, events)
		want := uuid.NewSHA1(uuid.MustParse("6f1a2c1e-6c39-4f0e-9a51-7a1d0e5a6b10"), []byte(kickoffOrg+"/"+kickoffProj)).String()
		if turnID != want {
			t.Fatalf("turn id = %q, want %q", turnID, want)
		}
	}
	calls := f.TurnCalls()
	if len(calls) != 2 || calls[0].Request.TurnID != calls[1].Request.TurnID {
		t.Fatalf("pod saw %+v, want two starts with one turn id", calls)
	}
	got := calls[0]
	if got.Ref != (aestudiotools.RepoRef{Org: kickoffOrg, Owner: "acme", Repo: "greeter", DefaultBranch: "main"}) {
		t.Fatalf("ref = %+v, want the project's repository in the org", got.Ref)
	}
	if got.Request.Kind != aestudiotools.TurnKindStart || got.Request.Project != kickoffProj {
		t.Fatalf("request = %+v, want a start turn for %q", got.Request, kickoffProj)
	}
}

// A different project is a different interview: the id is per project.
func TestKickoff_TurnIDIsPerProject(t *testing.T) {
	svc, f := newKickoff(kickoffLedger{})
	other := &sourcecontrol.GitRepository{OrgID: kickoffOrg, ProjectID: "q", RepoURL: "https://github.com/acme/other"}
	svc2 := spec.NewKickoffService(f, kickoffRepos{row: other}, kickoffLedger{})

	a, ev, err := svc.StartKickoff(t.Context(), kickoffOrg, kickoffProj)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ev)
	b, ev, err := svc2.StartKickoff(t.Context(), kickoffOrg, "q")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ev)
	if a == b {
		t.Fatalf("two projects share kickoff turn id %q", a)
	}
}

// A project whose ledger holds a turn has had its interview: the kickoff is
// refused before anything reaches the pod.
func TestKickoff_RefusedOnceTheProjectRanATurn(t *testing.T) {
	svc, f := newKickoff(kickoffLedger{newest: &spec.AgentTurn{ID: uuid.NewString()}})

	if _, _, err := svc.StartKickoff(t.Context(), kickoffOrg, kickoffProj); !errors.Is(err, spec.ErrKickoffAlreadyRan) {
		t.Fatalf("err = %v, want ErrKickoffAlreadyRan", err)
	}
	svc.Kickoff(t.Context(), kickoffOrg, kickoffProj)
	if n := len(f.TurnCalls()); n != 0 {
		t.Fatalf("pod saw %d turn starts, want none", n)
	}
}

// The turn is credited to the verified caller of the create (or upload)
// request — claims the JWT middleware verified, never the raw header.
func TestKickoff_CreditsTheVerifiedCaller(t *testing.T) {
	svc, f := newKickoff(kickoffLedger{})
	ctx := auth.WithClaims(t.Context(), &auth.Claims{Subject: "u-7", Name: "Ada Lovelace", Email: "ada@acme.io"})

	svc.Kickoff(ctx, kickoffOrg, kickoffProj)

	calls := f.TurnCalls()
	if len(calls) != 1 {
		t.Fatalf("pod saw %d turn starts, want 1", len(calls))
	}
	if want := (aestudiotools.Credit{UserID: "u-7", Name: "Ada Lovelace", Email: "ada@acme.io"}); calls[0].Request.Credit != want {
		t.Fatalf("credit = %+v, want %+v", calls[0].Request.Credit, want)
	}
}

// Kickoff never fails the creation it rides: a pod that cannot take the turn
// leaves the project un-started, which the spec view offers to begin.
func TestKickoff_SwallowsAPodFailure(t *testing.T) {
	svc, f := newKickoff(kickoffLedger{})
	f.FailOp(aestudiotest.OpStartTurn, aestudiotools.ErrAEStudioUnavailable)

	if _, _, err := svc.StartKickoff(t.Context(), kickoffOrg, kickoffProj); !errors.Is(err, aestudiotools.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want ErrAEStudioUnavailable", err)
	}
	svc.Kickoff(t.Context(), kickoffOrg, kickoffProj) // returns; nothing to assert but no panic
}

// A project with no repository row has nothing to start a turn in.
func TestKickoff_ProjectWithoutARepository(t *testing.T) {
	svc, f := newKickoff(kickoffLedger{})

	if _, _, err := svc.StartKickoff(t.Context(), kickoffOrg, "missing"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("err = %v, want ErrRepoNotFound", err)
	}
	if n := len(f.TurnCalls()); n != 0 {
		t.Fatalf("pod saw %d turn starts, want none", n)
	}
}
