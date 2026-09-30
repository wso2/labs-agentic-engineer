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

// UNIT tier: SreModelConnectionService over in-memory fakes for the row, the
// key's bytes, the card's unit of work and the probe. What a save refuses
// before and after the probe, what it writes, and that OnChange follows the
// commit.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

const (
	sreOrg      = "acme"
	sreActor    = "admin@acme.test"
	sreKey      = "sre-model-key-0123456789"
	sreOtherKey = "sre-model-key-second-9876"
)

// --- fakes -------------------------------------------------------------------

// sreWorld is the state the fakes share: the SRE row, the secret bytes, and
// the order things happened in (lock, commit, onChange).
type sreWorld struct {
	row    *OrgSreModelConnection
	keys   map[string][]byte // "<org>/<key>" -> bytes
	events []string
}

func newSreWorld() *sreWorld { return &sreWorld{keys: map[string][]byte{}} }

func (w *sreWorld) key(org string) string { return string(w.keys[org+"/"+sreModelKeyStoreKey]) }

// sreConns is OrgSreModelConnectionRepository over the world.
type sreConns struct{ w *sreWorld }

func (c sreConns) GetByOrg(_ context.Context, org string) (*OrgSreModelConnection, error) {
	if c.w.row == nil || c.w.row.OcOrgID != org {
		return nil, nil
	}
	row := *c.w.row
	return &row, nil
}

// sreStore is secrets.CredentialStore over the world.
type sreStore struct{ w *sreWorld }

func (s sreStore) Get(_ context.Context, org, key string) ([]byte, error) {
	v, ok := s.w.keys[org+"/"+key]
	if !ok {
		return nil, secrets.ErrSecretNotFound
	}
	return v, nil
}

func (s sreStore) Put(_ context.Context, org, key string, value []byte) error {
	s.w.keys[org+"/"+key] = append([]byte(nil), value...)
	return nil
}

func (s sreStore) Delete(_ context.Context, org, key string) error {
	delete(s.w.keys, org+"/"+key)
	return nil
}

// sreCard is AgentsCardRepository over the world. Writes land in a staged
// copy that only becomes the world on commit, so a failed closure leaves
// nothing behind, as the real transaction would.
type sreCard struct{ w *sreWorld }

func (c sreCard) Tx(_ context.Context, fn func(tx AgentsCardTx) error) error {
	staged := &sreWorld{keys: map[string][]byte{}}
	if c.w.row != nil {
		row := *c.w.row
		staged.row = &row
	}
	for k, v := range c.w.keys {
		staged.keys[k] = v
	}
	if err := fn(&sreTx{w: c.w, staged: staged}); err != nil {
		return err
	}
	c.w.row, c.w.keys = staged.row, staged.keys
	c.w.events = append(c.w.events, "commit")
	return nil
}

// sreTx implements the SRE part of AgentsCardTx; the rest of the interface is
// embedded nil and would panic if the service reached for it.
type sreTx struct {
	AgentsCardTx
	w, staged *sreWorld
}

func (t *sreTx) AdvisoryLock(key string) error {
	t.w.events = append(t.w.events, "lock:"+key)
	return nil
}

func (t *sreTx) GetSreModelConnection(org string) (*OrgSreModelConnection, error) {
	return sreConns{t.staged}.GetByOrg(context.Background(), org)
}

func (t *sreTx) UpsertSreModelConnection(row *OrgSreModelConnection) error {
	r := *row
	t.staged.row = &r
	return nil
}

func (t *sreTx) DeleteSreModelConnection(org string) error {
	if t.staged.row != nil && t.staged.row.OcOrgID == org {
		t.staged.row = nil
	}
	return nil
}

func (t *sreTx) Secrets() secrets.CredentialStore { return sreStore{t.staged} }

// sreProber is the openai-compatible prober: it records each target and
// answers err, or a passing result.
type sreProber struct {
	targets []probeTarget
	err     error
}

func (p *sreProber) Probe(_ context.Context, t probeTarget) (ProbeResult, error) {
	p.targets = append(p.targets, t)
	if p.err != nil {
		return ProbeResult{}, p.err
	}
	return ProbeResult{AuthScheme: modelconn.AuthBearer, ModelListed: modelconn.Yes}, nil
}

// sreOrgConn is the org's model connection as ConnectionReader reads it.
type sreOrgConn struct {
	conn modelconn.Connection
	key  string
	ok   bool
}

func (o sreOrgConn) Effective(context.Context, string) (modelconn.Connection, string, bool, error) {
	return o.conn, o.key, o.ok, nil
}

func (o sreOrgConn) KeyRef(context.Context, string) (modelconn.Connection, SecretRefTriplet, error) {
	return modelconn.Connection{}, SecretRefTriplet{}, errors.New("not used")
}

var sreNow = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

// newSreService wires the service over w, with prober in place of the network.
func newSreService(w *sreWorld, prober *sreProber, org ConnectionReader) *SreModelConnectionService {
	s := NewSreModelConnectionService(sreConns{w}, sreStore{w}, sreCard{w}, org)
	s.probers = modelProbers{byFormat: map[modelconn.Format]ModelProber{modelconn.FormatOpenAICompatible: prober}}
	s.now = func() time.Time { return sreNow }
	s.OnChange(func(org string) { w.events = append(w.events, "onChange:"+org) })
	return s
}

// seedSre stores a connection on host with key, as a previous save left it.
func seedSre(w *sreWorld, host, key string) {
	w.row = &OrgSreModelConnection{
		OcOrgID: sreOrg, BaseURL: "https://" + host + "/v1", Host: host, Model: "gpt-4o-mini",
		ConnectedAt: sreNow.Add(-time.Hour), UpdatedAt: sreNow.Add(-time.Hour), UpdatedBy: "someone@acme.test",
	}
	w.keys[sreOrg+"/"+sreModelKeyStoreKey] = []byte(key)
}

func ptr(s string) *string { return &s }

// wantSectionError asserts err is a SectionError on sreLlm with code.
func wantSectionError(t *testing.T, err error, code string) *SectionError {
	t.Helper()
	var se *SectionError
	if !errors.As(err, &se) {
		t.Fatalf("want a *SectionError, got %T: %v", err, err)
	}
	if se.Section != "sreLlm" || se.Code != code {
		t.Fatalf("SectionError = {Section:%q Code:%q Message:%q}, want {sreLlm %s}", se.Section, se.Code, se.Message, code)
	}
	return se
}

// --- Set ---------------------------------------------------------------------

func TestSreModelConnectionService_Set(t *testing.T) {
	ctx := context.Background()

	t.Run("refuses a probe failure and stores nothing", func(t *testing.T) {
		w := newSreWorld()
		prober := &sreProber{err: &ValidationError{Code: "llm_key_rejected", Message: "a.example rejected the key (401)"}}
		s := newSreService(w, prober, sreOrgConn{})

		err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{
			BaseURL: ptr("https://a.example/v1"), APIKey: ptr(sreKey), Model: ptr("gpt-4o-mini"),
		})
		se := wantSectionError(t, err, "llm_key_rejected")
		if se.Status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", se.Status)
		}
		if len(prober.targets) != 1 {
			t.Fatalf("probe calls = %d, want 1", len(prober.targets))
		}
		if got := w.key(sreOrg); got != "" {
			t.Errorf("store.Get(org, %s) = %q, want empty", sreModelKeyStoreKey, got)
		}
		if w.row != nil {
			t.Errorf("row = %+v, want none", w.row)
		}
		if slices.Contains(w.events, "onChange:"+sreOrg) {
			t.Errorf("onChange ran for a refused save: %v", w.events)
		}
	})

	t.Run("refuses non-https base URL", func(t *testing.T) {
		w := newSreWorld()
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{
			BaseURL: ptr("http://x"), APIKey: ptr(sreKey), Model: ptr("gpt-4o-mini"),
		})
		wantSectionError(t, err, "llm_base_url_invalid")
		if len(prober.targets) != 0 {
			t.Errorf("probe called %d times for a refused URL, want 0", len(prober.targets))
		}
		if w.row != nil || w.key(sreOrg) != "" {
			t.Errorf("a refused save wrote: row=%+v key=%q", w.row, w.key(sreOrg))
		}
	})

	t.Run("refuses a host change without a new key", func(t *testing.T) {
		w := newSreWorld()
		seedSre(w, "a.example", sreKey)
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{BaseURL: ptr("https://b.example/v1")})
		wantSectionError(t, err, "llm_key_required_for_new_host")
		if len(prober.targets) != 0 {
			t.Errorf("probe called %d times: the stored key must never reach b.example", len(prober.targets))
		}
		if w.row.Host != "a.example" || w.key(sreOrg) != sreKey {
			t.Errorf("a refused save changed the stored connection: row=%+v", w.row)
		}
	})

	t.Run("refuses keys under 12 characters", func(t *testing.T) {
		w := newSreWorld()
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{
			BaseURL: ptr("https://a.example/v1"), APIKey: ptr("short"), Model: ptr("gpt-4o-mini"),
		})
		wantSectionError(t, err, "llm_key_too_short")
		if len(prober.targets) != 0 {
			t.Errorf("probe called %d times for a refused key, want 0", len(prober.targets))
		}
		if w.row != nil || w.key(sreOrg) != "" {
			t.Errorf("a refused save wrote: row=%+v key=%q", w.row, w.key(sreOrg))
		}
	})

	t.Run("refuses a first save without every field", func(t *testing.T) {
		w := newSreWorld()
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{BaseURL: ptr("https://a.example/v1"), APIKey: ptr(sreKey)})
		wantSectionError(t, err, "llm_field_required")
		if len(prober.targets) != 0 {
			t.Errorf("probe called %d times, want 0", len(prober.targets))
		}
	})

	t.Run("stores row and key, then calls OnChange once", func(t *testing.T) {
		w := newSreWorld()
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		if err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{
			BaseURL: ptr("https://A.example/v1/"), APIKey: ptr(sreKey), Model: ptr("gpt-4o-mini"),
		}); err != nil {
			t.Fatalf("Set: %v", err)
		}

		wantTarget := probeTarget{Org: sreOrg, Format: modelconn.FormatOpenAICompatible,
			BaseURL: "https://a.example/v1", Host: "a.example", Model: "gpt-4o-mini", Key: sreKey}
		if len(prober.targets) != 1 || prober.targets[0] != wantTarget {
			t.Fatalf("probe targets = %+v, want [%+v]", prober.targets, wantTarget)
		}
		want := OrgSreModelConnection{OcOrgID: sreOrg, BaseURL: "https://a.example/v1", Host: "a.example",
			Model: "gpt-4o-mini", ConnectedAt: sreNow, UpdatedAt: sreNow, UpdatedBy: sreActor}
		if w.row == nil || *w.row != want {
			t.Fatalf("row = %+v, want %+v", w.row, want)
		}
		if got := w.key(sreOrg); got != sreKey {
			t.Errorf("stored key = %q, want the sent key", got)
		}
		wantEvents := []string{"lock:org_anthropic:" + sreOrg, "lock:org_model:" + sreOrg, "commit", "onChange:" + sreOrg}
		if !slices.Equal(w.events, wantEvents) {
			t.Errorf("events = %v, want %v (onChange once, after commit, under the card's lock)", w.events, wantEvents)
		}
	})

	t.Run("a model change on the same host probes with the stored key and keeps it", func(t *testing.T) {
		w := newSreWorld()
		seedSre(w, "a.example", sreKey)
		connectedAt := w.row.ConnectedAt
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		if err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{Model: ptr("gpt-4o")}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if len(prober.targets) != 1 || prober.targets[0].Key != sreKey || prober.targets[0].Host != "a.example" {
			t.Fatalf("probe targets = %+v, want one on a.example with the stored key", prober.targets)
		}
		if w.row.Model != "gpt-4o" || w.key(sreOrg) != sreKey {
			t.Errorf("row=%+v key=%q, want model gpt-4o and the stored key", w.row, w.key(sreOrg))
		}
		if !w.row.ConnectedAt.Equal(connectedAt) {
			t.Errorf("connectedAt = %v, want %v kept on the same host", w.row.ConnectedAt, connectedAt)
		}
	})

	t.Run("a host change with a new key replaces both and resets connectedAt", func(t *testing.T) {
		w := newSreWorld()
		seedSre(w, "a.example", sreKey)
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})

		if err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{
			BaseURL: ptr("https://b.example/v1"), APIKey: ptr(sreOtherKey),
		}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if w.row.Host != "b.example" || w.row.Model != "gpt-4o-mini" || w.key(sreOrg) != sreOtherKey {
			t.Errorf("row=%+v key=%q, want b.example, the stored model and the new key", w.row, w.key(sreOrg))
		}
		if !w.row.ConnectedAt.Equal(sreNow) {
			t.Errorf("connectedAt = %v, want reset to %v on a new host", w.row.ConnectedAt, sreNow)
		}
	})

	t.Run("a save the stored connection moved under is a conflict", func(t *testing.T) {
		w := newSreWorld()
		seedSre(w, "a.example", sreKey)
		prober := &sreProber{}
		s := newSreService(w, prober, sreOrgConn{})
		// Another save lands between this one's probe and its transaction.
		s.card = racingCard{sreCard{w}, func() { seedSre(w, "b.example", sreOtherKey) }}

		err := s.Set(ctx, sreOrg, sreActor, orgconfig.SreLlmWrite{Model: ptr("gpt-4o")})
		var se *SectionError
		if !errors.As(err, &se) || se.Section != "sreLlm" || se.Status != http.StatusConflict {
			t.Fatalf("want a 409 SectionError on sreLlm, got %v", err)
		}
		if w.row.Host != "b.example" || w.row.Model != "gpt-4o-mini" {
			t.Errorf("the conflicting save wrote over the newer row: %+v", w.row)
		}
	})
}

// racingCard runs race before the transaction opens.
type racingCard struct {
	sreCard
	race func()
}

func (c racingCard) Tx(ctx context.Context, fn func(tx AgentsCardTx) error) error {
	c.race()
	return c.sreCard.Tx(ctx, fn)
}

// --- Service.Patch probes the SRE model connection once ----------------------

// TestServicePatch_SreLlm_ProbesOnce guards the /config PATCH sequencing:
// Patch's probe phase calls Check, and the persist phase must write the draft
// Check already validated and probed rather than probing it again. A flaky
// second probe must not be able to leave sreLlm unwritten after sibling
// sections already committed.
func TestServicePatch_SreLlm_ProbesOnce(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	prober := &sreProber{}
	sre := newSreService(w, prober, sreOrgConn{})
	svc := NewService(nil, nil, nil, nil, PlatformIDPConfig{}, "", "").WithSreModel(sre)

	_, err := svc.Patch(ctx, sreOrg, sreActor, orgconfig.ConfigPatch{
		SreLLM: patch.Field[orgconfig.SreLlmWrite]{Sent: true, Value: orgconfig.SreLlmWrite{
			BaseURL: ptr("https://a.example/v1"), APIKey: ptr(sreKey), Model: ptr("gpt-4o-mini"),
		}},
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if len(prober.targets) != 1 {
		t.Fatalf("probe calls = %d, want 1 (Patch must not probe twice per save)", len(prober.targets))
	}
	if w.row == nil || w.row.Host != "a.example" || w.key(sreOrg) != sreKey {
		t.Fatalf("row = %+v key = %q, want a.example with the sent key written", w.row, w.key(sreOrg))
	}
}

// --- Clear -------------------------------------------------------------------

func TestSreModelConnectionService_Clear(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	seedSre(w, "a.example", sreKey)
	s := newSreService(w, &sreProber{}, sreOrgConn{})

	if err := s.Clear(ctx, sreOrg, sreActor); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if w.row != nil {
		t.Errorf("row = %+v, want deleted", w.row)
	}
	if got := w.key(sreOrg); got != "" {
		t.Errorf("stored key = %q, want deleted", got)
	}
	wantEvents := []string{"lock:org_anthropic:" + sreOrg, "lock:org_model:" + sreOrg, "commit", "onChange:" + sreOrg}
	if !slices.Equal(w.events, wantEvents) {
		t.Errorf("events = %v, want %v", w.events, wantEvents)
	}

	// Idempotent.
	if err := s.Clear(ctx, sreOrg, sreActor); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}

// --- reads -------------------------------------------------------------------

func TestSreModelConnectionService_EffectiveSRE(t *testing.T) {
	ctx := context.Background()
	orgConn := modelconn.Connection{Format: modelconn.FormatOpenAICompatible, BaseURL: "https://org.example/v1",
		Host: "org.example", Model: "org-model", AuthScheme: modelconn.AuthBearer}

	t.Run("the SRE model connection wins", func(t *testing.T) {
		w := newSreWorld()
		seedSre(w, "a.example", sreKey)
		s := newSreService(w, &sreProber{}, sreOrgConn{conn: orgConn, key: "org-key-0123456789", ok: true})

		eff, err := s.EffectiveSRE(ctx, sreOrg)
		if err != nil {
			t.Fatalf("EffectiveSRE: %v", err)
		}
		if eff.Source != SRESourceOverride || eff.Conn.Host != "a.example" || eff.Key != sreKey {
			t.Errorf("eff = {%s %s}, want the override on a.example with its key", eff.Source, eff.Conn.Host)
		}
	})

	t.Run("without one, the org connection when it can run the SRE agent", func(t *testing.T) {
		s := newSreService(newSreWorld(), &sreProber{}, sreOrgConn{conn: orgConn, key: "org-key-0123456789", ok: true})

		eff, err := s.EffectiveSRE(ctx, sreOrg)
		if err != nil {
			t.Fatalf("EffectiveSRE: %v", err)
		}
		if eff.Source != SRESourceOrganization || eff.Conn != orgConn || eff.Key != "org-key-0123456789" {
			t.Errorf("eff = {%s %+v}, want the org connection", eff.Source, eff.Conn)
		}
	})

	t.Run("no connection is none", func(t *testing.T) {
		s := newSreService(newSreWorld(), &sreProber{}, sreOrgConn{})

		eff, err := s.EffectiveSRE(ctx, sreOrg)
		if err != nil {
			t.Fatalf("EffectiveSRE: %v", err)
		}
		if eff.Source != SRESourceNone || eff.Key != "" {
			t.Errorf("eff = {%s key set=%v}, want none", eff.Source, eff.Key != "")
		}
	})
}

func TestSreModelConnectionService_Projection(t *testing.T) {
	ctx := context.Background()

	t.Run("none is nil", func(t *testing.T) {
		s := newSreService(newSreWorld(), &sreProber{}, sreOrgConn{})
		proj, err := s.Projection(ctx, sreOrg)
		if err != nil || proj != nil {
			t.Fatalf("Projection = %+v, %v; want nil, nil", proj, err)
		}
	})

	t.Run("a stored connection, the key previewed only", func(t *testing.T) {
		w := newSreWorld()
		seedSre(w, "a.example", sreKey)
		s := newSreService(w, &sreProber{}, sreOrgConn{})

		proj, err := s.Projection(ctx, sreOrg)
		if err != nil {
			t.Fatalf("Projection: %v", err)
		}
		want := orgconfig.SreLlmProjection{BaseURL: "https://a.example/v1", Host: "a.example", Model: "gpt-4o-mini",
			KeyPreview: keyPreview(sreKey), ConnectedAt: w.row.ConnectedAt, UpdatedAt: w.row.UpdatedAt, UpdatedBy: "someone@acme.test"}
		if proj == nil || *proj != want {
			t.Fatalf("Projection = %+v, want %+v", proj, want)
		}
	})
}

// --- GET /config's sreAgent ----------------------------------------------------

// sreStatus answers for owner only (ok=false for any other org), or fails
// every read with err.
type sreStatus struct {
	owner, status, reason string
	err                   error
}

func (s sreStatus) Status(_ context.Context, org string) (string, string, bool, error) {
	if s.err != nil {
		return "", "", false, s.err
	}
	if org != s.owner {
		return "", "", false, nil
	}
	return s.status, s.reason, true, nil
}

func TestService_Get_SreAgent(t *testing.T) {
	ctx := context.Background()
	w := newSreWorld()
	seedSre(w, "a.example", sreKey)
	sre := newSreService(w, &sreProber{}, sreOrgConn{})

	t.Run("null without a status reader", func(t *testing.T) {
		out, err := NewService(nil, nil, nil, nil, PlatformIDPConfig{}, "", "").WithSreModel(sre).Get(ctx, sreOrg)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if out.SreAgent != nil {
			t.Errorf("sreAgent = %+v, want nil", out.SreAgent)
		}
		if out.SreLLM == nil || out.SreLLM.Host != "a.example" {
			t.Errorf("sreLlm = %+v, want the stored connection", out.SreLLM)
		}
	})

	t.Run("the effective connection and the status with one", func(t *testing.T) {
		out, err := NewService(nil, nil, nil, nil, PlatformIDPConfig{}, "", "").WithSreModel(sre).
			WithSREAgentStatus(sreStatus{owner: sreOrg, status: "failed", reason: "helm upgrade failed"}).Get(ctx, sreOrg)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		want := orgconfig.SreAgentProjection{Enabled: true, Source: "override", Model: "gpt-4o-mini",
			Host: "a.example", Status: "failed", Reason: "helm upgrade failed"}
		if out.SreAgent == nil || *out.SreAgent != want {
			t.Errorf("sreAgent = %+v, want %+v", out.SreAgent, want)
		}
	})

	t.Run("null for an org the observability plane does not serve", func(t *testing.T) {
		out, err := NewService(nil, nil, nil, nil, PlatformIDPConfig{}, "", "").WithSreModel(sre).
			WithSREAgentStatus(sreStatus{owner: "globex", status: "running"}).Get(ctx, sreOrg)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if out.SreAgent != nil {
			t.Errorf("sreAgent = %+v, want nil (another org owns the SRE agent)", out.SreAgent)
		}
	})

	t.Run("failed, not an error, when the status cannot be read", func(t *testing.T) {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		defer slog.SetDefault(prev)

		out, err := NewService(nil, nil, nil, nil, PlatformIDPConfig{}, "", "").WithSreModel(sre).
			WithSREAgentStatus(sreStatus{err: errors.New("dial tcp: connection refused")}).Get(ctx, sreOrg)
		if err != nil {
			t.Fatalf("Get: %v, want GET /config to stay loadable", err)
		}
		want := orgconfig.SreAgentProjection{Enabled: true, Source: "override", Model: "gpt-4o-mini",
			Host: "a.example", Status: "failed",
			Reason: "SRE agent status unavailable: cannot read the observability plane"}
		if out.SreAgent == nil || *out.SreAgent != want {
			t.Errorf("sreAgent = %+v, want %+v", out.SreAgent, want)
		}
		logged := buf.String()
		if !strings.Contains(logged, `"level":"WARN"`) || !strings.Contains(logged, "connection refused") {
			t.Errorf("want a warning carrying the read error, got %q", logged)
		}
		if strings.Contains(logged, sreKey) {
			t.Error("the warning carries the SRE model key")
		}
	})
}
