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
// row, with the fixed key; the ai-agent model access takes the vault
// path from that SecretReference's spec (names and paths, never a value).
// There is no fallback to the pre-reference-row triplet columns.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// vaultSecretRefs answers GetSecretReference for the references the fake
// vault holds: spec.data maps each key to the vault path the reference was
// created at (a path the readers must take as given, never derive).
type vaultSecretRefs struct{ vault *fakeVault }

func (r vaultSecretRefs) GetSecretReference(_ context.Context, cpNS, name string) (*secretmanagersvc.SecretReference, error) {
	if !r.vault.refs[name] {
		return nil, secretmanagersvc.ErrNotFound
	}
	return &secretmanagersvc.SecretReference{Namespace: cpNS, Name: name, Data: []secretmanagersvc.SecretReferenceData{
		{SecretKey: "api-key", RemoteKey: "kv/elsewhere/" + name, Property: "api-key"},
	}}, nil
}

func TestKeyRef_FollowsARotation(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)
	first := c.ref(t, "acme", organization.OrgSecretDefaultKey).Name
	c.connect(t, "acme", anthropicDBKey2)
	row := c.ref(t, "acme", organization.OrgSecretDefaultKey).Name
	if row == first {
		t.Fatalf("the second save did not rotate the default-key reference (%q)", row)
	}
	_, ref, err := c.conns.KeyRef(context.Background(), "acme")
	if err != nil || ref != (organization.SecretRefTriplet{Name: row, Property: "api-key"}) {
		t.Fatalf("KeyRef = %+v, %v; want the row's %q, name + key only (R7, C10)", ref, err, row)
	}
}

// KeyPathRef reads no triplet column. The name is the default-key row's,
// the vault path is the one that reference's spec.data reads its api-key
// from. A save whose vault write failed saved nothing, so the path stays the
// previous save's; a reference that is gone fails closed, value-free.
func TestKeyPathRef_ResolvesThePathFromTheSecretReference_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.conns.WithSecretReferences(vaultSecretRefs{vault: c.vault})
	c.connect(t, "acme", anthropicUnitKey)
	a := c.ref(t, "acme", organization.OrgSecretDefaultKey).Name

	_, ref, err := c.conns.KeyPathRef(context.Background(), "acme")
	if err != nil || ref != (organization.SecretRefTriplet{Name: a, KVPath: "kv/elsewhere/" + a, Property: "api-key"}) {
		t.Fatalf("KeyPathRef = %+v, %v; want %s with the path its SecretReference reads", ref, err, a)
	}

	c.vault.createErr = errors.New("vault unavailable")
	if _, err := c.config.Patch(c.ctx, "acme", "ada", keyPatch(anthropicDBKey2)); err == nil {
		t.Fatal("a failed vault write must fail the save")
	}
	if _, ref, err := c.conns.KeyPathRef(context.Background(), "acme"); err != nil || ref.Name != a {
		t.Fatalf("after a failed save KeyPathRef = %+v, %v; want the previous save's %s", ref, err, a)
	}

	delete(c.vault.refs, a)
	_, ref, err = c.conns.KeyPathRef(context.Background(), "acme")
	if err == nil || strings.Contains(err.Error(), anthropicUnitKey) || strings.Contains(err.Error(), anthropicDBKey2) {
		t.Fatalf("KeyPathRef on a missing SecretReference = %+v, %v; want a value-free fail-closed error", ref, err)
	}
}

// No SecretReference reader: KeyPathRef refuses rather than derive a path.
func TestKeyPathRef_WithoutAReaderFailsClosed_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)
	if _, ref, err := c.conns.KeyPathRef(context.Background(), "acme"); err == nil {
		t.Fatalf("KeyPathRef = %+v, want an error with no SecretReference reader", ref)
	}
}

// An org whose reference row is missing (saved before the rows existed)
// has no other source for its key's reference: dispatch fails closed.
func TestResolveCodingCredential_WithoutARowFailsClosed_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)
	dropRow(t, c, organization.OrgSecretDefaultKey)

	if cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeOpenCode); err == nil {
		t.Fatalf("a triplet without its row resolved: %+v", cred)
	}
}
