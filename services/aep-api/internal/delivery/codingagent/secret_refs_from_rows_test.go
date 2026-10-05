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
// SecretReference names from their org_secrets rows (R7), and only from them:
// an org with no row has no usable reference (the secret lives only in vault,
// and the row is the record that it was written), so dispatch refuses rather
// than reading any other column. Dispatch needs only the name and the key
// (C10): the Job carries SecretKeyRef{Name, Key} and OpenChoreo resolves the
// reference itself.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
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

func TestResolveRunnerSecretRefs_ReadsTheGitHubPATRow(t *testing.T) {
	t.Parallel()
	anthropic, _ := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, fakeOrgSecrets{"acme/github-pat": "acme-github-pat-0000beef"})

	creds, err := e.resolveRunnerSecretRefs(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	if creds.github != (SecretRef{SecretRefName: "acme-github-pat-0000beef", Property: "token"}) {
		t.Fatalf("github = %+v: dispatch must read org_secrets, name + fixed key, no vault path (R7, C10)", creds.github)
	}
}

// No github-pat row: the dispatch fails and names the missing reference; there
// is no other column to read it from.
func TestResolveRunnerSecretRefs_NoGitHubPATRowFails(t *testing.T) {
	t.Parallel()
	anthropic, _ := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, fakeOrgSecrets{})

	_, err := e.resolveRunnerSecretRefs(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err == nil || !strings.Contains(err.Error(), "github-pat") {
		t.Fatalf("err = %v, want a missing github-pat reference", err)
	}
}

func TestNewCodingExecutor_RequiresTheOrgSecretRows(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("NewCodingExecutor without an org secret reader must panic at assembly")
		}
	}()
	NewCodingExecutor(nil, nil, nil, nil, "", nil, nil, nil)
}

// TestPublisherSecretEnv_FromOrgSecretsRow: the runner's publisher token
// (Q-1=A) is mounted from the reference the ae-publisher-client row names.
func TestPublisherSecretEnv_FromOrgSecretsRow(t *testing.T) {
	t.Parallel()
	r := NewIDPPublisherResolver(fakeOrgSecrets{"acme/ae-publisher-client": "acme-ae-publisher-client-1a2b"})
	name, err := r.SecretRefName(context.Background(), "acme")
	if err != nil || name != "acme-ae-publisher-client-1a2b" {
		t.Fatalf("%q %v", name, err)
	}

	anthropic, rows := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, rows).
		WithPublisherCredentials(r, "https://idp.example/oauth2/token")
	env, tokenURL, err := e.publisherSecretEnv(context.Background(), "acme")
	if err != nil || tokenURL != "https://idp.example/oauth2/token" {
		t.Fatalf("publisherSecretEnv: %q %v", tokenURL, err)
	}
	want := []SecretEnvRef{
		{Key: envPublisherClientID, SecretName: "acme-ae-publisher-client-1a2b", SecretKey: "client_id"},
		{Key: envPublisherClientSecret, SecretName: "acme-ae-publisher-client-1a2b", SecretKey: "client_secret"},
	}
	if len(env) != len(want) || env[0] != want[0] || env[1] != want[1] {
		t.Fatalf("publisher env = %+v, want %+v", env, want)
	}
}

// No ae-publisher-client row: the resolver answers no name and the dispatch
// refuses with ErrPublisherCredentialsMissing (no profile column is read).
func TestPublisherSecretEnv_NoRowIsCredentialsMissing(t *testing.T) {
	t.Parallel()
	anthropic, rows := fullSecretRefs()
	e := newCodingDispatchExecutor(anthropic, rows).
		WithPublisherCredentials(NewIDPPublisherResolver(fakeOrgSecrets{}), "https://idp.example/oauth2/token")
	if _, _, err := e.publisherSecretEnv(context.Background(), "acme"); !errors.Is(err, delivery.ErrPublisherCredentialsMissing) {
		t.Fatalf("err = %v, want ErrPublisherCredentialsMissing", err)
	}
}

func TestNewIDPPublisherResolver_RequiresTheOrgSecretRows(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("NewIDPPublisherResolver without an org secret reader must panic at assembly")
		}
	}()
	NewIDPPublisherResolver(nil)
}
