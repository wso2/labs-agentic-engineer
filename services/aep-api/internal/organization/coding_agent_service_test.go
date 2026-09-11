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

package organization

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// fakeCodingAgentRepo is an in-memory OrgCodingAgentRepository. The service's
// whole job is resolution and validation, so the storage is not what these
// tests are about — but "no row" has to be a real state, because it is the one
// every organization starts in.
type fakeCodingAgentRepo struct {
	rows    map[string]*OrgCodingAgentSetting
	getErr  error
	deletes []string
}

func newFakeCodingAgentRepo() *fakeCodingAgentRepo {
	return &fakeCodingAgentRepo{rows: map[string]*OrgCodingAgentSetting{}}
}

func (f *fakeCodingAgentRepo) GetByOrg(_ context.Context, org string) (*OrgCodingAgentSetting, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.rows[org], nil
}

func (f *fakeCodingAgentRepo) Upsert(_ context.Context, row *OrgCodingAgentSetting) error {
	copied := *row
	f.rows[row.OcOrgID] = &copied
	return nil
}

func (f *fakeCodingAgentRepo) DeleteByOrg(_ context.Context, org string) error {
	f.deletes = append(f.deletes, org)
	delete(f.rows, org)
	return nil
}

func fixedClock() func() time.Time {
	at := time.Date(2026, 9, 7, 10, 30, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func newCodingAgentSvc(repo OrgCodingAgentRepository) *CodingAgentService {
	return NewCodingAgentService(repo).WithClock(fixedClock())
}

// An org that has never opened the setting is not "unconfigured" — it is on the
// platform defaults, which is a complete answer. The null author is what the
// console reads to tell that apart from an org that chose the same values.
func TestCodingAgent_NoRowIsThePlatformDefaults(t *testing.T) {
	svc := newCodingAgentSvc(newFakeCodingAgentRepo())

	got, err := svc.Effective(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.Runtime != orgconfig.DefaultAgentRuntime || got.Model != orgconfig.DefaultCodingAgentModel {
		t.Errorf("Effective = %+v, want the platform defaults", got)
	}
	if got.UpdatedBy != nil || got.UpdatedAt != nil {
		t.Errorf("the defaults claim an author: %+v", got)
	}
}

func TestCodingAgent_SetThenEffectiveCarriesTheChoiceAndItsAuthor(t *testing.T) {
	repo := newFakeCodingAgentRepo()
	svc := newCodingAgentSvc(repo)

	if err := svc.Set(context.Background(), "acme", "anjanas@wso2.com",
		orgconfig.CodingAgentWrite{Runtime: "claude-code", Model: "claude-haiku-4-5"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := svc.Effective(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if got.Model != "claude-haiku-4-5" || got.Runtime != "claude-code" {
		t.Errorf("Effective = %+v", got)
	}
	if got.UpdatedBy == nil || *got.UpdatedBy != "anjanas@wso2.com" {
		t.Errorf("updatedBy = %v, want the actor from the JWT", got.UpdatedBy)
	}
	if got.UpdatedAt == nil || !got.UpdatedAt.Equal(fixedClock()()) {
		t.Errorf("updatedAt = %v", got.UpdatedAt)
	}
}

// The section is the ONE place a field may be omitted, because an org tunes its
// model far more often than it moves runtime. An omitted field has to resolve
// against what the org ACTUALLY has — resolving it against the default would
// silently move an org back off a runtime it had chosen.
func TestCodingAgent_AnOmittedFieldKeepsWhatTheOrgAlreadyHas(t *testing.T) {
	repo := newFakeCodingAgentRepo()
	repo.rows["acme"] = &OrgCodingAgentSetting{
		OcOrgID: "acme", Runtime: "claude-code", Model: "claude-haiku-4-5",
		UpdatedBy: "someone", UpdatedAt: fixedClock()(),
	}
	svc := newCodingAgentSvc(repo)

	if err := svc.Set(context.Background(), "acme", "other", orgconfig.CodingAgentWrite{Model: "claude-sonnet-5"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, _ := svc.Effective(context.Background(), "acme")
	if got.Runtime != "claude-code" {
		t.Errorf("runtime = %q, want the org's existing runtime", got.Runtime)
	}
	if got.Model != "claude-sonnet-5" {
		t.Errorf("model = %q, want the one just sent", got.Model)
	}
}

// Reset DELETES rather than writing the defaults, so `updatedBy` goes back to
// null and the console can say "on the platform defaults" truthfully. Writing
// the default values would leave a row claiming somebody chose them.
func TestCodingAgent_ResetRemovesTheRowRatherThanWritingTheDefaults(t *testing.T) {
	repo := newFakeCodingAgentRepo()
	svc := newCodingAgentSvc(repo)
	if err := svc.Set(context.Background(), "acme", "a", orgconfig.CodingAgentWrite{Model: "claude-haiku-4-5"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := svc.Reset(context.Background(), "acme"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, ok := repo.rows["acme"]; ok {
		t.Error("Reset left a row behind, so the org now looks like it chose the defaults")
	}
	got, _ := svc.Effective(context.Background(), "acme")
	if got.UpdatedBy != nil {
		t.Errorf("after Reset the setting still claims an author: %v", got.UpdatedBy)
	}
	// Idempotent: "put me back on the defaults" is satisfied either way.
	if err := svc.Reset(context.Background(), "acme"); err != nil {
		t.Fatalf("second Reset: %v", err)
	}
}

// The runtime is in the contract's enum, and this platform still cannot run it.
// Silently substituting the one we do have would bill an organization for a
// runtime it did not choose and never tell it, so the refusal names both what is
// missing and what is available.
func TestCodingAgent_OpenCodeIsRefusedByNameWithAReason(t *testing.T) {
	svc := newCodingAgentSvc(newFakeCodingAgentRepo())

	err := svc.Validate(context.Background(), "acme", orgconfig.CodingAgentWrite{Runtime: "opencode"})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Validate(opencode) = %v, want a ValidationError (422 at the edge)", err)
	}
	if ve.Code != "coding_agent_runtime_unavailable" {
		t.Errorf("code = %q", ve.Code)
	}
	for _, want := range []string{"opencode", "claude-code"} {
		if !strings.Contains(ve.Message, want) {
			t.Errorf("message %q does not name %q", ve.Message, want)
		}
	}
}

// "Does not exist" and "exists but this build cannot run it" are different
// facts about different mistakes, and a client that gets one message for both
// cannot tell a typo from a platform gap.
func TestCodingAgent_AnInventedRuntimeIsADifferentFailureFromAnUnavailableOne(t *testing.T) {
	svc := newCodingAgentSvc(newFakeCodingAgentRepo())

	err := svc.Validate(context.Background(), "acme", orgconfig.CodingAgentWrite{Runtime: "cursor"})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "coding_agent_runtime_unknown" {
		t.Fatalf("Validate(cursor) = %v, want coding_agent_runtime_unknown", err)
	}
}

// Only models the platform can PRICE are offered — cost stamping is
// all-or-nothing across a cycle's capture, so one unpriced model blanks the
// whole cycle's cost rather than just its own share.
func TestCodingAgent_AModelThePlatformCannotPriceIsRefused(t *testing.T) {
	svc := newCodingAgentSvc(newFakeCodingAgentRepo())

	err := svc.Validate(context.Background(), "acme", orgconfig.CodingAgentWrite{Model: "claude-opus-5"})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "coding_agent_model_unknown" {
		t.Fatalf("Validate(claude-opus-5) = %v, want coding_agent_model_unknown", err)
	}
	if !strings.Contains(ve.Message, "claude-sonnet-5") {
		t.Errorf("message %q does not say what IS on offer", ve.Message)
	}
}

// Validate is the section's share of the endpoint's never-half-apply promise:
// a rejected value must leave nothing written, and the check happens before any
// section is persisted.
func TestCodingAgent_ARejectedValueWritesNothing(t *testing.T) {
	repo := newFakeCodingAgentRepo()
	svc := newCodingAgentSvc(repo)

	if err := svc.Set(context.Background(), "acme", "a", orgconfig.CodingAgentWrite{Runtime: "opencode"}); err == nil {
		t.Fatal("Set(opencode) succeeded")
	}
	if len(repo.rows) != 0 {
		t.Errorf("a rejected Set still wrote %d row(s)", len(repo.rows))
	}
}

// A read failure is not "no row". Falling back to the defaults here would launch
// a run on a model the org may have moved off, and bill it, without ever saying
// so — see the dispatcher's codingAgentEnv for the other half of this rule.
func TestCodingAgent_AReadFailureIsAnErrorNotTheDefaults(t *testing.T) {
	repo := newFakeCodingAgentRepo()
	repo.getErr = errors.New("connection refused")
	svc := newCodingAgentSvc(repo)

	if _, err := svc.Effective(context.Background(), "acme"); err == nil {
		t.Fatal("Effective swallowed a storage failure and answered with the defaults")
	}
}
