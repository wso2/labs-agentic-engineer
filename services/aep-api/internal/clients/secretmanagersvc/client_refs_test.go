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

package secretmanagersvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc/providers/openbao"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// ---- fakes -----------------------------------------------------------------

// fakeKV is an in-memory KV-v2 server behind the real OpenBao provider, so the
// vault path under test is the one production computes.
type fakeKV struct {
	mu     sync.Mutex
	data   map[string]map[string]string // key: path under the mount
	writes int
	srv    *httptest.Server
}

const fakeKVDataPrefix = "/v1/secret/data/"
const fakeKVMetaPrefix = "/v1/secret/metadata/"

func newFakeKV(t *testing.T) *fakeKV {
	t.Helper()
	kv := &fakeKV{data: map[string]map[string]string{}}
	kv.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kv.mu.Lock()
		defer kv.mu.Unlock()
		switch {
		case (r.Method == http.MethodPut || r.Method == http.MethodPost) && strings.HasPrefix(r.URL.Path, fakeKVDataPrefix):
			var body struct {
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			kv.writes++
			kv.data[strings.TrimPrefix(r.URL.Path, fakeKVDataPrefix)] = body.Data
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, fakeKVMetaPrefix):
			p := strings.TrimPrefix(r.URL.Path, fakeKVMetaPrefix)
			if _, ok := kv.data[p]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(kv.data, p)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(kv.srv.Close)
	return kv
}

func (kv *fakeKV) provider(t *testing.T) Provider {
	t.Helper()
	p, err := openbao.NewProvider(&OpenBaoConfig{
		Server: kv.srv.URL,
		Path:   "secret",
		Auth:   &OpenBaoAuth{Token: "test-token"},
	})
	if err != nil {
		t.Fatalf("openbao.NewProvider: %v", err)
	}
	return p
}

// has reports whether the vault holds a value for the reference name.
func (kv *fakeKV) has(refName string) bool {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	for p := range kv.data {
		if strings.HasSuffix(p, "/"+refName) {
			return true
		}
	}
	return false
}

func (kv *fakeKV) at(path string) map[string]string {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	return kv.data[path]
}

// fakeOC is an in-memory OC SecretReference store: Create refuses an existing
// name, Delete of a missing name is ErrNotFound.
type fakeOC struct {
	created   map[string]CreateSecretReferenceRequest
	updates   int
	createErr error
}

func newFakeOC() *fakeOC { return &fakeOC{created: map[string]CreateSecretReferenceRequest{}} }

func (f *fakeOC) exists(name string) bool { _, ok := f.created[name]; return ok }

func (f *fakeOC) GetSecretReference(_ context.Context, cpNS, name string) (*SecretReference, error) {
	if !f.exists(name) {
		return nil, ErrNotFound
	}
	return &SecretReference{Namespace: cpNS, Name: name}, nil
}

func (f *fakeOC) CreateSecretReference(_ context.Context, cpNS string, req CreateSecretReferenceRequest) (*SecretReference, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.exists(req.Name) {
		return nil, ErrConflict
	}
	f.created[req.Name] = req
	return &SecretReference{Namespace: cpNS, Name: req.Name}, nil
}

func (f *fakeOC) UpdateSecretReference(_ context.Context, cpNS, name string, _ CreateSecretReferenceRequest) (*SecretReference, error) {
	f.updates++
	return &SecretReference{Namespace: cpNS, Name: name}, nil
}

func (f *fakeOC) DeleteSecretReference(_ context.Context, _, name string) error {
	if !f.exists(name) {
		return ErrNotFound
	}
	delete(f.created, name)
	return nil
}

// fakeSMProvider stands in for the Cloud overlay's SM API v1 provider: it
// names references itself (`cred-<secret>-<hex>`) and records the locations it
// is handed.
type fakeSMProvider struct {
	lastPush      SecretLocation
	lastPushValue map[string]string
	lastDelete    SecretLocation
	pushes        int
}

func newFakeSMProvider() *fakeSMProvider { return &fakeSMProvider{} }

func (p *fakeSMProvider) NewClient(*StoreConfig) (SecretsClient, error) { return p, nil }
func (p *fakeSMProvider) ValidateConfig(*StoreConfig) error             { return nil }
func (p *fakeSMProvider) Capabilities() StoreCapabilities               { return StoreCapabilityWriteOnly }
func (p *fakeSMProvider) ManagesSecretReferences() bool                 { return true }

func (p *fakeSMProvider) PushSecret(_ context.Context, loc SecretLocation, value []byte, _ *SecretMetadata) (string, error) {
	p.pushes++
	p.lastPush = loc
	p.lastPushValue = nil
	if err := json.Unmarshal(value, &p.lastPushValue); err != nil {
		return "", err
	}
	return "cred-" + loc.EntityName + "-0a1b2c", nil
}
func (p *fakeSMProvider) PatchSecret(context.Context, SecretLocation, []byte, *SecretMetadata) (string, error) {
	return "", ErrNotSupported
}
func (p *fakeSMProvider) DeleteSecret(_ context.Context, loc SecretLocation, _ *SecretMetadata) error {
	p.lastDelete = loc
	return nil
}
func (p *fakeSMProvider) GetSecret(context.Context, SecretLocation) (*SecretInfo, error) {
	return nil, ErrNotSupported
}
func (p *fakeSMProvider) GetSecretWithValue(context.Context, SecretLocation) ([]byte, error) {
	return nil, ErrNotSupported
}
func (p *fakeSMProvider) Close(context.Context) error { return nil }

func newRefsClient(t *testing.T, p Provider, oc *fakeOC) SecretManagementClient {
	t.Helper()
	cfg := SecretManagementClientConfig{StoreConfig: &StoreConfig{Provider: "test"}, Provider: p, RefreshInterval: "1h"}
	if oc != nil {
		cfg.OCClient = oc
	}
	c, err := NewSecretManagementClientWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewSecretManagementClientWithConfig: %v", err)
	}
	return c
}

func githubPatLocation() SecretLocation {
	return SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "default", EntityName: "github-pat"}
}

// ---- local install (OpenBao + OC-authored references) ----------------------

func TestCreateSecretRef_OpenBaoMintsANewNameEachTime(t *testing.T) {
	ctx := context.Background()
	kv, oc := newFakeKV(t), newFakeOC()
	c := newRefsClient(t, kv.provider(t), oc)
	loc := githubPatLocation()

	a, err := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v1"})
	b, err2 := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v2"})
	if err != nil || err2 != nil || a == b {
		t.Fatalf("a=%q b=%q err=%v/%v", a, b, err, err2)
	}
	if !regexp.MustCompile(`^default-github-pat-[0-9a-f]{8}$`).MatchString(a) {
		t.Fatalf("name %q", a)
	}
	wantPath := "user-app-secrets/" + tenant.OrgBaseNamespace("ou-1") + "/" + a
	if got := oc.created[a].KVPath; got != wantPath {
		t.Fatalf("kv path %q, want %q", got, wantPath)
	}
	if got := kv.at(wantPath); got["token"] != "v1" {
		t.Fatal("the value must be written at the minted reference's vault path")
	}
	if oc.updates != 0 {
		t.Fatal("a write must never update an existing reference")
	}
}

func TestCreateSecretRef_GithubPatCarriesTokenAndPassword(t *testing.T) {
	ctx := context.Background()
	kv, oc := newFakeKV(t), newFakeOC()
	c := newRefsClient(t, kv.provider(t), oc)

	name, err := c.CreateSecretRef(ctx, githubPatLocation(), map[string]string{"token": "v", "password": "v"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if kv.writes != 1 {
		t.Fatalf("both keys must land in one write, got %d writes", kv.writes)
	}
	req := oc.created[name]
	stored := kv.at(req.KVPath)
	if len(stored) != 2 || stored["token"] != "v" || stored["password"] != "v" {
		t.Fatalf("vault entry must hold token and password with the same value, got keys %d", len(stored))
	}
	if want := []string{"password", "token"}; !reflect.DeepEqual(req.SecretKeys, want) {
		t.Fatalf("SecretKeys = %v, want %v (sorted; each property = its key)", req.SecretKeys, want)
	}
}

func TestCreateSecretRef_ConflictIsAnErrorNeverAnUpdate(t *testing.T) {
	kv, oc := newFakeKV(t), newFakeOC()
	oc.createErr = ErrConflict
	c := newRefsClient(t, kv.provider(t), oc)

	_, err := c.CreateSecretRef(context.Background(), githubPatLocation(), map[string]string{"token": "v"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if oc.updates != 0 {
		t.Fatal("a conflict must never fall back to an update")
	}
}

func TestCreateSecretRef_ReferenceFailureRemovesTheNewValue(t *testing.T) {
	kv, oc := newFakeKV(t), newFakeOC()
	oc.createErr = errors.New("oc unavailable")
	c := newRefsClient(t, kv.provider(t), oc)

	if _, err := c.CreateSecretRef(context.Background(), githubPatLocation(), map[string]string{"token": "v"}); err == nil {
		t.Fatal("want an error when the reference cannot be created")
	}
	if kv.writes != 1 || len(kv.data) != 0 {
		t.Fatalf("the value written for the failed reference must be removed (writes=%d left=%d)", kv.writes, len(kv.data))
	}
}

func TestCreateSecretRef_LongNamesFitADNSLabel(t *testing.T) {
	kv, oc := newFakeKV(t), newFakeOC()
	c := newRefsClient(t, kv.provider(t), oc)
	loc := githubPatLocation()
	loc.ControlPlaneNamespace = strings.Repeat("a", 60)

	name, err := c.CreateSecretRef(context.Background(), loc, map[string]string{"token": "v"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if len(name) > 63 || !regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?-[0-9a-f]{8}$`).MatchString(name) {
		t.Fatalf("name %q (len %d) is not a DNS label ending in 8 hex", name, len(name))
	}
}

func TestCreateSecretRef_RequiresDataAndControlPlaneNamespace(t *testing.T) {
	kv, oc := newFakeKV(t), newFakeOC()
	c := newRefsClient(t, kv.provider(t), oc)
	ctx := context.Background()

	if _, err := c.CreateSecretRef(ctx, githubPatLocation(), nil); err == nil {
		t.Fatal("want an error for empty data")
	}
	loc := githubPatLocation()
	loc.ControlPlaneNamespace = ""
	if _, err := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v"}); err == nil {
		t.Fatal("want an error without ControlPlaneNamespace")
	}
	if kv.writes != 0 || len(oc.created) != 0 {
		t.Fatal("a rejected call must write nothing")
	}
}

func TestDeleteSecretRef_ByNameOnly(t *testing.T) {
	ctx := context.Background()
	kv, oc := newFakeKV(t), newFakeOC()
	c := newRefsClient(t, kv.provider(t), oc)
	loc := githubPatLocation()

	a, _ := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v1"})
	b, _ := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v2"})
	if err := c.DeleteSecretRef(ctx, loc, a); err != nil {
		t.Fatal(err)
	}
	if oc.exists(a) || !oc.exists(b) || kv.has(a) || !kv.has(b) {
		t.Fatal("delete must remove exactly the named reference and its value")
	}
	if err := c.DeleteSecretRef(ctx, loc, a); err != nil {
		t.Fatalf("second delete must be a no-op, got %v", err)
	}
}

func TestDeleteSecretRef_RequiresAName(t *testing.T) {
	kv, oc := newFakeKV(t), newFakeOC()
	c := newRefsClient(t, kv.provider(t), oc)
	ctx := context.Background()
	loc := githubPatLocation()
	b, _ := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v"})

	if err := c.DeleteSecretRef(ctx, loc, ""); err == nil {
		t.Fatal("an empty name must be refused, never derived from the location")
	}
	if !oc.exists(b) || !kv.has(b) {
		t.Fatal("a refused delete must remove nothing")
	}
}

// ---- Cloud install (provider names references itself) ----------------------

func TestCreateSecretRef_ManagedProviderReturnsItsName(t *testing.T) {
	ctx := context.Background()
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm, nil)
	loc := SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "wc-x", EntityName: "github-pat"}

	name, err := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if !strings.HasPrefix(name, "cred-github-pat-") || sm.lastPush.RefName == "" {
		t.Fatalf("name %q; the minted hint must still be passed", name)
	}
	if err := c.DeleteSecretRef(ctx, loc, name); err != nil {
		t.Fatalf("DeleteSecretRef: %v", err)
	}
	if sm.lastDelete.RefName != name {
		t.Fatal("managed delete must carry the stored name")
	}
}

func TestCreateSecretRef_ManagedProviderGetsBothGithubPatKeys(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm, nil)
	data := map[string]string{"token": "v", "password": "v"}

	if _, err := c.CreateSecretRef(context.Background(), SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "wc-x", EntityName: "github-pat"}, data); err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if sm.pushes != 1 || !reflect.DeepEqual(sm.lastPushValue, data) {
		t.Fatalf("the two-key map must be pushed as-is in one call (pushes=%d)", sm.pushes)
	}
}
