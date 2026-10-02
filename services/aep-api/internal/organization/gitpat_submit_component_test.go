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

// COMPONENT tier, Component+DB flavor: the real organization.Service gitpat
// submit (PATCH /config {gitProvider}) over the real credential, IDP and
// org secret services on a pristine Postgres (real rows, real advisory
// lock), with GitHub, the secrets client, Thunder and the AE Studio
// converger faked at their edges. One call log across the fakes shows the
// order the submit runs its steps in. Self-skips under -short.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

const submitThunderSecret = "thunder-issued-once"

// submitLog is the call log the fakes share. A step repeated back to back is
// recorded once: the PAT is probed by the submit and validated again inside
// Connect, which is one "validate" step.
type submitLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *submitLog) add(call string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := len(l.calls); n > 0 && l.calls[n-1] == call {
		return
	}
	l.calls = append(l.calls, call)
}

func (l *submitLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.calls
	l.calls = nil
	return out
}

// submitCredRepo records a committed Connect.
type submitCredRepo struct {
	organization.OrgCredentialRepository
	log *submitLog
}

func (r submitCredRepo) Tx(ctx context.Context, fn func(tx organization.OrgCredentialTx) error) error {
	if err := r.OrgCredentialRepository.Tx(ctx, fn); err != nil {
		return err
	}
	r.log.add("connect")
	return nil
}

// submitVault is the secrets client: each new reference is logged as
// write:<secret>, counted per secret, and its data kept by name.
type submitVault struct {
	secretmanagersvc.SecretManagementClient // anything else is a test bug (nil panic)

	log *submitLog
	err error

	mu     sync.Mutex
	n      int
	live   map[string]bool
	data   map[string]map[string]string
	writes map[string]int // CreateSecretRef calls per secret
}

func (v *submitVault) CreateSecretRef(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	v.log.add("write:" + loc.EntityName)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.writes[loc.EntityName]++
	if v.err != nil {
		return "", v.err
	}
	v.n++
	name := fmt.Sprintf("%s-%s-%08x", loc.ControlPlaneNamespace, loc.EntityName, v.n)
	v.live[name] = true
	v.data[name] = maps.Clone(data)
	return name, nil
}

func (v *submitVault) DeleteSecretRef(_ context.Context, _ secretmanagersvc.SecretLocation, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.live, name)
	return nil
}

// takeWrites returns the per-secret write counts since the last call.
func (v *submitVault) takeWrites() map[string]int {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := v.writes
	v.writes = map[string]int{}
	return out
}

func (v *submitVault) clientSecret(name string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.data[name]["client_secret"]
}

// submitThunder creates each org app on its first ensure and finds it after,
// and keeps the secret each app holds. A create waits (briefly) for another
// ensure to arrive, so a concurrent ensure that is not held off by the
// client secret's lock finds the app before the creator stores its secret.
type submitThunder struct {
	thundersvc.Client // anything else is a test bug (nil panic)

	log     *submitLog
	mu      sync.Mutex
	apps    map[string]bool
	secrets map[string]string // entity id → the secret Thunder holds
	arrived chan struct{}
}

func (t *submitThunder) ensure(kind, name string) thundersvc.OrgApp {
	t.log.add("ensure:" + kind)
	select {
	case t.arrived <- struct{}{}:
	default:
	}
	t.mu.Lock()
	id := "id-" + name
	if t.apps[name] {
		t.mu.Unlock()
		return thundersvc.OrgApp{EntityID: id, ClientID: name}
	}
	t.apps[name] = true
	secret := submitThunderSecret + "-" + kind
	t.secrets[id] = secret
	t.mu.Unlock()
	select {
	case <-t.arrived:
	case <-time.After(300 * time.Millisecond):
	}
	return thundersvc.OrgApp{EntityID: id, ClientID: name, Secret: secret, Created: true}
}

func (t *submitThunder) EnsurePublisherApp(_ context.Context, org, _, _ string) (thundersvc.OrgApp, error) {
	return t.ensure("publisher", thundersvc.PublisherAppName(org)), nil
}

func (t *submitThunder) EnsureOrgApp(_ context.Context, spec thundersvc.OrgAppSpec) (thundersvc.OrgApp, error) {
	return t.ensure("studio", spec.Name), nil
}

func (t *submitThunder) SetAppSecret(_ context.Context, entityID, secret string) error {
	t.log.add("thunder:put")
	t.mu.Lock()
	defer t.mu.Unlock()
	t.secrets[entityID] = secret
	return nil
}

// DeletePublisherApp is the revoke an idp kind switch runs.
func (t *submitThunder) DeletePublisherApp(_ context.Context, org, _ string) (bool, error) {
	t.log.add("thunder:delete-publisher")
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.apps, thundersvc.PublisherAppName(org))
	return true, nil
}

func (t *submitThunder) holds(entityID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.secrets[entityID]
}

type submitConverger struct{ log *submitLog }

func (c submitConverger) Trigger(context.Context, string) { c.log.add("converge") }

// --- fixture ------------------------------------------------------------------

type submitFixture struct {
	svc     *organization.Service
	log     *submitLog
	vault   *submitVault
	thunder *submitThunder
	rows    organization.OrgSecretRepository
	logs    *bytes.Buffer
	calls   []string
	writes  map[string]int
}

type submitOption func(*submitOptions)

type submitOptions struct {
	vaultErr    error
	noConverger bool
}

func withVaultError() submitOption {
	return func(o *submitOptions) { o.vaultErr = errors.New("openbao: sealed") }
}

func withConvergeNotConfigured() submitOption {
	return func(o *submitOptions) { o.noConverger = true }
}

var submitOU = uuid.MustParse("6f1c2d3e-4b5a-4c6d-8e9f-0a1b2c3d4e5f")

func newSubmitFixture(t *testing.T, opts ...submitOption) *submitFixture {
	t.Helper()
	var o submitOptions
	for _, fn := range opts {
		fn(&o)
	}
	db := dbtest.New(t) // self-skips under -short
	log := &submitLog{}

	gh := newCfgFakeGH(t)
	gh.patHappy()
	gh.mu.Lock()
	gh.routes["GET /user"] = func(w http.ResponseWriter, _ *http.Request) {
		log.add("validate")
		_, _ = io.WriteString(w, `{"login":"ghorg","name":"GH Org","email":"gh@x.io"}`)
	}
	gh.routes["GET /orgs/ghorg/repos"] = func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `[]`) }
	gh.mu.Unlock()

	orgRepo := organization.NewOrganizationRepository(db)
	if err := orgRepo.Create(context.Background(), &organization.Organization{UUID: uuid.New(), Name: "default"}); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := orgRepo.SetThunderOrgUUID(context.Background(), "default", submitOU); err != nil {
		t.Fatalf("seed org OU: %v", err)
	}

	store, err := secrets.NewDBStore(db, []byte(configAESKey))
	if err != nil {
		t.Fatalf("NewDBStore: %v", err)
	}
	minter, err := secrets.NewAppTokenMinter(nil)
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}

	vault := &submitVault{log: log, err: o.vaultErr, live: map[string]bool{}, data: map[string]map[string]string{}, writes: map[string]int{}}
	rows := organization.NewOrgSecretRepository(db)
	orgSecrets := organization.NewOrgSecretWriter(vault, rows, organization.NewOrgSecretLock(db), time.Now)
	credRepo := submitCredRepo{OrgCredentialRepository: organization.NewOrgCredentialRepository(db, nil), log: log}
	idpRepo := organization.NewIDPRepository(db, nil)
	refWriter := organization.NewSecretRefWriter(vault, credRepo, organization.NewOrgAnthropicRepository(db), idpRepo, organization.NewOrgModelConnectionRepository(db)).
		WithOrgSecretWriter(orgSecrets)

	credSvc := organization.NewCredentialService(credRepo, store, minter, configEnvSec).WithGitHubAPIBase(gh.URL).WithSecretRefWriter(refWriter)
	thunder := &submitThunder{log: log, apps: map[string]bool{}, secrets: map[string]string{}, arrived: make(chan struct{})}
	idpSvc := organization.NewIDPService(idpRepo, orgRepo, thunder, organization.PlatformIDPConfig{Issuer: platformIss, JWKSURL: platformJWKS}).
		WithSecretRefWriter(refWriter)

	var converger organization.StudioConverger = submitConverger{log: log}
	if o.noConverger {
		converger = nil
	}
	svc := organization.NewService(credSvc, nil, idpSvc, organization.PlatformIDPConfig{Issuer: platformIss, JWKSURL: platformJWKS}).
		WithAEStudio(orgSecrets, converger)

	// The submit's log lines, captured to check what they say and that no
	// secret value is among them.
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return &submitFixture{svc: svc, log: log, vault: vault, thunder: thunder, rows: rows, logs: logs}
}

// patch submits the gitpat and records the steps it ran in f.calls.
func (f *submitFixture) patch(ctx context.Context, login, pat string) error {
	_, err := f.svc.Patch(ctx, "default", "admin", gitProviderPatch(login, pat))
	f.calls = f.log.take()
	f.writes = f.vault.takeWrites()
	return err
}

func (f *submitFixture) row(t *testing.T, s organization.OrgSecret) string {
	t.Helper()
	ref, err := f.rows.Get(context.Background(), "default", s)
	if err != nil {
		t.Fatalf("row %s: %v", s, err)
	}
	if ref == nil {
		return ""
	}
	return ref.Name
}

func userCtx(ou string) context.Context {
	return jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: ou})
}

func gitProviderPatch(login, pat string) orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{GitProvider: patch.Field[orgconfig.GitProviderWrite]{
		Sent:  true,
		Value: orgconfig.GitProviderWrite{Kind: "github", Mode: "pat", PAT: pat, GitHubLogin: login},
	}}
}

// --- tests --------------------------------------------------------------------

func TestSubmit_Sequence(t *testing.T) {
	f := newSubmitFixture(t)
	ctx := userCtx(submitOU.String())
	if err := f.patch(ctx, "ghorg", "pat-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"validate", "connect", "write:github-pat", "write:github-webhook-secret", "ensure:publisher", "write:ae-publisher-client", "ensure:studio", "write:ae-studio-client", "converge"}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("calls %v\nwant  %v", f.calls, want)
	}
	if f.writes["github-pat"] != 1 {
		t.Fatalf("one submit writes one github-pat reference (Connect writes none), got %d", f.writes["github-pat"])
	}
	webhook, pat1 := f.row(t, organization.OrgSecretGitHubWebhookSecret), f.row(t, organization.OrgSecretGitHubPAT)

	if err := f.patch(ctx, "ghorg", "pat-2"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.calls, "write:github-webhook-secret") {
		t.Fatal("the webhook secret is made once (06 §6)")
	}
	if f.writes["github-pat"] != 1 {
		t.Fatalf("one resubmit writes one github-pat reference, got %d", f.writes["github-pat"])
	}
	want = []string{"validate", "connect", "write:github-pat", "ensure:publisher", "ensure:studio", "converge"}
	if !slices.Equal(f.calls, want) {
		t.Fatalf("resubmit calls %v\nwant  %v", f.calls, want)
	}
	if f.row(t, organization.OrgSecretGitHubWebhookSecret) != webhook {
		t.Fatal("a resubmit keeps the webhook secret's reference")
	}
	pat2 := f.row(t, organization.OrgSecretGitHubPAT)
	if pat2 == pat1 || !f.vault.live[pat2] || f.vault.live[pat1] {
		t.Fatalf("one submit = one github-pat reference, the previous one deleted: %q → %q, live %v", pat1, pat2, f.vault.live)
	}
	for _, v := range []string{"pat-1", "pat-2", submitThunderSecret} {
		if strings.Contains(f.logs.String(), v) {
			t.Fatal("a secret value reached the logs")
		}
	}
}

func TestSubmit_ConcurrentFirstSubmitsMakeOneWebhookSecret(t *testing.T) {
	f := newSubmitFixture(t)
	ctx := userCtx(submitOU.String())
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.Patch(ctx, "default", "admin", gitProviderPatch("ghorg", "pat"))
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("both first submits succeed: %v", errs)
	}
	written := slices.ContainsFunc(f.log.take(), func(c string) bool { return c == "write:github-webhook-secret" })
	if n := f.vault.takeWrites()["github-webhook-secret"]; !written || n != 1 {
		t.Fatalf("two first submits stored %d webhook secrets, want 1", n)
	}
	// Each client's reference holds the secret Thunder ends on, though both
	// submits ensured it at once (one created it, the other found it).
	for _, c := range []struct {
		secret organization.OrgSecret
		app    string
	}{
		{organization.OrgSecretPublisherClient, "id-aep-publisher-default"},
		{organization.OrgSecretStudioClient, "id-ae-studio-default"},
	} {
		stored := f.vault.clientSecret(f.row(t, c.secret))
		if stored == "" || stored != f.thunder.holds(c.app) {
			t.Fatalf("%s: the stored client_secret is not the one Thunder holds (stored %d chars)", c.secret, len(stored))
		}
	}
}

func TestSubmit_VaultFailureFailsTheRequest(t *testing.T) {
	f := newSubmitFixture(t, withVaultError())
	err := f.patch(userCtx(submitOU.String()), "ghorg", "pat")
	var se *organization.SectionError
	if !errors.As(err, &se) || se.Section != "gitProvider" || se.Status != http.StatusBadGateway {
		t.Fatalf("the gitpat reference is required now: Ensure cannot run without it; err = %v", err)
	}
	if slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, "ensure:") || c == "converge" }) {
		t.Fatalf("nothing after the failed write runs: %v", f.calls)
	}
}

func TestSubmit_EnsureNotConfiguredStillSucceeds(t *testing.T) {
	f := newSubmitFixture(t, withConvergeNotConfigured())
	if err := f.patch(userCtx(submitOU.String()), "ghorg", "pat"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.logs.String(), "ae_studio_not_configured") {
		t.Fatal("must log loudly")
	}
	if slices.Contains(f.calls, "converge") || f.row(t, organization.OrgSecretStudioClient) == "" {
		t.Fatalf("the clients are still ensured; nothing converges: %v", f.calls)
	}
}

// An idp kind switch in the same patch revokes the publisher app; the
// submit's setup runs after it, so the publisher it ensures survives.
func TestSubmit_RunsAfterAnIDPKindSwitch(t *testing.T) {
	f := newSubmitFixture(t)
	ctx := userCtx(submitOU.String())
	if err := f.patch(ctx, "ghorg", "pat-1"); err != nil {
		t.Fatal(err)
	}
	p := gitProviderPatch("ghorg", "pat-2")
	p.IDP = patch.Field[orgconfig.IDPWrite]{Sent: true, Value: orgconfig.IDPWrite{Kind: "custom", Issuer: "https://idp.example/", JWKSURL: "https://idp.example/jwks"}}
	if _, err := f.svc.Patch(ctx, "default", "admin", p); err != nil {
		t.Fatal(err)
	}
	calls := f.log.take()
	del, ens := slices.Index(calls, "thunder:delete-publisher"), slices.Index(calls, "ensure:publisher")
	if del < 0 || ens < del {
		t.Fatalf("the publisher is ensured after the revoke: %v", calls)
	}
}
