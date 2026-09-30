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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// handoffTokenKey is the org_secrets key of the org's SRE handoff token: the
// bearer the SRE agent presents to aep-mcp-server (AEP_MCP_TOKEN).
const handoffTokenKey = "sre/handoff-token"

// handoffTokenBytes is the token's entropy, before hex encoding.
const handoffTokenBytes = 32

// Tokens holds each org's SRE handoff token. The token is never logged.
type Tokens interface {
	// Ensure returns the org's token, minting and storing one on first use.
	Ensure(ctx context.Context, org string) (string, error)
	// Get returns the org's token; ok=false when none was minted yet.
	Get(ctx context.Context, org string) (token string, ok bool, err error)
}

// NewTokens keeps the tokens in store (org_secrets).
func NewTokens(store secrets.CredentialStore) Tokens {
	return storeTokens{store: store}
}

type storeTokens struct {
	store secrets.CredentialStore
}

func (t storeTokens) Ensure(ctx context.Context, org string) (string, error) {
	tok, ok, err := t.Get(ctx, org)
	if err != nil || ok {
		return tok, err
	}
	raw := make([]byte, handoffTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("sre handoff token: mint: %w", err)
	}
	tok = hex.EncodeToString(raw)
	if err := t.store.Put(ctx, org, handoffTokenKey, []byte(tok)); err != nil {
		return "", fmt.Errorf("sre handoff token: store: %w", err)
	}
	return tok, nil
}

func (t storeTokens) Get(ctx context.Context, org string) (string, bool, error) {
	b, err := t.store.Get(ctx, org, handoffTokenKey)
	switch {
	case errors.Is(err, secrets.ErrSecretNotFound):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("sre handoff token: read: %w", err)
	case len(b) == 0:
		return "", false, nil
	}
	return string(b), true, nil
}
