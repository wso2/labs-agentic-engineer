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

package organization_test

// DBTEST tier (skips under -short; `make test-db` runs it): the model
// connection's readers take the SecretReference name from the org_secrets
// row (R7), with the fixed key, so a rotation whose triplet stamp lags never
// hands a consumer the reference the write already deleted. An org with no
// row yet (connected before phase 1) still resolves from its triplet, name
// and key from that one source, and needs no vault path (C10).

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

func TestKeyRef_FollowsARotation(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.card.conns.WithOrgSecrets(f.refs)
	f.save(keyPatch(anthropicUnitKey))
	first := f.ref(organization.OrgSecretDefaultKey).Name
	f.save(keyPatch(anthropicDBKey2))
	row := f.ref(organization.OrgSecretDefaultKey).Name
	if row == first {
		t.Fatalf("the second save did not rotate the default-key reference (%q)", row)
	}

	_, ref, err := f.card.conns.KeyRef(context.Background(), "acme")
	if err != nil || ref.Name != row || ref.Property != "api-key" || !strings.HasSuffix(ref.KVPath, "/"+row) {
		t.Fatalf("KeyRef = %+v, %v; want the row's %q with its vault path", ref, err, row)
	}

	// The triplet stamp lags (still the pre-rotation name): the row wins, and
	// no vault path of another reference is handed out with its name.
	stampConnectionTriplet(t, f.card.connRepo, "acme", first, "user-app-secrets/wc-acme/"+first, "api-key")
	_, ref, err = f.card.conns.KeyRef(context.Background(), "acme")
	if err != nil || ref.Name != row || ref.Property != "api-key" || ref.KVPath != "" {
		t.Fatalf("KeyRef = %+v, %v; want the row's %q, no vault path (R7)", ref, err, row)
	}
}

func TestResolveCodingCredential_ReadsTheRecordedReferences_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c) // triplets acme-anthropic / acme-anthropic-coding: pre-rotation
	refs := organization.NewOrgSecretRepository(c.db)
	c.conns.WithOrgSecrets(refs)
	upsertRef(t, refs, organization.OrgSecretDefaultKey, "acme-default-key-0000beef")
	upsertRef(t, refs, organization.OrgSecretCodingAgentKey, "acme-coding-agent-key-0000cafe")

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil || cred.Kind != organization.CodingCredentialClaudeSubscription ||
		cred.Ref != (organization.SecretRefTriplet{Name: "acme-coding-agent-key-0000cafe", Property: "api-key"}) {
		t.Fatalf("Claude Code = %+v, %v; want the coding-agent-key row (R7)", cred, err)
	}
	cred, err = c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeOpenCode)
	if err != nil || cred.Kind != organization.CodingCredentialConnectionKey ||
		cred.Ref != (organization.SecretRefTriplet{Name: "acme-default-key-0000beef", Property: "api-key"}) {
		t.Fatalf("OpenCode = %+v, %v; want the default-key row (R7)", cred, err)
	}
}

func TestResolveCodingCredential_FallsBackToTheTripletWithoutARow_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c)
	c.conns.WithOrgSecrets(organization.NewOrgSecretRepository(c.db))
	// A pre-phase-1 triplet with no vault path still mounts: dispatch needs
	// only the name and the key (C10).
	stampTriplet(t, c.repo, "acme", "acme-anthropic-coding", "", "token")

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil || cred.Ref != (organization.SecretRefTriplet{Name: "acme-anthropic-coding", Property: "token"}) {
		t.Fatalf("legacy org = %+v, %v; want name and key from its triplet", cred, err)
	}
}

func upsertRef(t *testing.T, refs organization.OrgSecretRepository, s organization.OrgSecret, name string) {
	t.Helper()
	if err := refs.Upsert(context.Background(), "acme", organization.OrgSecretRef{Secret: s, Name: name}, ""); err != nil {
		t.Fatalf("upsert %s row: %v", s, err)
	}
}
