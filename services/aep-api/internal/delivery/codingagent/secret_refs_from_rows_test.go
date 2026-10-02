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

package codingagent

// Coding dispatch takes the GitHub PAT's and the publisher client's
// SecretReference names from their org_secrets rows (R7), so a rotation whose
// triplet stamp lags never leaves a Job mounting the reference the write
// already deleted. An org with no row yet (connected before phase 1) still
// resolves from its triplet columns, name and key from that one source. Either
// way dispatch needs only the name and the key (C10): the Job carries
// SecretKeyRef{Name, Key} and OpenChoreo resolves the reference itself.

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// fakeOrgSecrets maps "<org>/<secret>" to the reference name its row records.
type fakeOrgSecrets map[string]string

func (f fakeOrgSecrets) Get(_ context.Context, ocOrgID string, s organization.OrgSecret) (*organization.OrgSecretRef, error) {
	name, ok := f[ocOrgID+"/"+string(s)]
	if !ok {
		return nil, nil
	}
	return &organization.OrgSecretRef{Secret: s, Name: name}, nil
}

// githubTriplet is an org_credentials row whose triplet names name.
func githubTriplet(name, kvPath, property string) *organization.OrgCredential {
	return &organization.OrgCredential{SecretRefName: strPtr(name), SecretRefKVPath: strPtr(kvPath), SecretRefProperty: strPtr(property)}
}

func TestResolveRunnerSecretRefs_ReadsOrgSecretNamesNotStaleTriplets(t *testing.T) {
	t.Parallel()
	anthropic, _ := fullSecretRefs()
	// the triplet columns still hold the pre-rotation name (the stamp lags)
	stale := githubTriplet("acme-github-pat-00000001", "user-app-secrets/wc-acme/acme-github-pat-00000001", "token")
	e := newCodingDispatchExecutor(anthropic, stale).
		WithOrgSecrets(fakeOrgSecrets{"acme/github-pat": "acme-github-pat-0000beef"})

	creds, err := e.resolveRunnerSecretRefs(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	if creds.github != (SecretRef{SecretRefName: "acme-github-pat-0000beef", Property: "token"}) {
		t.Fatalf("github = %+v: dispatch must read org_secrets, name + fixed key, no vault path (R7, C10)", creds.github)
	}
}

func TestResolveRunnerSecretRefs_RowNeedsNoCredentialTriplet(t *testing.T) {
	t.Parallel()
	anthropic, _ := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, nil).
		WithOrgSecrets(fakeOrgSecrets{"acme/github-pat": "acme-github-pat-0000beef"})

	creds, err := e.resolveRunnerSecretRefs(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil || creds.github.SecretRefName != "acme-github-pat-0000beef" {
		t.Fatalf("%+v %v: a row alone resolves the PAT", creds.github, err)
	}
}

func TestResolveRunnerSecretRefs_FallsBackToTheTripletWithoutARow(t *testing.T) {
	t.Parallel()
	anthropic, _ := fullSecretRefs()
	// A pre-phase-1 triplet: its own key (api-key), and no vault path is needed (C10).
	e := newCodingDispatchExecutor(anthropic, githubTriplet("github-pat-secrets", "", "api-key")).
		WithOrgSecrets(fakeOrgSecrets{})

	creds, err := e.resolveRunnerSecretRefs(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil || creds.github.SecretRefName != "github-pat-secrets" || creds.github.Property != "api-key" {
		t.Fatalf("legacy org: %+v %v (name and key from the same triplet, C10)", creds.github, err)
	}
}

func TestPublisherResolver_ReadsTheAePublisherClientRow(t *testing.T) {
	t.Parallel()
	stale := "acme-ae-publisher-client-00000001"
	r := NewIDPPublisherResolver(fakeIDPRepo{profile: &organization.OrganizationIDPProfile{SecretRefName: &stale}},
		fakeOrgSecrets{"acme/ae-publisher-client": "acme-ae-publisher-client-1a2b"})
	if name, err := r.SecretRefName(context.Background(), "acme"); err != nil || name != "acme-ae-publisher-client-1a2b" {
		t.Fatalf("%q %v: dispatch must read the ae-publisher-client row (R7)", name, err)
	}
}

func TestPublisherResolver_RowNeedsNoProfile(t *testing.T) {
	t.Parallel()
	r := NewIDPPublisherResolver(nil, fakeOrgSecrets{"acme/ae-publisher-client": "acme-ae-publisher-client-1a2b"})
	if name, err := r.SecretRefName(context.Background(), "acme"); err != nil || name != "acme-ae-publisher-client-1a2b" {
		t.Fatalf("%q %v", name, err)
	}
}
