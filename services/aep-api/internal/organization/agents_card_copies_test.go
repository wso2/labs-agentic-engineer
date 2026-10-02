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

// UNIT tier — the AI agents card's copies after commit (syncCopies): made
// under the card's locks from the rows as they stand, so a save's copies that
// run after a later save's leave the later key. The DB-backed half (the
// copies uploaded and stamped through a real transaction) is
// anthropic_dbtest_test.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// standingCard is a card whose rows hold one connection and its key.
type standingCard struct {
	row     *OrgModelConnection
	key     string
	locks   []string
	stamped []SecretRefTriplet
	// commitErr fails the commit after fn ran (its stamps rolled back).
	commitErr error
}

func (c *standingCard) Tx(_ context.Context, fn func(tx AgentsCardTx) error) error {
	if err := fn(standingTx{card: c}); err != nil {
		return err
	}
	return c.commitErr
}

type standingTx struct {
	AgentsCardTx // every other method: never reached
	card         *standingCard
}

func (t standingTx) AdvisoryLock(key string) error {
	t.card.locks = append(t.card.locks, key)
	return nil
}
func (t standingTx) GetConnection(string) (*OrgModelConnection, error) { return t.card.row, nil }
func (t standingTx) Secrets() secrets.CredentialStore                  { return storedKey{key: t.card.key} }
func (t standingTx) StampConnectionSecretRef(_ string, ref SecretRefTriplet) error {
	t.card.stamped = append(t.card.stamped, ref)
	return nil
}

// uploadsSM records the keys uploaded to SM-API.
type uploadsSM struct {
	secretmanagersvc.SecretManagementClient // every other method: never reached
	uploads                                 []string
	deleted                                 []string
}

func (u *uploadsSM) CreateSecretRef(_ context.Context, _ secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	u.uploads = append(u.uploads, data[secretmanagersvc.SecretKeyAPIKey])
	return "acme-default-key-0000aaaa", nil
}

func (u *uploadsSM) DeleteSecretRef(_ context.Context, _ secretmanagersvc.SecretLocation, name string) error {
	u.deleted = append(u.deleted, name)
	return nil
}

// The first connect's copies run after the switch to Ollama committed: they
// upload the Ollama key the rows hold, never the key the first save wrote, so
// the vault never holds a key beside another host's row.
func TestSyncCopies_ALateMirrorUploadsTheKeyAsItStands(t *testing.T) {
	provider := &recordingProvider{}
	sm := &uploadsSM{}
	card := &standingCard{row: holding(onOllama(), "").row, key: "ollama-key-0123456789"}
	writer := NewSecretRefWriter(sm, nil, nil, nil, nil).
		WithOrgSecretWriter(NewOrgSecretWriter(sm, newMemOrgSecretRepo(), memOrgSecretLock{}, time.Now))
	svc := NewAgentSettingsService(nil, nil,
		NewAnthropicCredentialService(nil, nil).WithModelProvider(provider),
		(&ModelConnectionService{}).WithSecretRefWriter(writer), card, everyRuntime)
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "3f0c5d1e-0000-4000-8000-000000000001"})

	svc.syncCopies(ctx, "acme", cardCopies{keyWritten: true, after: firstParty()})

	if len(sm.uploads) != 1 || sm.uploads[0] != "ollama-key-0123456789" {
		t.Fatalf("uploaded %q, want the Ollama key alone", sm.uploads)
	}
	if len(card.stamped) != 1 || card.stamped[0].Name != "acme-default-key-0000aaaa" || card.stamped[0].KVPath == "" {
		t.Fatalf("stamped %+v, want the uploaded copy", card.stamped)
	}
	if len(provider.published) != 1 || provider.published[0].conn != *onOllama() {
		t.Fatalf("published %+v, want the Ollama connection", provider.published)
	}
	if len(card.locks) != len(cardLockPrefixes) {
		t.Fatalf("locks = %v, want the card's locks", card.locks)
	}
}

// The copies' transaction stamped the new reference and then failed to
// commit: the connection row still names the previous reference, so Retire
// must not run. The org secret row is on the new name (the safe split): the
// previous reference stays live, an orphan at worst.
func TestSyncCopies_ARolledBackCopyRetiresNothing(t *testing.T) {
	sm := &uploadsSM{}
	refs := newMemOrgSecretRepo()
	refs.rows[memOrgSecretKey("acme", OrgSecretDefaultKey)] = OrgSecretRef{Secret: OrgSecretDefaultKey, Name: "acme-default-key-previous"}
	card := &standingCard{row: holding(firstParty(), "").row, key: anthropicUnitKey, commitErr: errors.New("commit failed")}
	writer := NewSecretRefWriter(sm, nil, nil, nil, nil).
		WithOrgSecretWriter(NewOrgSecretWriter(sm, refs, memOrgSecretLock{}, time.Now))
	converger := &triggerCount{}
	svc := NewAgentSettingsService(nil, nil, NewAnthropicCredentialService(nil, nil),
		(&ModelConnectionService{}).WithSecretRefWriter(writer), card, everyRuntime).WithStudioConverger(converger)
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "3f0c5d1e-0000-4000-8000-000000000001"})

	svc.syncCopies(ctx, "acme", cardCopies{keyWritten: true, before: firstParty(), after: firstParty()})

	if len(sm.uploads) != 1 || len(card.stamped) != 1 {
		t.Fatalf("uploads=%d stamps=%d, want the write and its stamp to have run", len(sm.uploads), len(card.stamped))
	}
	if len(sm.deleted) != 0 {
		t.Fatalf("deleted %v, want the previous reference kept while the rolled-back row may name it", sm.deleted)
	}
	if converger.n != 1 {
		t.Fatalf("triggers = %d, want the roll regardless: converge reads the rows as they stand", converger.n)
	}
}

type triggerCount struct{ n int }

func (c *triggerCount) Trigger(context.Context, string) { c.n++ }

func TestCardCopies_RollsStudio(t *testing.T) {
	limit := func(n int) *int { return &n }
	for _, tc := range []struct {
		name string
		c    cardCopies
		want bool
	}{
		{"a written key rolls", cardCopies{keyWritten: true, before: firstParty(), after: firstParty()}, true},
		{"a disconnect rolls", cardCopies{forgotKey: "acme-default-key-0000aaaa", before: firstParty()}, true},
		{"a disconnect of a row with no reference rolls", cardCopies{before: firstParty()}, true},
		{"a model change rolls", cardCopies{before: firstParty(),
			after: with(firstParty(), func(c *modelconn.Connection) { c.Model = "claude-haiku-4-5" })}, true},
		{"a context window change rolls", cardCopies{before: firstParty(),
			after: with(firstParty(), func(c *modelconn.Connection) { c.ContextWindow = limit(1) })}, true},
		{"equal limits behind different pointers do not roll", cardCopies{
			before: with(firstParty(), func(c *modelconn.Connection) { c.OutputLimit = limit(8) }),
			after:  with(firstParty(), func(c *modelconn.Connection) { c.OutputLimit = limit(8) })}, false},
		{"a subscription token does not roll", cardCopies{tokenWritten: true, forgotToken: "x", before: firstParty(), after: firstParty()}, false},
		{"a runtime-only save does not roll", cardCopies{before: firstParty(), after: firstParty()}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.rollsStudio(); got != tc.want {
				t.Fatalf("rollsStudio = %v, want %v", got, tc.want)
			}
		})
	}
}
