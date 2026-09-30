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

package sreagent

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// memStore is a CredentialStore over a map, keyed org + "|" + key.
type memStore struct {
	m      map[string][]byte
	getErr error
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, org, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	v, ok := s.m[org+"|"+key]
	if !ok {
		return nil, secrets.ErrSecretNotFound
	}
	return v, nil
}

func (s *memStore) Put(_ context.Context, org, key string, v []byte) error {
	s.m[org+"|"+key] = v
	return nil
}

func (s *memStore) Delete(_ context.Context, org, key string) error {
	delete(s.m, org+"|"+key)
	return nil
}

func TestTokens_EnsureMintsOnceAndStores(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	tok := NewTokens(store)

	if _, ok, err := tok.Get(ctx, "acme"); err != nil || ok {
		t.Fatalf("Get before Ensure: ok=%v err=%v, want none", ok, err)
	}
	first, err := tok.Ensure(ctx, "acme")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if raw, err := hex.DecodeString(first); err != nil || len(raw) != 32 {
		t.Fatalf("token %d chars, want 32 random bytes hex-encoded", len(first))
	}
	if string(store.m["acme|sre/handoff-token"]) != first {
		t.Fatal("the token is not stored under org_secrets key sre/handoff-token")
	}
	again, err := tok.Ensure(ctx, "acme")
	if err != nil || again != first {
		t.Fatal("a second Ensure must return the stored token, not mint another")
	}
	got, ok, err := tok.Get(ctx, "acme")
	if err != nil || !ok || got != first {
		t.Fatalf("Get after Ensure: ok=%v err=%v, want the stored token", ok, err)
	}
	other, err := tok.Ensure(ctx, "globex")
	if err != nil || other == first {
		t.Fatal("each org gets a token of its own")
	}
}

func TestTokens_StoreErrorIsNotAMint(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	store.getErr = errors.New("db down")
	tok := NewTokens(store)
	if _, err := tok.Ensure(ctx, "acme"); err == nil {
		t.Fatal("Ensure over a failing read: want an error, not a fresh token")
	}
	if len(store.m) != 0 {
		t.Fatal("a failing read must not overwrite the stored token")
	}
	if _, _, err := tok.Get(ctx, "acme"); err == nil {
		t.Fatal("Get over a failing read: want an error")
	}
}
