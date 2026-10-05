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

// UNIT tier for EnsureClient: the real idpService, SecretRefWriter and
// OrgSecretWriter over in-memory rows, a fake vault and a fake Thunder that
// share one call log, so the 06 §5 table and the heal order (vault before
// the Thunder PUT) are observable step by step.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// --- in-memory org secret rows and lock --------------------------------------

// memOrgSecretRepo keeps one row per (org, secret) with the real
// repository's compare-and-swap rules.
type memOrgSecretRepo struct {
	rows map[string]OrgSecretRef
}

func newMemOrgSecretRepo() *memOrgSecretRepo {
	return &memOrgSecretRepo{rows: map[string]OrgSecretRef{}}
}

var _ OrgSecretRepository = (*memOrgSecretRepo)(nil)

func memOrgSecretKey(org string, s OrgSecret) string { return org + "/" + string(s) }

func (r *memOrgSecretRepo) Get(_ context.Context, org string, s OrgSecret) (*OrgSecretRef, error) {
	row, ok := r.rows[memOrgSecretKey(org, s)]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *memOrgSecretRepo) List(context.Context, string) ([]OrgSecretRef, error) {
	panic("memOrgSecretRepo: List is not on the EnsureClient path")
}

func (r *memOrgSecretRepo) Upsert(_ context.Context, org string, ref OrgSecretRef, expectPrev string) error {
	cur, ok := r.rows[memOrgSecretKey(org, ref.Secret)]
	if (expectPrev == "" && ok) || (expectPrev != "" && (!ok || cur.Name != expectPrev)) {
		return ErrOrgSecretConflict
	}
	r.rows[memOrgSecretKey(org, ref.Secret)] = ref
	return nil
}

func (r *memOrgSecretRepo) Delete(_ context.Context, org string, s OrgSecret, name string) error {
	cur, ok := r.rows[memOrgSecretKey(org, s)]
	if !ok || cur.Name != name {
		return ErrOrgSecretConflict
	}
	delete(r.rows, memOrgSecretKey(org, s))
	return nil
}

// memOrgSecretLock never contends: these tests are single-threaded.
type memOrgSecretLock struct{}

func (memOrgSecretLock) Lock(context.Context, string, OrgSecret) (func(), error) {
	return func() {}, nil
}

// --- fake vault and Thunder sharing one call log -----------------------------

// ensureVault is the secrets client: CreateSecretRef mints a name and logs
// "vault"; DeleteSecretRef logs "delete:<name>".
type ensureVault struct {
	secretmanagersvc.SecretManagementClient // anything else is a test bug (nil panic)

	log       *[]string
	createErr error
	n         int
	writes    []ensureWrite
	refs      map[string]bool
}

type ensureWrite struct {
	entity string
	ou     string
	data   map[string]string
}

func (v *ensureVault) CreateSecretRef(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	*v.log = append(*v.log, "vault")
	if v.createErr != nil {
		return "", v.createErr
	}
	v.n++
	name := fmt.Sprintf("%s-%s-%08x", loc.ControlPlaneNamespace, loc.EntityName, v.n)
	v.writes = append(v.writes, ensureWrite{entity: loc.EntityName, ou: loc.OrgName, data: maps.Clone(data)})
	v.refs[name] = true
	return name, nil
}

func (v *ensureVault) DeleteSecretRef(_ context.Context, _ secretmanagersvc.SecretLocation, name string) error {
	*v.log = append(*v.log, "delete:"+name)
	delete(v.refs, name)
	return nil
}

func (v *ensureVault) wrote(s OrgSecret) bool {
	return slices.ContainsFunc(v.writes, func(w ensureWrite) bool { return w.entity == string(s) })
}

// ensureThunder answers EnsureOrgApp / EnsurePublisherApp from appExists and
// logs a create and SetAppSecret.
type ensureThunder struct {
	thundersvc.Client // anything else is a test bug (nil panic)

	log       *[]string
	appExists bool
	foreignOU bool
	putErr    error

	created, put bool
	putSecret    string
	specs        []thundersvc.OrgAppSpec
	onEnsure     func()
}

func (t *ensureThunder) app(name string) (thundersvc.OrgApp, error) {
	if t.onEnsure != nil {
		t.onEnsure()
	}
	if t.foreignOU {
		return thundersvc.OrgApp{}, thundersvc.ErrAppInForeignOU
	}
	if t.appExists {
		return thundersvc.OrgApp{EntityID: "app-1", ClientID: name}, nil
	}
	t.created = true
	*t.log = append(*t.log, "thunder:create")
	return thundersvc.OrgApp{EntityID: "app-new", ClientID: name, Secret: "thunder-once", Created: true}, nil
}

func (t *ensureThunder) EnsureOrgApp(_ context.Context, spec thundersvc.OrgAppSpec) (thundersvc.OrgApp, error) {
	t.specs = append(t.specs, spec)
	return t.app(spec.Name)
}

func (t *ensureThunder) EnsurePublisherApp(_ context.Context, orgHandle, orgOUID, storedID string) (thundersvc.OrgApp, error) {
	t.specs = append(t.specs, thundersvc.OrgAppSpec{Name: thundersvc.PublisherAppName(orgHandle), OUID: orgOUID, StoredID: storedID})
	return t.app(thundersvc.PublisherAppName(orgHandle))
}

func (t *ensureThunder) SetAppSecret(_ context.Context, entityID, secret string) error {
	*t.log = append(*t.log, "thunder:put:"+entityID)
	if t.putErr != nil {
		return t.putErr
	}
	t.put, t.putSecret = true, secret
	return nil
}

// ouOrgRepo is an OrganizationRepository whose org row carries ou.
type ouOrgRepo struct {
	stubOrgRepo
	ou *uuid.UUID
}

func (r ouOrgRepo) GetByName(_ context.Context, name string) (*Organization, error) {
	return &Organization{Name: name, ThunderOrgUUID: r.ou}, nil
}

// --- fixture ------------------------------------------------------------------

var ensureOU = uuid.MustParse("1b4e28ba-2fa1-11d2-883f-0016d3cca427")

// ensureCtx is a request whose ouId is the org's OU.
func ensureCtx() context.Context {
	return jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: ensureOU.String()})
}

type ensureFixture struct {
	svc     *idpService
	log     []string
	vault   *ensureVault
	thunder *ensureThunder
	rows    *memOrgSecretRepo
	idp     *memIDPRepo
}

// newEnsureFixture builds the service for org "default". rowSet seeds both
// client rows naming "old-ref" (a reference the vault holds).
func newEnsureFixture(t *testing.T, appExists, rowSet bool) *ensureFixture {
	t.Helper()
	f := &ensureFixture{rows: newMemOrgSecretRepo(), idp: newMemIDPRepo()}
	f.vault = &ensureVault{log: &f.log, refs: map[string]bool{}}
	f.thunder = &ensureThunder{log: &f.log, appExists: appExists}
	if rowSet {
		for _, s := range []OrgSecret{OrgSecretPublisherClient, OrgSecretStudioClient} {
			f.rows.rows[memOrgSecretKey("default", s)] = OrgSecretRef{Secret: s, Name: "old-ref"}
		}
		f.vault.refs["old-ref"] = true
	}
	ou := ensureOU
	writer := NewSecretRefWriter(f.vault, nil, f.idp).
		WithOrgSecretWriter(NewOrgSecretWriter(f.vault, f.rows, memOrgSecretLock{}, time.Now))
	f.svc = NewIDPService(f.idp, ouOrgRepo{ou: &ou}, f.thunder, PlatformIDPConfig{}).WithSecretRefWriter(writer)
	return f
}

func (f *ensureFixture) row(s OrgSecret) string {
	return f.rows.rows[memOrgSecretKey("default", s)].Name
}

func (f *ensureFixture) profile(t *testing.T) *OrganizationIDPProfile {
	t.Helper()
	p, err := f.idp.GetProfileByOrgID(context.Background(), "default")
	if err != nil || p == nil {
		t.Fatalf("profile: %v %v", p, err)
	}
	return p
}

// --- the 06 §5 table ------------------------------------------------------------

func TestEnsureClient_Table(t *testing.T) {
	cases := []struct {
		name                string
		appExists, rowSet   bool
		wantCreate, wantPut bool
		wantWrite           bool
	}{
		{"app missing", false, false, true, false, true},
		{"app missing, stale row", false, true, true, false, true},
		{"exists, row missing: heal", true, false, false, true, true},
		{"exists, row present: nothing", true, true, false, false, false},
	}
	for _, kind := range []struct {
		kind   ClientKind
		secret OrgSecret
	}{{ClientStudio, OrgSecretStudioClient}, {ClientPublisher, OrgSecretPublisherClient}} {
		for _, c := range cases {
			t.Run(string(kind.kind)+": "+c.name, func(t *testing.T) {
				f := newEnsureFixture(t, c.appExists, c.rowSet)
				if err := f.svc.EnsureClient(ensureCtx(), "default", kind.kind); err != nil {
					t.Fatal(err)
				}
				if f.thunder.created != c.wantCreate || f.thunder.put != c.wantPut || f.vault.wrote(kind.secret) != c.wantWrite {
					t.Fatalf("create=%v put=%v write=%v (log %v)", f.thunder.created, f.thunder.put, f.vault.wrote(kind.secret), f.log)
				}
				if c.wantPut && !slices.Equal(f.log[:2], []string{"vault", "thunder:put:app-1"}) {
					t.Fatalf("heal: vault write before Thunder PUT, so Thunder never holds a secret AE failed to store: %v", f.log)
				}
				if !c.wantWrite {
					if f.row(kind.secret) != "old-ref" || len(f.vault.writes) != 0 {
						t.Fatalf("present: nothing written, row %q", f.row(kind.secret))
					}
					return
				}
				// The row names the one new reference and the old one is gone.
				w := f.vault.writes[0]
				if f.row(kind.secret) == "old-ref" || !f.vault.refs[f.row(kind.secret)] || f.vault.refs["old-ref"] {
					t.Fatalf("row %q, live %v", f.row(kind.secret), f.vault.refs)
				}
				wantSecret := "thunder-once"
				if c.wantPut {
					wantSecret = f.thunder.putSecret
				}
				if w.data["client_secret"] != wantSecret || w.data["client_id"] == "" {
					t.Fatalf("stored the secret Thunder holds: stored %d chars", len(w.data["client_secret"]))
				}
			})
		}
	}
}

func TestEnsureClient_HealSecretIs32RandomBytes(t *testing.T) {
	f := newEnsureFixture(t, true, false)
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientStudio); err != nil {
		t.Fatal(err)
	}
	g := newEnsureFixture(t, true, false)
	if err := g.svc.EnsureClient(ensureCtx(), "default", ClientStudio); err != nil {
		t.Fatal(err)
	}
	if len(f.thunder.putSecret) != 64 || f.thunder.putSecret == g.thunder.putSecret {
		t.Fatalf("heal secret: 32 random bytes hex (got %d chars, distinct=%v)", len(f.thunder.putSecret), f.thunder.putSecret != g.thunder.putSecret)
	}
}

func TestEnsureClient_StudioRecordsIDsAndNoSecretColumn(t *testing.T) {
	f := newEnsureFixture(t, false, false)
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientStudio); err != nil {
		t.Fatal(err)
	}
	p := f.profile(t)
	if p.StudioClientID != "ae-studio-default" || p.StudioThunderAppID != "app-new" {
		t.Fatalf("studio ids: %q %q", p.StudioClientID, p.StudioThunderAppID)
	}
	if p.PublisherClientSecret != "" || p.SecretRefName != nil {
		t.Fatal("the studio secret lives only in its reference")
	}
	spec := f.thunder.specs[0]
	if spec.Name != "ae-studio-default" || spec.OUID != ensureOU.String() {
		t.Fatalf("studio app spec: %+v", spec)
	}
}

func TestEnsureClient_PublisherKeepsTheDualPath(t *testing.T) {
	f := newEnsureFixture(t, true, false)
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientPublisher); err != nil {
		t.Fatal(err)
	}
	p := f.profile(t)
	if p.PublisherClientSecret != f.thunder.putSecret {
		t.Fatal("dual: the sealed publisher_client_secret holds the healed secret")
	}
	if p.SecretRefName == nil || *p.SecretRefName != f.row(OrgSecretPublisherClient) {
		t.Fatalf("triplet names the row's reference: %v vs %q", p.SecretRefName, f.row(OrgSecretPublisherClient))
	}
	if p.PublisherThunderAppID != "app-1" || p.PublisherClientID != "aep-publisher-default" {
		t.Fatalf("publisher ids: %q %q", p.PublisherClientID, p.PublisherThunderAppID)
	}
}

func TestEnsureClient_HealPutFailureRollsBackTheReference(t *testing.T) {
	f := newEnsureFixture(t, true, false)
	f.thunder.putErr = errors.New("thunder: 500")
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientStudio); err == nil {
		t.Fatal("a failed PUT fails the ensure")
	}
	if f.row(OrgSecretStudioClient) != "" || len(f.vault.refs) != 0 {
		t.Fatalf("no row and no live reference after the rollback: row %q live %v", f.row(OrgSecretStudioClient), f.vault.refs)
	}
	if f.profile(t).StudioThunderAppID != "app-1" {
		// The scan-found id is recorded before the write; nothing else moved.
		t.Fatal("found app id recorded")
	}
}

func TestEnsureClient_HealVaultFailureNeverReachesThunder(t *testing.T) {
	f := newEnsureFixture(t, true, false)
	f.vault.createErr = errors.New("openbao: sealed")
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientPublisher); err == nil {
		t.Fatal("a vault failure fails the ensure")
	}
	if slices.ContainsFunc(f.log, func(e string) bool { return e == "thunder:put:app-1" }) {
		t.Fatalf("Thunder must not hold a secret AE failed to store: %v", f.log)
	}
}

func TestEnsureClient_ForeignOUFailsLoudly(t *testing.T) {
	f := newEnsureFixture(t, true, false)
	f.thunder.foreignOU = true
	err := f.svc.EnsureClient(ensureCtx(), "default", ClientStudio)
	if !errors.Is(err, thundersvc.ErrAppInForeignOU) {
		t.Fatalf("err = %v, want ErrAppInForeignOU", err)
	}
	if f.thunder.put || len(f.vault.writes) != 0 {
		t.Fatal("a foreign-OU app is never written to nor given a secret")
	}
}

func TestEnsureClient_UnknownOUTouchesNothing(t *testing.T) {
	f := newEnsureFixture(t, false, false)
	f.svc.orgRepo = ouOrgRepo{}
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientStudio); !errors.Is(err, errOrgOUUnknown) {
		t.Fatalf("err = %v, want errOrgOUUnknown", err)
	}
	if len(f.log) != 0 || len(f.thunder.specs) != 0 {
		t.Fatalf("nothing is created under a guessed OU: %v", f.log)
	}
}

func TestEnsureClient_OUMismatchTouchesNothing(t *testing.T) {
	f := newEnsureFixture(t, false, false)
	other := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: uuid.NewString()})
	for _, kind := range []ClientKind{ClientPublisher, ClientStudio} {
		if err := f.svc.EnsureClient(other, "default", kind); !errors.Is(err, errOrgOUMismatch) {
			t.Fatalf("%s: err = %v, want errOrgOUMismatch", kind, err)
		}
	}
	if len(f.log) != 0 || len(f.thunder.specs) != 0 {
		t.Fatalf("the vault path and the client OU must name one org: %v", f.log)
	}
}

// The claim carries the org's OU in another case than the row's canonical
// form: one OU, so the ensure goes ahead.
func TestEnsureClient_OUComparedAsUUIDs(t *testing.T) {
	f := newEnsureFixture(t, false, false)
	upper := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: strings.ToUpper(ensureOU.String())})
	for _, kind := range []ClientKind{ClientPublisher, ClientStudio} {
		if err := f.svc.EnsureClient(upper, "default", kind); err != nil {
			t.Fatalf("%s: %v, want the uppercase ouId to match the org's OU", kind, err)
		}
	}
	// The vault path namespace hashes the OU, so it is derived from the
	// canonical form, never the claim's case.
	if len(f.vault.writes) != 2 {
		t.Fatalf("writes = %+v, want both clients stored", f.vault.writes)
	}
	for _, w := range f.vault.writes {
		if w.ou != ensureOU.String() {
			t.Fatalf("%s written under OU %q, want the canonical %q", w.entity, w.ou, ensureOU.String())
		}
	}
}

func TestEnsureClient_HoldsTheSecretLockAcrossTheEnsure(t *testing.T) {
	f := newEnsureFixture(t, true, false)
	lock := &recordingLock{log: &f.log}
	f.svc.secretRefWriter.orgSecrets = NewOrgSecretWriter(f.vault, f.rows, lock, time.Now)
	f.thunder.onEnsure = func() {
		if lock.held {
			lock.ensuresInside++
		}
	}
	if err := f.svc.EnsureClient(ensureCtx(), "default", ClientStudio); err != nil {
		t.Fatal(err)
	}
	want := []string{"lock:ae-studio-client", "vault", "thunder:put:app-1", "unlock"}
	if !slices.Equal(f.log, want) {
		t.Fatalf("log %v, want %v (the row check, write and PUT under one lock)", f.log, want)
	}
	if lock.ensuresInside != 1 {
		t.Fatal("the Thunder ensure runs under the lock")
	}
}

// recordingLock logs lock/unlock into the shared call log.
type recordingLock struct {
	log           *[]string
	held          bool
	ensuresInside int
}

func (l *recordingLock) Lock(_ context.Context, _ string, s OrgSecret) (func(), error) {
	*l.log = append(*l.log, "lock:"+string(s))
	l.held = true
	return func() { l.held = false; *l.log = append(*l.log, "unlock") }, nil
}

// A missing or disagreeing OU is not fixed by saving the token again, so
// the submit's error names the operator action, never a retry.
func TestSubmitFailure_OUErrorsNameTheOperatorAction(t *testing.T) {
	for _, cause := range []error{errOrgOUUnknown, errOrgOUMismatch} {
		err := submitFailure(context.Background(), "default", "client:studio", fmt.Errorf("ensure studio client: %w", cause))
		var se *SectionError
		if !errors.As(err, &se) {
			t.Fatalf("%v: want a SectionError, got %#v", cause, err)
		}
		if se.Status != http.StatusConflict || strings.Contains(strings.ToLower(se.Message), "again") ||
			!strings.Contains(se.Message, "an operator must") {
			t.Fatalf("%v: %d %q, want a 409 naming the operator action, no retry", cause, se.Status, se.Message)
		}
	}
}
