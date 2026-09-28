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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// standingCard is a card whose rows hold one connection and its key.
type standingCard struct {
	row     *OrgModelConnection
	key     string
	locks   []string
	stamped []SecretRefTriplet
}

func (c *standingCard) Tx(_ context.Context, fn func(tx AgentsCardTx) error) error {
	return fn(standingTx{card: c})
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
}

func (u *uploadsSM) CreateSecret(_ context.Context, _ secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	u.uploads = append(u.uploads, data[secretmanagersvc.SecretKeyAPIKey])
	return modelKeyRefName, nil
}

// The first connect's copies run after the switch to Ollama committed: they
// upload the Ollama key the rows hold, never the key the first save wrote, so
// the vault never holds a key beside another host's row.
func TestSyncCopies_ALateMirrorUploadsTheKeyAsItStands(t *testing.T) {
	provider := &recordingProvider{}
	sm := &uploadsSM{}
	card := &standingCard{row: holding(onOllama(), "").row, key: "ollama-key-0123456789"}
	writer := NewSecretRefWriter(sm, nil, nil, nil, nil)
	svc := NewAgentSettingsService(nil, nil,
		NewAnthropicCredentialService(nil, nil).WithModelProvider(provider),
		(&ModelConnectionService{}).WithSecretRefWriter(writer), card, everyRuntime)
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "3f0c5d1e-0000-4000-8000-000000000001"})

	svc.syncCopies(ctx, "acme", cardCopies{keyWritten: true, after: firstParty()})

	if len(sm.uploads) != 1 || sm.uploads[0] != "ollama-key-0123456789" {
		t.Fatalf("uploaded %q, want the Ollama key alone", sm.uploads)
	}
	if len(card.stamped) != 1 || card.stamped[0].Name != modelKeyRefName || card.stamped[0].KVPath == "" {
		t.Fatalf("stamped %+v, want the uploaded copy", card.stamped)
	}
	if len(provider.published) != 1 || provider.published[0].conn != *onOllama() {
		t.Fatalf("published %+v, want the Ollama connection", provider.published)
	}
	if len(card.locks) != len(cardLockPrefixes) {
		t.Fatalf("locks = %v, want the card's locks", card.locks)
	}
}
