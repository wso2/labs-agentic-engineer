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

package app

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/identity"
)

// toGateCredentials is the single line the test users' logins cross on their way
// from the identity domain to the ticket that publishes them. It has no
// behaviour, which is exactly why it is worth a test: drop a field here and the
// build still succeeds, the gate still closes, and every published row reads
// "unavailable" — so a validation run signs in as nobody and reports every
// role-gated criterion `not_run`, with nothing anywhere saying why.
func TestToGateCredentialsCarriesEveryField(t *testing.T) {
	got := toGateCredentials([]identity.Credential{
		{Username: "test-team-member", Password: "Aep1!alpha", Role: "Team Member", ColdStart: true},
		{Username: "test-trainer", Password: "Aep1!beta", Role: "Trainer"},
	})

	if len(got) != 2 {
		t.Fatalf("got %d credentials, want 2: %+v", len(got), got)
	}
	if got[0].Username != "test-team-member" {
		t.Errorf("username = %q", got[0].Username)
	}
	if got[0].Password != "Aep1!alpha" {
		t.Errorf("password = %q — an empty one publishes as 'unavailable'", got[0].Password)
	}
	if got[0].Role != "Team Member" {
		t.Errorf("role = %q — the agent matches a criterion's role on this column", got[0].Role)
	}
	if !got[0].ColdStart {
		t.Error("coldStart was lost — it is how a criterion with no role picks a login")
	}
	if got[1].ColdStart {
		t.Error("coldStart was set on the account that does not hold it")
	}
	if got[1].Password != "Aep1!beta" {
		t.Errorf("second password = %q", got[1].Password)
	}
}

// Nil rather than an empty slice, so the gate's "no accounts, no table" branch
// reads the same whether the ensure returned nothing or was never asked.
func TestToGateCredentialsMapsNothingToNil(t *testing.T) {
	if got := toGateCredentials(nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
	if got := toGateCredentials([]identity.Credential{}); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}
