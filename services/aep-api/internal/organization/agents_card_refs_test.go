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

// DBTEST tier (skips under -short; `make test-db` runs it): the AI agents
// card's key saves as org secret references. Every saved Default key and
// subscription token is a new SecretReference recorded in its org_secrets row
// (the real repository and the real advisory lock), no triplet is stamped on
// the connection or subscription rows, the previous reference is deleted by
// its stored name once the save commits, and a save that changes what the AE
// Studio pod reads triggers its converge. The vault is faked at the writer's
// port: it mints names and tracks which exist.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
)

// mintingSM is the secrets client with the org secret writer's two calls
// (CreateSecretRef, DeleteSecretRef) and the SecretRefWriter's in-place
// CreateSecret served by a fakeVault (new name per write, existence tracked).
type mintingSM struct {
	*fakeSMClient
	vault *fakeVault
}

func (m mintingSM) CreateSecretRef(ctx context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	return m.vault.CreateSecretRef(ctx, loc, data)
}

func (m mintingSM) DeleteSecretRef(ctx context.Context, loc secretmanagersvc.SecretLocation, name string) error {
	return m.vault.DeleteSecretRef(ctx, loc, name)
}

func (m mintingSM) CreateSecret(ctx context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	return m.vault.CreateSecret(ctx, loc, data)
}

// countingConverger counts the converges a save triggered.
type countingConverger struct {
	mu       sync.Mutex
	triggers int
}

func (c *countingConverger) Trigger(context.Context, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.triggers++
}

func (c *countingConverger) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.triggers
}

// pathConsumers is the org's ai-agent-model-access reference: it reads the
// key by the vault path it was last pointed at.
type pathConsumers struct {
	mu      sync.Mutex
	path    string // "" = no reference (no direct agent deployed)
	err     error
	repoint int
}

func (c *pathConsumers) RepointModelKey(_ context.Context, _ string, ref organization.SecretRefTriplet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repoint++
	if c.err != nil {
		return c.err
	}
	if c.path != "" {
		c.path = ref.KVPath
	}
	return nil
}

func (c *pathConsumers) current() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path
}

// cardFixture is the card over one Postgres with references written through
// the org secret writer and a converger that counts.
type cardFixture struct {
	t         *testing.T
	card      *cardDB
	vault     *fakeVault
	converger *countingConverger
	consumers *pathConsumers
}

func newCardFixture(t *testing.T, existing ...string) *cardFixture {
	t.Helper()
	c := newCardDB(t, http.StatusOK, existing...)
	converger := &countingConverger{}
	c.settings.WithStudioConverger(converger)
	return &cardFixture{t: t, card: c, vault: c.vault, converger: converger, consumers: c.consumers}
}

func (f *cardFixture) save(p orgconfig.ConfigPatch) {
	f.t.Helper()
	f.card.patch(f.t, "acme", p)
}

// ref is the org secret's row, nil when unset.
func (f *cardFixture) ref(s organization.OrgSecret) *organization.OrgSecretRef {
	f.t.Helper()
	return f.card.ref(f.t, "acme", s)
}

func (f *cardFixture) exists(name string) bool { return f.vault.refs[name] }

func TestCardSave_DefaultKeyNewRefAndRoll(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	r1 := f.ref(organization.OrgSecretDefaultKey)
	f.save(keyPatch(anthropicDBKey2))
	r2 := f.ref(organization.OrgSecretDefaultKey)

	if r1 == nil || r2 == nil || r1.Name == r2.Name {
		t.Fatalf("r1=%+v r2=%+v: a new reference per write", r1, r2)
	}
	if f.exists(r1.Name) || !f.exists(r2.Name) {
		t.Fatalf("vault holds %v: the old reference deleted by name, the new one kept", f.vault.live())
	}
	if f.vault.lastData["api-key"] != anthropicDBKey2 || len(f.vault.lastData) != 1 {
		t.Fatal("the second reference holds the second key under api-key alone")
	}
	if got := f.converger.count(); got != 2 {
		t.Fatalf("triggers = %d, want one per key save", got)
	}
}

func TestCardSave_CodingKeyDoesNotRoll(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	f.save(subscriptionPatch(anthropicDBOAuthToken))

	ref := f.ref(organization.OrgSecretCodingAgentKey)
	if ref == nil || !f.exists(ref.Name) {
		t.Fatalf("coding-agent-key = %+v, want a reference written", ref)
	}
	if got := f.converger.count(); got != 1 {
		t.Fatalf("triggers = %d, want only the key save's: the subscription reaches no pod", got)
	}
	sub, err := f.card.repo.GetByOrg(context.Background(), "acme", organization.AnthropicRoleCoding)
	if err != nil || sub == nil {
		t.Fatalf("subscription row %+v (%v), want one (the coding-agent-key row names the reference)", sub, err)
	}

	f.save(subscriptionPatch(anthropicDBOAuthToken))
	if again := f.ref(organization.OrgSecretCodingAgentKey); again.Name == ref.Name || f.exists(ref.Name) {
		t.Fatalf("a replaced token: new reference %q, the old %q deleted", again.Name, ref.Name)
	}
}

func TestCardSave_DisconnectRemovesRowAndRolls(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	r1 := f.ref(organization.OrgSecretDefaultKey)
	f.save(disconnectPatch())

	if f.ref(organization.OrgSecretDefaultKey) != nil || f.exists(r1.Name) {
		t.Fatalf("O-5: the default-key row and its reference go (vault %v)", f.vault.live())
	}
	if got := f.converger.count(); got != 2 {
		t.Fatalf("triggers = %d, want the pod re-pinned without the key", got)
	}
}

func TestCardSave_RemovedSubscriptionRemovesItsRowWithoutRoll(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	f.save(subscriptionPatch(anthropicDBOAuthToken))
	ref := f.ref(organization.OrgSecretCodingAgentKey)
	f.save(orgconfig.ConfigPatch{Agents: agentsWithoutSubscription()})

	if f.ref(organization.OrgSecretCodingAgentKey) != nil || f.exists(ref.Name) {
		t.Fatalf("the coding-agent-key row and its reference go (vault %v)", f.vault.live())
	}
	if got := f.converger.count(); got != 1 {
		t.Fatalf("triggers = %d, want only the key save's", got)
	}
}

// A save that changes a non-secret field of the connection reaches the pod as
// AE_MODEL_CONNECTION, so it rolls; it writes no new reference. A runtime-only
// save changes nothing the pod reads.
func TestCardSave_ConnectionFieldChangeRollsWithoutAReference(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	writes := f.vault.creates

	f.save(llmPatch(orgconfig.LLMPatch{Model: "claude-haiku-4-5"}))
	if got := f.converger.count(); got != 2 || f.vault.creates != writes {
		t.Fatalf("model change: triggers=%d writes=%d, want a roll and no new reference", got, f.vault.creates-writes)
	}
	f.save(orgconfig.ConfigPatch{Agents: agentsRuntime("opencode")})
	if got := f.converger.count(); got != 2 {
		t.Fatalf("runtime-only save: triggers=%d, want none", got)
	}
}

func agentsWithoutSubscription() patch.Field[orgconfig.AgentsWrite] {
	return patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{
		Subscription: patch.Field[orgconfig.SubscriptionWrite]{Sent: true, Null: true},
	}}
}

func agentsRuntime(r orgconfig.AgentRuntime) patch.Field[orgconfig.AgentsWrite] {
	return patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{Runtime: r}}
}

// A deployed direct agent's ai-agent-model-access reference reads the key by
// its vault path: a key save moves it onto the new reference before the
// previous one is deleted, so ESO's refresh delivers the new key.
func TestCardSave_RepointsTheModelAccessBeforeRetiring(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	r1 := f.ref(organization.OrgSecretDefaultKey)
	f.consumers.path = "user-app-secrets/ns/" + r1.Name // a direct agent reads r1
	var atDelete []string
	f.vault.onDelete = func(name string) {
		if name == r1.Name {
			atDelete = append(atDelete, f.consumers.current())
		}
	}

	f.save(keyPatch(anthropicDBKey2))
	r2 := f.ref(organization.OrgSecretDefaultKey)
	want := f.consumers.current()
	if !strings.HasSuffix(want, "/"+r2.Name) {
		t.Fatalf("model access reads %q, want the new reference's path", want)
	}
	if f.exists(r1.Name) || len(atDelete) != 1 || atDelete[0] != want {
		t.Fatalf("r1 deleted while the model access read %v, want it deleted once, after the repoint", atDelete)
	}
}

// A failed repoint keeps the previous reference: the model access may still
// read it. The save itself stands (the row and the pod on the new one).
func TestCardSave_AFailedModelAccessRepointKeepsThePreviousReference(t *testing.T) {
	t.Parallel()
	f := newCardFixture(t)
	f.save(keyPatch(anthropicUnitKey))
	r1 := f.ref(organization.OrgSecretDefaultKey)
	f.consumers.err = errors.New("oc down")

	f.save(keyPatch(anthropicDBKey2))
	r2 := f.ref(organization.OrgSecretDefaultKey)
	if r2.Name == r1.Name || !f.exists(r1.Name) || !f.exists(r2.Name) {
		t.Fatalf("r1=%s r2=%s vault %v: the new reference recorded, the previous one kept", r1.Name, r2.Name, f.vault.live())
	}
	if f.converger.count() != 2 {
		t.Fatal("the save stands: the row on the new reference and the pod rolled")
	}
}
