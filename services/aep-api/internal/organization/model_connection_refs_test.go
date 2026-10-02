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

	// The triplet stamp lags (still the pre-rotation name): a mount gets the
	// row's name and key, never a vault path.
	stampConnectionTriplet(t, f.card.connRepo, "acme", first, "user-app-secrets/wc-acme/"+first, "api-key")
	_, ref, err := f.card.conns.KeyRef(context.Background(), "acme")
	if err != nil || ref != (organization.SecretRefTriplet{Name: row, Property: "api-key"}) {
		t.Fatalf("KeyRef = %+v, %v; want the row's %q, name + key only (R7, C10)", ref, err, row)
	}
}

// The model access reads the stamped triplet whole. A card save whose row
// write succeeded but whose stamp rolled back leaves the default-key row on
// the new reference B and the triplet on A, which is never retired: KeyPathRef
// stays on A. A committed save moves it to that save's reference.
func TestKeyPathRef_FollowsTheCommittedStamp(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.card.conns.WithOrgSecrets(f.refs)
	f.save(keyPatch(anthropicUnitKey))
	a := f.ref(organization.OrgSecretDefaultKey).Name
	aPath := derefStr(f.card.row(t, "acme").SecretRefKVPath)

	// The state a rolled-back card transaction leaves after a successful row
	// write: the row names B, the stamp (and A itself) stay.
	b := "acme-default-key-0000b0b0"
	if err := f.refs.Upsert(context.Background(), "acme", organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: b}, a); err != nil {
		t.Fatalf("row write: %v", err)
	}
	_, ref, err := f.card.conns.KeyPathRef(context.Background(), "acme")
	if err != nil || ref != (organization.SecretRefTriplet{Name: a, KVPath: aPath, Property: "api-key"}) || !f.exists(a) {
		t.Fatalf("KeyPathRef = %+v, %v (A exists: %v); want the live stamped A %q at %q", ref, err, f.exists(a), a, aPath)
	}

	f.save(keyPatch(anthropicDBKey2))
	committed := derefStr(f.card.row(t, "acme").SecretRefName)
	if committed == a || committed == "" {
		t.Fatalf("the committed save stamped %q, want a new reference", committed)
	}
	_, ref, err = f.card.conns.KeyPathRef(context.Background(), "acme")
	if err != nil || ref.Name != committed || !strings.HasSuffix(ref.KVPath, "/"+committed) || ref.Property != "api-key" || !f.exists(committed) {
		t.Fatalf("KeyPathRef = %+v, %v; want the committed save's %q with its path", ref, err, committed)
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
