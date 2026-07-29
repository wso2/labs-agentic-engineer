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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// ---- test doubles ----------------------------------------------------------

type stubSecretsClient struct {
	pushPath   string
	patchPath  string
	deleteErr  error
	info       *SecretInfo
	getInfoErr error
}

func (s *stubSecretsClient) PushSecret(_ context.Context, _ SecretLocation, _ []byte, _ *SecretMetadata) (string, error) {
	return s.pushPath, nil
}
func (s *stubSecretsClient) PatchSecret(_ context.Context, _ SecretLocation, _ []byte, _ *SecretMetadata) (string, error) {
	return s.patchPath, nil
}
func (s *stubSecretsClient) DeleteSecret(_ context.Context, _ SecretLocation, _ *SecretMetadata) error {
	return s.deleteErr
}
func (s *stubSecretsClient) GetSecret(_ context.Context, _ SecretLocation) (*SecretInfo, error) {
	if s.getInfoErr != nil {
		return nil, s.getInfoErr
	}
	return s.info, nil
}
func (s *stubSecretsClient) GetSecretWithValue(_ context.Context, _ SecretLocation) ([]byte, error) {
	return nil, ErrNotSupported
}
func (s *stubSecretsClient) Close(_ context.Context) error { return nil }

type stubProvider struct {
	client      SecretsClient
	managesRefs bool
	caps        StoreCapabilities
}

func (p *stubProvider) NewClient(_ *StoreConfig) (SecretsClient, error) { return p.client, nil }
func (p *stubProvider) ValidateConfig(_ *StoreConfig) error             { return nil }
func (p *stubProvider) Capabilities() StoreCapabilities {
	if p.caps == "" {
		return StoreCapabilityWriteOnly
	}
	return p.caps
}
func (p *stubProvider) ManagesSecretReferences() bool { return p.managesRefs }

type recordingOCClient struct {
	gets    [][2]string
	creates []createCall
	updates []createCall
	deletes [][2]string
	getErr  error
}

type createCall struct {
	orgNS string
	req   CreateSecretReferenceRequest
}

func (r *recordingOCClient) GetSecretReference(_ context.Context, orgNS, name string) (*SecretReference, error) {
	r.gets = append(r.gets, [2]string{orgNS, name})
	if r.getErr != nil {
		return nil, r.getErr
	}
	return &SecretReference{Namespace: orgNS, Name: name}, nil
}
func (r *recordingOCClient) CreateSecretReference(_ context.Context, orgNS string, req CreateSecretReferenceRequest) (*SecretReference, error) {
	r.creates = append(r.creates, createCall{orgNS: orgNS, req: req})
	return &SecretReference{Namespace: orgNS, Name: req.Name}, nil
}
func (r *recordingOCClient) UpdateSecretReference(_ context.Context, orgNS, name string, req CreateSecretReferenceRequest) (*SecretReference, error) {
	r.updates = append(r.updates, createCall{orgNS: orgNS, req: req})
	_ = name
	return &SecretReference{Namespace: orgNS, Name: req.Name}, nil
}
func (r *recordingOCClient) DeleteSecretReference(_ context.Context, orgNS, name string) error {
	r.deletes = append(r.deletes, [2]string{orgNS, name})
	return nil
}

func testLocation() SecretLocation {
	return SecretLocation{
		OrgName:    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		EntityName: "anthropic",
	}
}

func newClientForTest(t *testing.T, p Provider, oc OpenChoreoSecretReferenceClient) SecretManagementClient {
	t.Helper()
	c, err := NewSecretManagementClientWithConfig(SecretManagementClientConfig{
		StoreConfig:     &StoreConfig{Provider: "test"},
		Provider:        p,
		OCClient:        oc,
		RefreshInterval: "1h",
	})
	if err != nil {
		t.Fatalf("NewSecretManagementClientWithConfig: %v", err)
	}
	return c
}

// ---- gating tests ----------------------------------------------------------

func TestCreateSecret_ManagesRefsFalse_AuthorsSRWithOrgBaseNamespace(t *testing.T) {
	loc := testLocation()
	wantNS := tenant.OrgBaseNamespace(loc.OrgName)
	vaultPath := "user-app-secrets/" + wantNS + "/" + loc.SecretRefName()

	oc := &recordingOCClient{getErr: ErrNotFound}
	p := &stubProvider{
		client:      &stubSecretsClient{pushPath: vaultPath},
		managesRefs: false,
	}
	c := newClientForTest(t, p, oc)

	got, err := c.CreateSecret(context.Background(), loc, map[string]string{"api-key": "sk-test"})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	wantRefName := loc.SecretRefName()
	if got != wantRefName {
		t.Fatalf("got SecretReference name %q, want %q", got, wantRefName)
	}
	if len(oc.creates) != 1 {
		t.Fatalf("expected 1 CreateSecretReference, got %d (gets=%d updates=%d)", len(oc.creates), len(oc.gets), len(oc.updates))
	}
	call := oc.creates[0]
	if call.orgNS != wantNS {
		t.Fatalf("Create orgNS = %q, want OrgBaseNamespace %q (not raw UUID)", call.orgNS, wantNS)
	}
	if call.req.Namespace != wantNS {
		t.Fatalf("req.Namespace = %q, want %q", call.req.Namespace, wantNS)
	}
	if call.req.KVPath != vaultPath {
		t.Fatalf("req.KVPath = %q, want %q", call.req.KVPath, vaultPath)
	}
	if call.req.Name != loc.SecretRefName() {
		t.Fatalf("req.Name = %q, want %q", call.req.Name, loc.SecretRefName())
	}
	if len(call.req.SecretKeys) != 1 || call.req.SecretKeys[0] != "api-key" {
		t.Fatalf("req.SecretKeys = %v", call.req.SecretKeys)
	}
	// Guard: never pass the raw UUID as the k8s namespace.
	if strings.Contains(call.orgNS, "eeeeeeeeeeee") || call.orgNS == loc.OrgName {
		t.Fatalf("orgNS must be OrgBaseNamespace, got raw-looking %q", call.orgNS)
	}
}

func TestCreateSecret_ManagesRefsTrue_SkipsOCEvenIfOCClientSet(t *testing.T) {
	loc := testLocation()
	vaultPath := "sm-api/returned-path"

	oc := &recordingOCClient{getErr: ErrNotFound}
	p := &stubProvider{
		client:      &stubSecretsClient{pushPath: vaultPath},
		managesRefs: true,
	}
	c := newClientForTest(t, p, oc)

	got, err := c.CreateSecret(context.Background(), loc, map[string]string{"api-key": "sk-test"})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if got != vaultPath {
		t.Fatalf("got path %q, want %q", got, vaultPath)
	}
	if len(oc.gets)+len(oc.creates)+len(oc.updates)+len(oc.deletes) != 0 {
		t.Fatalf("expected no OC calls when ManagesSecretReferences=true; gets=%d creates=%d updates=%d deletes=%d",
			len(oc.gets), len(oc.creates), len(oc.updates), len(oc.deletes))
	}
}

func TestCreateSecret_ManagesRefsFalse_RequiresOCClient(t *testing.T) {
	loc := testLocation()
	p := &stubProvider{
		client:      &stubSecretsClient{pushPath: "user-app-secrets/x/y"},
		managesRefs: false,
	}
	c := newClientForTest(t, p, nil)

	_, err := c.CreateSecret(context.Background(), loc, map[string]string{"api-key": "sk-test"})
	if err == nil {
		t.Fatal("expected error when OCClient nil and provider does not manage refs")
	}
	if !strings.Contains(err.Error(), "OCClient required when provider does not manage SecretReferences") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPatchSecret_ManagesRefsFalse_AuthorsSR(t *testing.T) {
	loc := testLocation()
	wantNS := tenant.OrgBaseNamespace(loc.OrgName)
	vaultPath := "user-app-secrets/" + wantNS + "/" + loc.SecretRefName()

	oc := &recordingOCClient{getErr: ErrNotFound}
	p := &stubProvider{
		client: &stubSecretsClient{
			patchPath: vaultPath,
			info:      &SecretInfo{Keys: []string{"api-key", "extra"}},
		},
		managesRefs: false,
	}
	c := newClientForTest(t, p, oc)

	got, err := c.PatchSecret(context.Background(), loc, map[string]string{"extra": "v"}, nil)
	if err != nil {
		t.Fatalf("PatchSecret: %v", err)
	}
	wantRefName := loc.SecretRefName()
	if got != wantRefName {
		t.Fatalf("got SecretReference name %q, want %q", got, wantRefName)
	}
	if len(oc.creates) != 1 {
		t.Fatalf("expected 1 create, got %d", len(oc.creates))
	}
	if oc.creates[0].orgNS != wantNS {
		t.Fatalf("orgNS = %q, want %q", oc.creates[0].orgNS, wantNS)
	}
}

func TestPatchSecret_ManagesRefsTrue_SkipsOC(t *testing.T) {
	oc := &recordingOCClient{}
	p := &stubProvider{
		client:      &stubSecretsClient{patchPath: "sm-api/path"},
		managesRefs: true,
	}
	c := newClientForTest(t, p, oc)

	_, err := c.PatchSecret(context.Background(), testLocation(), map[string]string{"k": "v"}, nil)
	if err != nil {
		t.Fatalf("PatchSecret: %v", err)
	}
	if len(oc.gets)+len(oc.creates)+len(oc.updates) != 0 {
		t.Fatal("expected no OC calls")
	}
}

func TestDeleteSecret_ManagesRefsFalse_DeletesSRWithOrgBaseNamespace(t *testing.T) {
	loc := testLocation()
	wantNS := tenant.OrgBaseNamespace(loc.OrgName)
	refName := loc.SecretRefName()

	oc := &recordingOCClient{}
	p := &stubProvider{
		client:      &stubSecretsClient{},
		managesRefs: false,
	}
	c := newClientForTest(t, p, oc)

	if err := c.DeleteSecret(context.Background(), loc, refName); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if len(oc.deletes) != 1 {
		t.Fatalf("expected 1 delete, got %d", len(oc.deletes))
	}
	if oc.deletes[0][0] != wantNS || oc.deletes[0][1] != refName {
		t.Fatalf("delete call = %v, want [%s %s]", oc.deletes[0], wantNS, refName)
	}
}

func TestDeleteSecret_ManagesRefsTrue_SkipsOC(t *testing.T) {
	oc := &recordingOCClient{}
	p := &stubProvider{
		client:      &stubSecretsClient{},
		managesRefs: true,
	}
	c := newClientForTest(t, p, oc)

	if err := c.DeleteSecret(context.Background(), testLocation(), "anthropic-secrets"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if len(oc.deletes) != 0 {
		t.Fatalf("expected no OC deletes, got %d", len(oc.deletes))
	}
}

func TestDeleteSecret_ManagesRefsFalse_RequiresOCClient(t *testing.T) {
	p := &stubProvider{
		client:      &stubSecretsClient{},
		managesRefs: false,
	}
	c := newClientForTest(t, p, nil)

	err := c.DeleteSecret(context.Background(), testLocation(), "anthropic-secrets")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "OCClient required when provider does not manage SecretReferences") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSecret_ManagesRefsFalse_UpdateWhenExists(t *testing.T) {
	loc := testLocation()
	wantNS := tenant.OrgBaseNamespace(loc.OrgName)
	vaultPath := "user-app-secrets/" + wantNS + "/" + loc.SecretRefName()

	oc := &recordingOCClient{} // get succeeds → update path
	p := &stubProvider{
		client:      &stubSecretsClient{pushPath: vaultPath},
		managesRefs: false,
	}
	c := newClientForTest(t, p, oc)

	_, err := c.CreateSecret(context.Background(), loc, map[string]string{"api-key": "x"})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if len(oc.updates) != 1 || len(oc.creates) != 0 {
		t.Fatalf("expected update-only; creates=%d updates=%d", len(oc.creates), len(oc.updates))
	}
	if oc.updates[0].orgNS != wantNS {
		t.Fatalf("update orgNS = %q, want %q", oc.updates[0].orgNS, wantNS)
	}
}

// Ensure stubProvider satisfies SecretReferenceManager (compile-time).
var (
	_ SecretReferenceManager          = (*stubProvider)(nil)
	_ Provider                        = (*stubProvider)(nil)
	_ SecretsClient                   = (*stubSecretsClient)(nil)
	_ OpenChoreoSecretReferenceClient = (*recordingOCClient)(nil)
)
