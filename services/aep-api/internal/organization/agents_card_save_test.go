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

// UNIT tier — one save of the AI agents card (apply) over an in-memory card,
// org secret rows and vault that share one event log, so the order the save
// runs in is observable: the card's lock, each key written to the vault from
// the request, the rows committed inside the last write, then the copies
// (Agent Manager's provider, the retire of the replaced references), all
// before the lock is released. The DB-backed half (the real lock,
// repositories and transaction) is agents_card_refs_test.go and
// anthropic_dbtest_test.go.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
)

// saveLog is the one ordered record every fake below appends to.
type saveLog struct{ events []string }

func (l *saveLog) add(format string, args ...any) {
	l.events = append(l.events, fmt.Sprintf(format, args...))
}
func (l *saveLog) String() string { return strings.Join(l.events, " → ") }

// memCard is the card's rows. Tx stages a copy and applies it on commit, so
// a failed commit leaves the rows as they were.
type memCard struct {
	log       *saveLog
	conn      *OrgModelConnection
	creds     map[AnthropicRole]*OrgAnthropicCredential
	settings  *OrgAgentSettings
	commitErr error
}

func newMemCard(log *saveLog) *memCard {
	return &memCard{log: log, creds: map[AnthropicRole]*OrgAnthropicCredential{}}
}

func (c *memCard) Lock(context.Context, string) (func(), error) {
	c.log.add("lock")
	return func() { c.log.add("unlock") }, nil
}

func (c *memCard) Tx(_ context.Context, fn func(tx AgentsCardTx) error) error {
	staged := &memCard{log: c.log, conn: c.conn, settings: c.settings, creds: map[AnthropicRole]*OrgAnthropicCredential{}}
	for r, row := range c.creds {
		staged.creds[r] = row
	}
	if err := fn(memCardTx{staged}); err != nil {
		return err
	}
	if c.commitErr != nil {
		c.log.add("rollback")
		return c.commitErr
	}
	c.conn, c.creds, c.settings = staged.conn, staged.creds, staged.settings
	c.log.add("commit")
	return nil
}

type memCardTx struct{ c *memCard }

func (t memCardTx) GetCredential(_ string, role AnthropicRole) (*OrgAnthropicCredential, error) {
	return t.c.creds[role], nil
}
func (t memCardTx) UpsertCredential(row *OrgAnthropicCredential) error {
	cp := *row
	t.c.creds[row.Role] = &cp
	return nil
}
func (t memCardTx) DeleteCredential(_ string, role AnthropicRole) error {
	delete(t.c.creds, role)
	return nil
}
func (t memCardTx) GetConnection(string) (*OrgModelConnection, error) { return t.c.conn, nil }
func (t memCardTx) UpsertConnection(row *OrgModelConnection) error {
	cp := *row
	t.c.conn = &cp
	return nil
}
func (t memCardTx) DeleteConnection(string) error                 { t.c.conn = nil; return nil }
func (t memCardTx) GetSettings(string) (*OrgAgentSettings, error) { return t.c.settings, nil }
func (t memCardTx) UpsertSettings(row *OrgAgentSettings) error {
	cp := *row
	t.c.settings = &cp
	return nil
}
func (t memCardTx) DeleteSettings(string) error                   { t.c.settings = nil; return nil }
func (t memCardTx) SetKeyDisconnectedAt(string, *time.Time) error { return nil }
func (c *memCard) connRepo() OrgModelConnectionRepository         { return memConnRepo{c} }
func (c *memCard) subRepo() OrgAnthropicRepository                { return memSubRepo{c} }
func (c *memCard) settingsRepo() OrgAgentSettingsRepository       { return memSettingsRepo{c} }
func (memNoRates) Priced(string, string) bool                     { return false }

type (
	memConnRepo     struct{ c *memCard }
	memSubRepo      struct{ c *memCard }
	memSettingsRepo struct{ c *memCard }
	memNoRates      struct{}
)

func (r memConnRepo) GetByOrg(context.Context, string) (*OrgModelConnection, error) {
	return r.c.conn, nil
}
func (r memSubRepo) GetByOrg(_ context.Context, _ string, role AnthropicRole) (*OrgAnthropicCredential, error) {
	return r.c.creds[role], nil
}
func (r memSettingsRepo) GetByOrg(context.Context, string) (*OrgAgentSettings, error) {
	return r.c.settings, nil
}

// saveVault is the secrets client, logging "vault:<secret>" per new
// reference and "delete:<name>" per delete.
type saveVault struct {
	secretmanagersvc.SecretManagementClient // anything else is a test bug (nil panic)
	log                                     *saveLog
	createErr, deleteErr                    error
	failEntity                              string // a create of this secret fails with createErr
	n                                       int
	live                                    map[string]bool
	data                                    map[string]string // the last write's data
}

func (v *saveVault) CreateSecretRef(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	v.log.add("vault:%s", loc.EntityName)
	if v.createErr != nil && (v.failEntity == "" || v.failEntity == loc.EntityName) {
		return "", v.createErr
	}
	v.n++
	name := fmt.Sprintf("acme-%s-%04d", loc.EntityName, v.n)
	v.live[name], v.data = true, data
	return name, nil
}

func (v *saveVault) DeleteSecretRef(_ context.Context, _ secretmanagersvc.SecretLocation, name string) error {
	v.log.add("delete:%s", name)
	if v.deleteErr != nil {
		return v.deleteErr
	}
	delete(v.live, name)
	return nil
}

// loggingProvider is Agent Manager's provider, logging each publish with
// its key and each clear.
type loggingProvider struct {
	log        *saveLog
	publishErr error
}

func (p *loggingProvider) PublishOrgModelConnection(_ context.Context, _ string, conn modelconn.Connection, key string) error {
	p.log.add("publish:%s:%s", conn.Host, key)
	return p.publishErr
}

func (p *loggingProvider) ClearOrgModelKey(context.Context, string, modelconn.Connection) error {
	p.log.add("clear")
	return nil
}

// saveFixture is one org's card over the fakes above.
type saveFixture struct {
	t     *testing.T
	log   *saveLog
	card  *memCard
	refs  *memOrgSecretRepo
	vault *saveVault
	svc   *AgentSettingsService
	ctx   context.Context
}

func newSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	log := &saveLog{}
	card := newMemCard(log)
	refs := newMemOrgSecretRepo()
	vault := &saveVault{log: log, live: map[string]bool{}}
	writer := NewSecretRefWriter(vault, nil).
		WithOrgSecretWriter(NewOrgSecretWriter(vault, refs, memOrgSecretLock{}, time.Now))
	creds := NewAnthropicCredentialService(card.subRepo()).WithSecretRefWriter(writer).
		WithModelProvider(&loggingProvider{log: log})
	conns := NewModelConnectionService(card.connRepo(), card.subRepo(), refs, memNoRates{}).WithSecretRefWriter(writer)
	svc := NewAgentSettingsService(card.settingsRepo(), nil, creds, conns, card, everyRuntime)
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "3f0c5d1e-0000-4000-8000-000000000001"})
	return &saveFixture{t: t, log: log, card: card, refs: refs, vault: vault, svc: svc, ctx: ctx}
}

// save applies p as the probe phase would hand it over: every connection
// draft "probed" (a fixed result), so apply's own judgement is what runs.
func (f *saveFixture) save(p orgconfig.ConfigPatch) error {
	f.t.Helper()
	state, err := f.svc.currentState(f.ctx, "acme")
	if err != nil {
		f.t.Fatalf("state: %v", err)
	}
	eff, err := judgeCard(state, everyRuntime, p)
	if err != nil {
		return err
	}
	probed := cardProbe{basis: state.conn, draft: eff.writeConn,
		result: ProbeResult{AuthScheme: modelconn.AuthXAPIKey, ImageInput: modelconn.Yes}}
	f.log.events = nil
	return f.svc.apply(f.ctx, "acme", "ada", p, probed)
}

func (f *saveFixture) mustSave(p orgconfig.ConfigPatch) {
	f.t.Helper()
	if err := f.save(p); err != nil {
		f.t.Fatalf("save: %v", err)
	}
}

func (f *saveFixture) ref(s OrgSecret) string {
	row, _ := f.refs.Get(context.Background(), "acme", s)
	if row == nil {
		return ""
	}
	return row.Name
}

const (
	saveKey1  = "sk-ant-api03-SAVEtestKeyOne-0123456789"
	saveKey2  = "sk-ant-api03-SAVEtestKeyTwo-9876543210"
	saveToken = "sk-ant-oat01-SAVEsubscriptionToken-0123"
)

func connectPatch(key string) orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMPatch]{Sent: true, Value: orgconfig.LLMPatch{Kind: "anthropic", APIKey: key}}}
}

func withToken(p orgconfig.ConfigPatch, token string) orgconfig.ConfigPatch {
	p.Agents = patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{
		Subscription: patch.Field[orgconfig.SubscriptionWrite]{Sent: true, Value: orgconfig.SubscriptionWrite{Kind: "claude", Token: token}},
	}}
	return p
}

// A key save writes the request's key to the vault under the card's lock,
// commits the rows inside that write, publishes the request's key, and only
// then retires the reference it replaced; the lock is released last.
func TestApply_WritesTheRequestKeyUnderTheLockThenCommitsPublishesAndRetires(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(connectPatch(saveKey1))
	first := f.ref(OrgSecretDefaultKey)
	if want := "lock → vault:default-key → commit → publish:api.anthropic.com:" + saveKey1 + " → unlock"; f.log.String() != want {
		t.Fatalf("first connect ran\n  %s\nwant\n  %s", f.log, want)
	}

	f.mustSave(connectPatch(saveKey2))
	want := "lock → vault:default-key → commit → publish:api.anthropic.com:" + saveKey2 + " → delete:" + first + " → unlock"
	if f.log.String() != want {
		t.Fatalf("rotation ran\n  %s\nwant\n  %s", f.log, want)
	}
	if f.vault.data["api-key"] != saveKey2 || f.ref(OrgSecretDefaultKey) == first {
		t.Fatalf("the vault holds %v under %s, want the request's key under a new reference", f.vault.data, f.ref(OrgSecretDefaultKey))
	}
}

// A failed vault write fails the save with nothing saved: the rows are not
// written and the previous reference stays the recorded one.
func TestApply_AVaultFailureSavesNothing(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(connectPatch(saveKey1))
	before, conn := f.ref(OrgSecretDefaultKey), *f.card.conn
	f.vault.createErr = errors.New("vault down")

	if err := f.save(connectPatch(saveKey2)); err == nil {
		t.Fatal("a failed vault write must fail the save")
	}
	if strings.Contains(f.log.String(), "commit") || strings.Contains(f.log.String(), "publish") {
		t.Fatalf("a failed vault write went on: %s", f.log)
	}
	if f.ref(OrgSecretDefaultKey) != before || !f.vault.live[before] || *f.card.conn != conn {
		t.Fatalf("the failed save moved something: ref %s → %s, row %+v", before, f.ref(OrgSecretDefaultKey), f.card.conn)
	}
}

// A card transaction that fails after the vault writes undoes them: each new
// reference is deleted and the rows name the previous ones again. Nothing is
// published or retired.
func TestApply_ACardCommitFailureUndoesTheNewReferences(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(withToken(connectPatch(saveKey1), saveToken))
	key, token := f.ref(OrgSecretDefaultKey), f.ref(OrgSecretCodingAgentKey)
	f.card.commitErr = errors.New("commit failed")

	if err := f.save(withToken(connectPatch(saveKey2), saveToken)); err == nil {
		t.Fatal("a failed commit must fail the save")
	}
	want := "lock → vault:default-key → vault:coding-agent-key → rollback → delete:acme-coding-agent-key-0004 → delete:acme-default-key-0003 → unlock"
	if f.log.String() != want {
		t.Fatalf("the failed commit ran\n  %s\nwant\n  %s", f.log, want)
	}
	if f.ref(OrgSecretDefaultKey) != key || f.ref(OrgSecretCodingAgentKey) != token || !f.vault.live[key] || !f.vault.live[token] {
		t.Fatalf("the rows must name the previous references again: %s %s (vault %v)",
			f.ref(OrgSecretDefaultKey), f.ref(OrgSecretCodingAgentKey), f.vault.live)
	}
}

// The push follows every committed key save: no step between the commit and
// the push can skip it. Here the deleted subscription's reference cannot be
// removed (a vault delete failure, logged); the key still reaches the
// provider and the save answers success.
func TestApply_APushIsNeverSkippedAfterACommittedKeySave(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(withToken(connectPatch(saveKey1), saveToken))
	f.vault.deleteErr = errors.New("vault delete failed")

	err := f.save(orgconfig.ConfigPatch{
		LLM:    patch.Field[orgconfig.LLMPatch]{Sent: true, Value: orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: "ollama-save-key-0123456789"}},
		Agents: patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{Runtime: "opencode"}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(f.log.String(), "commit → delete:") || !strings.Contains(f.log.String(), "publish:ollama.com:ollama-save-key-0123456789") {
		t.Fatalf("the committed key save was not pushed after the failed forget: %s", f.log)
	}
}

// A failed push answers AgentManagerNotUpdatedError with the save standing.
func TestApply_AFailedPushIsAgentManagerNotUpdatedWithTheSaveStanding(t *testing.T) {
	f := newSaveFixture(t)
	f.svc.creds.WithModelProvider(&loggingProvider{log: f.log, publishErr: errors.New("amp down")})

	err := f.save(connectPatch(saveKey1))
	var am *AgentManagerNotUpdatedError
	if !errors.As(err, &am) {
		t.Fatalf("err = %v, want AgentManagerNotUpdatedError", err)
	}
	if f.card.conn == nil || f.ref(OrgSecretDefaultKey) == "" {
		t.Fatal("the save must stand: the row and its default-key reference")
	}
}

// A model-only edit carries no key: it writes nothing to the vault and
// publishes nothing.
func TestApply_AModelOnlyEditTouchesNoKey(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(connectPatch(saveKey1))

	f.mustSave(orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMPatch]{Sent: true, Value: orgconfig.LLMPatch{Model: "claude-haiku-4-5"}}})
	if want := "lock → commit → unlock"; f.log.String() != want {
		t.Fatalf("a model-only edit ran %s, want %s", f.log, want)
	}
	if f.card.conn.Model != "claude-haiku-4-5" {
		t.Fatalf("model = %s", f.card.conn.Model)
	}
}

func TestCardCopies_RollsStudio(t *testing.T) {
	limit := func(n int) *int { return &n }
	key := &keyWrite{value: "k"}
	for _, tc := range []struct {
		name string
		c    cardCopies
		want bool
	}{
		{"a written key rolls", cardCopies{key: key, before: firstParty(), after: firstParty()}, true},
		{"a disconnect rolls", cardCopies{forgotKey: true, before: firstParty()}, true},
		{"a model change rolls", cardCopies{before: firstParty(),
			after: with(firstParty(), func(c *modelconn.Connection) { c.Model = "claude-haiku-4-5" })}, true},
		{"a context window change rolls", cardCopies{before: firstParty(),
			after: with(firstParty(), func(c *modelconn.Connection) { c.ContextWindow = limit(1) })}, true},
		{"equal limits behind different pointers do not roll", cardCopies{
			before: with(firstParty(), func(c *modelconn.Connection) { c.OutputLimit = limit(8) }),
			after:  with(firstParty(), func(c *modelconn.Connection) { c.OutputLimit = limit(8) })}, false},
		{"a subscription token does not roll", cardCopies{tokenWrite: &OrgSecretWrite{}, forgotToken: true, before: firstParty(), after: firstParty()}, false},
		{"a runtime-only save does not roll", cardCopies{before: firstParty(), after: firstParty()}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.rollsStudio(); got != tc.want {
				t.Fatalf("rollsStudio = %v, want %v", got, tc.want)
			}
		})
	}
}

// wantStoreRefusal asserts err is the coded 502 a vault refusal answers, on
// section.
func wantStoreRefusal(t *testing.T, err error, section string) {
	t.Helper()
	var se *SectionError
	if !errors.As(sectionErrorFrom("llm", err), &se) || se.Status != http.StatusBadGateway ||
		se.Code != "secret_store_write_failed" || se.Section != section ||
		se.Message != "Key not saved; the secret store did not accept it. Try again." {
		t.Fatalf("err = %#v, want 502 secret_store_write_failed on %s", err, section)
	}
}

// A vault that refuses the key answers a coded 502 on the section whose key
// it refused, and logs one value-free orgsecret.write_failed event naming
// the org, the secret and the reason class. Not parallel: it swaps the
// global logger.
func TestApply_AVaultRefusalIsA502AndOneValueFreeLogLine(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	f := newSaveFixture(t)
	f.vault.createErr = fmt.Errorf("openbao: 403 permission denied for %s", saveKey1)
	err := f.save(connectPatch(saveKey1))
	wantStoreRefusal(t, err, "llm")

	logs := buf.String()
	if n := strings.Count(logs, `"msg":"orgsecret.write_failed"`); n != 1 {
		t.Fatalf("want one orgsecret.write_failed line, got %d:\n%s", n, logs)
	}
	for _, want := range []string{`"org":"acme"`, `"secret":"default-key"`, `"reason":"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("the event must carry %s:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, saveKey1) || strings.Contains(logs, "permission denied") {
		t.Fatalf("the log carries the value or the store's error text:\n%s", logs)
	}
}

// The subscription token's write fails after the connection key's succeeded:
// the outer write is undone (its new reference deleted, the row back on the
// previous one), nothing is committed, and the 502 names agents.
func TestApply_ATokenWriteFailureAfterTheKeyWriteSavesNothing(t *testing.T) {
	f := newSaveFixture(t)
	f.mustSave(withToken(connectPatch(saveKey1), saveToken))
	key, token, conn := f.ref(OrgSecretDefaultKey), f.ref(OrgSecretCodingAgentKey), *f.card.conn
	f.vault.createErr, f.vault.failEntity = errors.New("vault down"), string(OrgSecretCodingAgentKey)

	err := f.save(withToken(connectPatch(saveKey2), saveToken))
	wantStoreRefusal(t, err, "agents")
	want := "lock → vault:default-key → vault:coding-agent-key → delete:acme-default-key-0003 → unlock"
	if f.log.String() != want {
		t.Fatalf("the failed token write ran\n  %s\nwant\n  %s", f.log, want)
	}
	if f.ref(OrgSecretDefaultKey) != key || f.ref(OrgSecretCodingAgentKey) != token || !f.vault.live[key] ||
		f.vault.live["acme-default-key-0003"] || *f.card.conn != conn {
		t.Fatalf("something was saved: %s %s (vault %v)", f.ref(OrgSecretDefaultKey), f.ref(OrgSecretCodingAgentKey), f.vault.live)
	}
}
