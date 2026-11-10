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

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

const (
	staticPublisherSecret = "imported-publisher-secret"
	staticStudioSecret    = "imported-studio-secret"
)

func newStatic() *StaticOrgClients {
	return NewStaticOrgClients("default", staticPublisherSecret, staticStudioSecret)
}

// The static registry answers for the one org's two pre-created clients:
// their fixed client ids and the imported secrets, and "store this secret"
// (Created) on every ensure.
func TestStaticOrgClients_Ensure(t *testing.T) {
	c := newStatic()
	ctx := context.Background()

	pub, err := c.EnsurePublisherApp(ctx, "default", "ou-ignored", "stored-ignored")
	if err != nil {
		t.Fatalf("EnsurePublisherApp: %v", err)
	}
	want := thundersvc.OrgApp{EntityID: "aep-publisher-default", ClientID: "aep-publisher-default", Secret: staticPublisherSecret, Created: true}
	if pub != want {
		t.Fatalf("publisher = %+v, want %+v", pub, want)
	}

	studio, err := c.EnsureOrgApp(ctx, thundersvc.OrgAppSpec{Name: "ae-studio-default", OUID: "ou-ignored"})
	if err != nil {
		t.Fatalf("EnsureOrgApp: %v", err)
	}
	want = thundersvc.OrgApp{EntityID: "ae-studio-default", ClientID: "ae-studio-default", Secret: staticStudioSecret, Created: true}
	if studio != want {
		t.Fatalf("studio = %+v, want %+v", studio, want)
	}
}

// Only the configured org has clients, and only its AE Studio client is an
// org app: nothing else is handed a secret.
func TestStaticOrgClients_RefusesAnythingElse(t *testing.T) {
	c := newStatic()
	ctx := context.Background()
	if app, err := c.EnsurePublisherApp(ctx, "globex", "", ""); err == nil || app.Secret != "" {
		t.Fatalf("another org's publisher: app=%+v err=%v, want refused", app, err)
	}
	for _, name := range []string{"ae-studio-globex", "aep-publisher-default", ""} {
		if app, err := c.EnsureOrgApp(ctx, thundersvc.OrgAppSpec{Name: name}); err == nil || app.Secret != "" {
			t.Fatalf("org app %q: app=%+v err=%v, want refused", name, app, err)
		}
	}
}

// A registry built without an org or without a secret hands out nothing: the
// error names the setting, never a secret.
func TestStaticOrgClients_MissingSettings(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		c    *StaticOrgClients
		want string
	}{
		"no publisher secret": {NewStaticOrgClients("default", "", staticStudioSecret), "PLATFORM_IDP_ORG_PUBLISHER_SECRET"},
		"no studio secret":    {NewStaticOrgClients("default", staticPublisherSecret, ""), "PLATFORM_IDP_ORG_STUDIO_SECRET"},
	} {
		t.Run(name, func(t *testing.T) {
			_, perr := tc.c.EnsurePublisherApp(ctx, "default", "", "")
			_, serr := tc.c.EnsureOrgApp(ctx, thundersvc.OrgAppSpec{Name: "ae-studio-default"})
			joined := ""
			for _, err := range []error{perr, serr} {
				if err != nil {
					joined += err.Error()
				}
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("errors %q do not name %s", joined, tc.want)
			}
			if strings.Contains(joined, staticPublisherSecret) || strings.Contains(joined, staticStudioSecret) {
				t.Fatalf("an error carries a secret: %q", joined)
			}
		})
	}
	if _, err := NewStaticOrgClients("", staticPublisherSecret, staticStudioSecret).EnsurePublisherApp(ctx, "", "", ""); err == nil {
		t.Fatal("a registry with no org served an empty org handle")
	}
}

// The apps are an admin's: aep-api cannot set a secret on one, and a delete
// removes nothing (the caller still drops its own record).
func TestStaticOrgClients_WritesAreNotAepApis(t *testing.T) {
	c := newStatic()
	ctx := context.Background()
	if err := c.SetAppSecret(ctx, "aep-publisher-default", "new"); err == nil {
		t.Fatal("SetAppSecret = nil, want an error: the secret is rotated in the IdP")
	}
	deleted, err := c.DeletePublisherApp(ctx, "default", "aep-publisher-default")
	if deleted || err != nil {
		t.Fatalf("DeletePublisherApp = %v, %v; want false, nil", deleted, err)
	}
}

// newStaticEnsureFixture is newEnsureFixture over the static registry: the
// real idpService and writers, no Thunder.
func newStaticEnsureFixture(t *testing.T) *ensureFixture {
	t.Helper()
	f := &ensureFixture{rows: newMemOrgSecretRepo(), idp: newMemIDPRepo()}
	f.vault = &ensureVault{log: &f.log, refs: map[string]bool{}}
	ou := ensureOU
	writer := NewSecretRefWriter(f.vault, f.idp).
		WithOrgSecretWriter(NewOrgSecretWriter(f.vault, f.rows, memOrgSecretLock{}, time.Now))
	f.svc = NewIDPService(f.idp, ouOrgRepo{ou: &ou}, newStatic(), PlatformIDPConfig{}).WithSecretRefWriter(writer)
	return f
}

// EnsureClient over the static registry stores the imported secret in the
// org's reference and records the fixed client id, with no IdP call. A
// reconnect stores it again under a new reference and retires the old one,
// which is how a rotated, re-imported secret reaches the org.
func TestEnsureClient_StaticRegistry(t *testing.T) {
	for _, tc := range []struct {
		kind     ClientKind
		secret   OrgSecret
		clientID string
		value    string
	}{
		{ClientPublisher, OrgSecretPublisherClient, "aep-publisher-default", staticPublisherSecret},
		{ClientStudio, OrgSecretStudioClient, "ae-studio-default", staticStudioSecret},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			f := newStaticEnsureFixture(t)
			if err := f.svc.EnsureClient(ensureCtx(), "default", tc.kind); err != nil {
				t.Fatalf("first connect: %v", err)
			}
			first := f.row(tc.secret)
			if first == "" || !f.vault.refs[first] {
				t.Fatalf("the row names no live reference: %q %v", first, f.vault.refs)
			}
			w := f.vault.writes[0]
			if w.entity != string(tc.secret) || w.data["client_id"] != tc.clientID || w.data["client_secret"] != tc.value {
				t.Fatalf("the reference holds entity %q client_id %q, want %q %q with the imported secret", w.entity, w.data["client_id"], tc.secret, tc.clientID)
			}
			p := f.profile(t)
			if tc.kind == ClientPublisher && p.PublisherClientID != tc.clientID {
				t.Fatalf("publisher client id = %q", p.PublisherClientID)
			}
			if tc.kind == ClientStudio && p.StudioClientID != tc.clientID {
				t.Fatalf("studio client id = %q", p.StudioClientID)
			}

			if err := f.svc.EnsureClient(ensureCtx(), "default", tc.kind); err != nil {
				t.Fatalf("reconnect: %v", err)
			}
			second := f.row(tc.secret)
			if second == first || !f.vault.refs[second] || f.vault.refs[first] {
				t.Fatalf("reconnect: row %q -> %q, live refs %v; want a new reference and the old one retired", first, second, f.vault.refs)
			}
			if len(f.vault.writes) != 2 || f.vault.writes[1].data["client_secret"] != tc.value {
				t.Fatalf("reconnect wrote %d references", len(f.vault.writes))
			}
		})
	}
}
