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
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// ---- fakes -----------------------------------------------------------------

// fakeSMProvider stands in for a provider that creates the SecretReference
// itself (OpenChoreo's Secret API, Cloud's SM API): it names references
// `cred-<entity>-<hex>` and records the locations it is handed.
type fakeSMProvider struct {
	lastPush      SecretLocation
	lastPushValue map[string]string
	lastDelete    SecretLocation
	pushes        int
	// takenNames answers that many pushes with ErrConflict, as if each
	// minted name were already in use.
	takenNames int
}

func newFakeSMProvider() *fakeSMProvider { return &fakeSMProvider{} }

func (p *fakeSMProvider) NewClient(*StoreConfig) (SecretsClient, error) { return p, nil }
func (p *fakeSMProvider) ValidateConfig(*StoreConfig) error             { return nil }
func (p *fakeSMProvider) Capabilities() StoreCapabilities               { return StoreCapabilityWriteOnly }
func (p *fakeSMProvider) ManagesSecretReferences() bool                 { return true }

func (p *fakeSMProvider) PushSecret(_ context.Context, loc SecretLocation, value []byte, _ *SecretMetadata) (string, error) {
	p.lastPush = loc
	if p.takenNames > 0 {
		p.takenNames--
		return "", ErrConflict
	}
	p.pushes++
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

// unmanagedProvider stores values but creates no SecretReference.
type unmanagedProvider struct{ *fakeSMProvider }

func (unmanagedProvider) ManagesSecretReferences() bool { return false }

func newRefsClient(t *testing.T, p Provider) SecretManagementClient {
	t.Helper()
	c, err := NewSecretManagementClientWithConfig(SecretManagementClientConfig{StoreConfig: &StoreConfig{Provider: "test"}, Provider: p})
	if err != nil {
		t.Fatalf("NewSecretManagementClientWithConfig: %v", err)
	}
	return c
}

func githubPatLocation() SecretLocation {
	return SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "default", EntityName: "github-pat"}
}

// ---- construction ----------------------------------------------------------

func TestNewClient_RefusesAProviderThatCreatesNoReferences(t *testing.T) {
	_, err := NewSecretManagementClientWithConfig(SecretManagementClientConfig{
		StoreConfig: &StoreConfig{Provider: "test"},
		Provider:    unmanagedProvider{newFakeSMProvider()},
	})
	if err == nil {
		t.Fatal("want an error: nothing would point at the values such a provider stores")
	}
}

// ---- new reference per write -----------------------------------------------

func TestCreateSecretRef_ReturnsTheProvidersNameAndPassesAMintedOne(t *testing.T) {
	ctx := context.Background()
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)
	loc := githubPatLocation()

	name, err := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if name != "cred-github-pat-0a1b2c" {
		t.Fatalf("name %q, want the provider's", name)
	}
	if !regexp.MustCompile(`^default-github-pat-[0-9a-f]{8}$`).MatchString(sm.lastPush.RefName) {
		t.Fatalf("minted name %q, want default-github-pat-<8 hex>", sm.lastPush.RefName)
	}
}

func TestCreateSecretRef_GithubPatKeysArePushedTogether(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)
	data := map[string]string{"token": "v", "password": "v"}

	if _, err := c.CreateSecretRef(context.Background(), githubPatLocation(), data); err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if sm.pushes != 1 || !reflect.DeepEqual(sm.lastPushValue, data) {
		t.Fatalf("the two-key map must be pushed as-is in one call (pushes=%d)", sm.pushes)
	}
}

// fixedNames makes the client mint the given names in order (then the last
// one forever), so a test can see which name each attempt used.
func fixedNames(t *testing.T, c SecretManagementClient, names ...string) {
	t.Helper()
	i := 0
	c.(*secretManagementClient).mintName = func(string, string) (string, error) {
		n := names[min(i, len(names)-1)]
		i++
		return n, nil
	}
}

func TestCreateSecretRef_ClashIsRetriedOnceWithANewName(t *testing.T) {
	sm := newFakeSMProvider()
	sm.takenNames = 1
	c := newRefsClient(t, sm)
	fixedNames(t, c, "default-github-pat-00000001", "default-github-pat-00000002")

	if _, err := c.CreateSecretRef(context.Background(), githubPatLocation(), map[string]string{"token": "v"}); err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if sm.lastPush.RefName != "default-github-pat-00000002" || sm.pushes != 1 {
		t.Fatalf("stored once under %q, want the re-minted name", sm.lastPush.RefName)
	}
}

func TestCreateSecretRef_SecondClashIsAConflict(t *testing.T) {
	sm := newFakeSMProvider()
	sm.takenNames = 2
	c := newRefsClient(t, sm)

	_, err := c.CreateSecretRef(context.Background(), githubPatLocation(), map[string]string{"token": "v"})
	if !errors.Is(err, ErrConflict) || sm.pushes != 0 {
		t.Fatalf("err = %v, pushes = %d; want ErrConflict with nothing stored", err, sm.pushes)
	}
}

func TestCreateSecretRef_LongNamesFitADNSLabel(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)
	loc := githubPatLocation()
	loc.ControlPlaneNamespace = strings.Repeat("a", 60)

	if _, err := c.CreateSecretRef(context.Background(), loc, map[string]string{"token": "v"}); err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	name := sm.lastPush.RefName
	if len(name) > 63 || !regexp.MustCompile(`^a+-github-pat-[0-9a-f]{8}$`).MatchString(name) {
		t.Fatalf("name %q (len %d): want a DNS label keeping the entity, cpNS trimmed", name, len(name))
	}
}

func TestCreateSecretRef_RequiresDataAndControlPlaneNamespace(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)
	ctx := context.Background()

	if _, err := c.CreateSecretRef(ctx, githubPatLocation(), nil); err == nil {
		t.Fatal("want an error for empty data")
	}
	loc := githubPatLocation()
	loc.ControlPlaneNamespace = ""
	if _, err := c.CreateSecretRef(ctx, loc, map[string]string{"token": "v"}); err == nil {
		t.Fatal("want an error without ControlPlaneNamespace")
	}
	if sm.pushes != 0 {
		t.Fatal("a rejected call must store nothing")
	}
}

func TestDeleteSecretRef_DeletesByTheStoredName(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)

	if err := c.DeleteSecretRef(context.Background(), githubPatLocation(), "cred-github-pat-0a1b2c"); err != nil {
		t.Fatalf("DeleteSecretRef: %v", err)
	}
	if sm.lastDelete.RefName != "cred-github-pat-0a1b2c" {
		t.Fatalf("deleted %q, want the stored name", sm.lastDelete.RefName)
	}
}

func TestDeleteSecretRef_RequiresAName(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)

	if err := c.DeleteSecretRef(context.Background(), githubPatLocation(), ""); err == nil {
		t.Fatal("an empty name must be refused, never derived from the location")
	}
	if sm.lastDelete != (SecretLocation{}) {
		t.Fatal("a refused delete must remove nothing")
	}
}

// ---- fixed-name writes -----------------------------------------------------

func TestCreateSecret_PassesTheLocationAndReturnsTheProvidersName(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)
	loc := SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "default", ProjectName: "shop", EntityName: "extres-postgres-dev"}

	name, err := c.CreateSecret(context.Background(), loc, map[string]string{"password": "p"})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if name != "cred-extres-postgres-dev-0a1b2c" || sm.lastPush != loc || sm.lastPushValue["password"] != "p" {
		t.Fatalf("name %q, pushed %+v %v", name, sm.lastPush, sm.lastPushValue)
	}
}

func TestDeleteSecret_PassesTheLocation(t *testing.T) {
	sm := newFakeSMProvider()
	c := newRefsClient(t, sm)
	loc := SecretLocation{OrgName: "ou-1", ControlPlaneNamespace: "default", EntityName: "amp-model-billing-dev"}

	if err := c.DeleteSecret(context.Background(), loc, "ignored"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if sm.lastDelete != loc {
		t.Fatalf("deleted %+v, want %+v", sm.lastDelete, loc)
	}
}
