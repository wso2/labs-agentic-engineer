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

// UNIT tier — the reference-row predicates of the model connection: what
// counts as a recorded secret (keySet, CodingKeySet) and what coding
// dispatch does when the subscription's reference cannot be read. The
// DB-backed half is model_connection_dbtest_test.go and
// anthropic_dbtest_test.go.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// orgSecretsFailingOn reads rows from rows, but a read of the failOn secret
// fails with err: one secret's row is unreadable, the others are fine.
type orgSecretsFailingOn struct {
	rows   fakeOrgSecrets
	failOn OrgSecret
	err    error
}

func (f orgSecretsFailingOn) Get(ctx context.Context, ocOrgID string, s OrgSecret) (*OrgSecretRef, error) {
	if s == f.failOn {
		return nil, f.err
	}
	return f.rows.Get(ctx, ocOrgID, s)
}

// connWithSubscription is acme's card with a connection and an active Claude
// subscription row.
func connWithSubscription() *memCard {
	card := newMemCard(&saveLog{})
	card.conn = &OrgModelConnection{OcOrgID: "acme", Host: "api.anthropic.com", Model: "claude-sonnet-4-5"}
	card.creds[AnthropicRoleCoding] = &OrgAnthropicCredential{
		OcOrgID: "acme", Role: AnthropicRoleCoding, CredentialKind: AnthropicCredentialOAuth, Status: "active",
	}
	return card
}

// The billing guard: only a reference that was never written falls back to
// the connection's key. A failed read of the coding-agent-key row is an
// error, so a transient database fault never bills the org's API key in
// place of its Claude plan (and logs no fallback WARN).
func TestResolveCodingCredential_FailedReferenceReadIsAnError(t *testing.T) {
	card := connWithSubscription()
	refs := orgSecretsFailingOn{
		rows:   fakeOrgSecrets{"acme/default-key": "acme-default-key-0001"},
		failOn: OrgSecretCodingAgentKey,
		err:    errors.New("db down"),
	}
	svc := NewModelConnectionService(card.connRepo(), card.subRepo(), refs, memNoRates{})

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	cred, err := svc.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err == nil {
		t.Fatalf("a failed coding-agent-key read resolved %+v, want an error", cred)
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want the read failure wrapped", err)
	}
	if cred.Ref.Name == "acme-default-key-0001" {
		t.Fatalf("a failed read handed out the default-key reference: %+v", cred)
	}
	if strings.Contains(buf.String(), `"level":"WARN"`) {
		t.Fatalf("a failed read logged the fallback WARN: %s", buf.String())
	}
}

// A reference row with no name records nothing: recordedRef refuses to mount
// it, so the Settings predicates must not call it set either.
func TestCodingKeySet_EmptyRefNameIsNotSet(t *testing.T) {
	card := connWithSubscription()
	svc := NewModelConnectionService(card.connRepo(), card.subRepo(), fakeOrgSecrets{"acme/coding-agent-key": ""}, memNoRates{})
	set, err := svc.CodingKeySet(context.Background(), "acme")
	if err != nil || set {
		t.Fatalf("CodingKeySet over an empty name = %v, %v; want false, nil", set, err)
	}

	failing := NewModelConnectionService(card.connRepo(), card.subRepo(), failingOrgSecrets{err: errors.New("db down")}, memNoRates{})
	if set, err := failing.CodingKeySet(context.Background(), "acme"); err == nil {
		t.Fatalf("CodingKeySet over a failed read = %v, nil; want an error", set)
	}
}

// The default key follows the same predicate: an empty name is no key.
func TestKeySet_EmptyRefNameIsNotSet(t *testing.T) {
	card := connWithSubscription()
	svc := NewModelConnectionService(card.connRepo(), card.subRepo(), fakeOrgSecrets{"acme/default-key": ""}, memNoRates{})
	set, err := svc.keySet(context.Background(), "acme")
	if err != nil || set {
		t.Fatalf("keySet over an empty name = %v, %v; want false, nil", set, err)
	}
}
