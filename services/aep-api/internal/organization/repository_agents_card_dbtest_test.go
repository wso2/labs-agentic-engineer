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

// DBTEST tier: the AI agents card's lock. A save holds it from its first read
// through its vault writes, its transaction and its copies, on a connection
// of its own, so it must hold both of the card's lock names (the earlier
// release takes them inside its transaction, with the same function) and
// drop them on release.

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func TestAgentsCardLock_HoldsBothNamesUntilReleased_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	card := organization.NewAgentsCardRepository(db)
	ctx := context.Background()

	// tryLock asks, from another session, whether name is free; a free one
	// is taken and dropped at once (xact lock in its own transaction).
	tryLock := func(name string) bool {
		t.Helper()
		var free bool
		if err := db.Raw(`SELECT pg_try_advisory_xact_lock(hashtext(?))`, name).Scan(&free).Error; err != nil {
			t.Fatalf("try %s: %v", name, err)
		}
		return free
	}

	unlock, err := card.Lock(ctx, "acme")
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	for _, name := range []string{"org_anthropic:acme", "org_model:acme"} {
		if tryLock(name) {
			t.Fatalf("%s is free while the card is locked", name)
		}
	}
	if !tryLock("org_model:globex") {
		t.Fatal("another org's card is locked too")
	}
	// The save's own transaction runs on another connection while the lock
	// is held: it must not wait on it.
	if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
		_, err := tx.GetConnection("acme")
		return err
	}); err != nil {
		t.Fatalf("tx under the lock: %v", err)
	}

	unlock()
	for _, name := range []string{"org_anthropic:acme", "org_model:acme"} {
		if !tryLock(name) {
			t.Fatalf("%s still held after release", name)
		}
	}
}
