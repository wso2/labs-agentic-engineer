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
	pushPath    string
	patchPath   string
	deleteErr   error
	info        *SecretInfo
	getInfoErr  error
	pushCalls   int
	patchCalls  int
	deleteCalls int
}

func (s *stubSecretsClient) PushSecret(_ context.Context, _ SecretLocation, _ []byte, _ *SecretMetadata) (string, error) {
	s.pushCalls++
	return s.pushPath, nil
}
func (s *stubSecretsClient) PatchSecret(_ context.Context, _ SecretLocation, _ []byte, _ *SecretMetadata) (string, error) {
	s.patchCalls++
	return s.patchPath, nil
}
func (s *stubSecretsClient) DeleteSecret(_ context.Context, _ SecretLocation, _ *SecretMetadata) error {
	s.deleteCalls++
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
	cpNS string
	req  CreateSecretReferenceRequest
}

func (r *recordingOCClient) GetSecretReference(_ context.Context, cpNS, name string) (*SecretReference, error) {
	r.gets = append(r.gets, [2]string{cpNS, name})
	if r.getErr != nil {
		return nil, r.getErr
	}
	return &SecretReference{Namespace: cpNS, Name: name}, nil
}
func (r *recordingOCClient) CreateSecretReference(_ context.Context, cpNS string, req CreateSecretReferenceRequest) (*SecretReference, error) {
	r.creates = append(r.creates, createCall{cpNS: cpNS, req: req})
	return &SecretReference{Namespace: cpNS, Name: req.Name}, nil
}
func (r *recordingOCClient) UpdateSecretReference(_ context.Context, cpNS, name string, req CreateSecretReferenceRequest) (*SecretReference, error) {
	r.updates = append(r.updates, createCall{cpNS: cpNS, req: req})
	_ = name
	return &SecretReference{Namespace: cpNS, Name: req.Name}, nil
}
func (r *recordingOCClient) DeleteSecretReference(_ context.Context, cpNS, name string) error {
	r.deletes = append(r.deletes, [2]string{cpNS, name})
	return nil
}

func testLocation() SecretLocation {
	return SecretLocation{
		OrgName:    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		EntityName: "anthropic",
	}
}

// testLocationWithCP is the OpenBao-direct authoring case: vault path is
// derived from the org UUID, but the SecretReference CR must land in the
// OC control-plane namespace (same ns as Workload/ReleaseBinding).
func testLocationWithCP() SecretLocation {
	loc := testLocation()
	loc.ControlPlaneNamespace = "default"
	return loc
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

func TestCreateSecret_ManagesRefsFalse_AuthorsSRInControlPlaneNamespace(t *testing.T) {
	loc := testLocationWithCP()
	vaultNS := tenant.OrgBaseNamespace(loc.OrgName)
	wantCRNS := loc.ControlPlaneNamespace
	vaultPath := "user-app-secrets/" + vaultNS + "/" + loc.SecretRefName()

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
	if call.cpNS != wantCRNS {
		t.Fatalf("Create cpNS = %q, want control-plane ns %q", call.cpNS, wantCRNS)
	}
	if call.req.Namespace != wantCRNS {
		t.Fatalf("req.Namespace = %q, want %q", call.req.Namespace, wantCRNS)
	}
	if call.cpNS == vaultNS {
		t.Fatalf("CR namespace %q must not be the vault OrgBaseNamespace", call.cpNS)
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
	if strings.Contains(call.cpNS, "eeeeeeeeeeee") || call.cpNS == loc.OrgName {
		t.Fatalf("cpNS must not be the raw org UUID, got %q", call.cpNS)
	}
}

func TestCreateSecret_ManagesRefsFalse_RequiresControlPlaneNamespace(t *testing.T) {
	loc := testLocation() // no ControlPlaneNamespace
	vaultNS := tenant.OrgBaseNamespace(loc.OrgName)
	vaultPath := "user-app-secrets/" + vaultNS + "/" + loc.SecretRefName()

	oc := &recordingOCClient{getErr: ErrNotFound}
	p := &stubProvider{
		client:      &stubSecretsClient{pushPath: vaultPath},
		managesRefs: false,
	}
	c := newClientForTest(t, p, oc)
	low := p.client.(*stubSecretsClient)

	_, err := c.CreateSecret(context.Background(), loc, map[string]string{"api-key": "sk-test"})
	if err == nil {
		t.Fatal("expected error when ControlPlaneNamespace is empty")
	}
	if !strings.Contains(err.Error(), "ControlPlaneNamespace") {
		t.Fatalf("unexpected error: %v", err)
	}
	if low.pushCalls != 0 {
		t.Fatalf("must not PushSecret without ControlPlaneNamespace; pushes=%d", low.pushCalls)
	}
	if len(oc.creates)+len(oc.updates) != 0 {
		t.Fatalf("must not author a SecretReference without ControlPlaneNamespace; creates=%d updates=%d", len(oc.creates), len(oc.updates))
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
	loc := testLocationWithCP()
	wantNS := loc.ControlPlaneNamespace
	vaultPath := "user-app-secrets/" + tenant.OrgBaseNamespace(loc.OrgName) + "/" + loc.SecretRefName()

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
	if oc.creates[0].cpNS != wantNS {
		t.Fatalf("cpNS = %q, want %q", oc.creates[0].cpNS, wantNS)
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

func TestDeleteSecret_ManagesRefsFalse_RequiresControlPlaneNamespace(t *testing.T) {
	loc := testLocation()
	oc := &recordingOCClient{}
	low := &stubSecretsClient{}
	p := &stubProvider{client: low, managesRefs: false}
	c := newClientForTest(t, p, oc)

	err := c.DeleteSecret(context.Background(), loc, loc.SecretRefName())
	if err == nil {
		t.Fatal("expected error when ControlPlaneNamespace is empty")
	}
	if !strings.Contains(err.Error(), "ControlPlaneNamespace") {
		t.Fatalf("unexpected error: %v", err)
	}
	if low.deleteCalls != 0 {
		t.Fatalf("must not DeleteSecret without ControlPlaneNamespace; deletes=%d", low.deleteCalls)
	}
	if len(oc.deletes) != 0 {
		t.Fatalf("must not delete SecretReference without ControlPlaneNamespace; oc.deletes=%d", len(oc.deletes))
	}
}

func TestPatchSecret_ManagesRefsFalse_RequiresControlPlaneNamespace(t *testing.T) {
	loc := testLocation()
	oc := &recordingOCClient{getErr: ErrNotFound}
	low := &stubSecretsClient{patchPath: "user-app-secrets/x/y"}
	p := &stubProvider{client: low, managesRefs: false}
	c := newClientForTest(t, p, oc)

	_, err := c.PatchSecret(context.Background(), loc, map[string]string{"k": "v"}, nil)
	if err == nil {
		t.Fatal("expected error when ControlPlaneNamespace is empty")
	}
	if !strings.Contains(err.Error(), "ControlPlaneNamespace") {
		t.Fatalf("unexpected error: %v", err)
	}
	if low.patchCalls != 0 {
		t.Fatalf("must not PatchSecret without ControlPlaneNamespace; patches=%d", low.patchCalls)
	}
}

func TestDeleteSecret_ManagesRefsFalse_DeletesSRInControlPlaneNamespace(t *testing.T) {
	loc := testLocationWithCP()
	wantNS := loc.ControlPlaneNamespace
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
	oldNS := tenant.OrgBaseNamespace(loc.OrgName)
	if len(oc.deletes) != 2 {
		t.Fatalf("expected CP-ns + leftover OrgBaseNamespace deletes, got %d: %v", len(oc.deletes), oc.deletes)
	}
	if oc.deletes[0][0] != wantNS || oc.deletes[0][1] != refName {
		t.Fatalf("first delete = %v, want [%s %s]", oc.deletes[0], wantNS, refName)
	}
	if oc.deletes[1][0] != oldNS || oc.deletes[1][1] != refName {
		t.Fatalf("leftover delete = %v, want [%s %s]", oc.deletes[1], oldNS, refName)
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
	loc := testLocationWithCP()
	wantNS := loc.ControlPlaneNamespace
	vaultPath := "user-app-secrets/" + tenant.OrgBaseNamespace(loc.OrgName) + "/" + loc.SecretRefName()

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
	if oc.updates[0].cpNS != wantNS {
		t.Fatalf("update cpNS = %q, want %q", oc.updates[0].cpNS, wantNS)
	}
}

// Ensure stubProvider satisfies SecretReferenceManager (compile-time).
var (
	_ SecretReferenceManager          = (*stubProvider)(nil)
	_ Provider                        = (*stubProvider)(nil)
	_ SecretsClient                   = (*stubSecretsClient)(nil)
	_ OpenChoreoSecretReferenceClient = (*recordingOCClient)(nil)
)
