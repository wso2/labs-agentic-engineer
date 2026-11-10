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

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/oidc"
)

// testAsgardeoIDP is an Asgardeo organization as discovery names it. There is
// no in-cluster address: the pods reach the public endpoints.
var testAsgardeoIDP = func() platformIDP {
	pub := oidc.Endpoints{
		Issuer:   "https://api.asgardeo.io/t/acmecorp/oauth2/token",
		JWKSURL:  "https://api.asgardeo.io/t/acmecorp/oauth2/jwks",
		TokenURL: "https://api.asgardeo.io/t/acmecorp/oauth2/token",
	}
	return platformIDP{Kind: "asgardeo", Public: pub, InCluster: pub, OrgHandle: "acmecorp", OrgNamespace: "default"}
}()

func setAsgardeoConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(viper.Reset)
	viper.Set("idp.kind", "asgardeo")
	viper.Set("idp.issuer", "https://api.asgardeo.io/t/acmecorp/oauth2/token")
	viper.Set("idp.org_handle", "acmecorp")
	viper.Set("oc.api_url", "http://api.openchoreo.localhost:8080")
	viper.Set("oc.system_namespace", "openchoreo-control-plane")
	viper.Set("console.public_url", "http://console.ae.localhost:8080")
}

// On Asgardeo the document is read over idp.issuer, the pods get the public
// endpoints whatever thunder.url says, and the one org's handle and
// namespace travel with the IdP.
func TestDiscoverPlatformIDP_Asgardeo(t *testing.T) {
	setAsgardeoConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/t/acmecorp/oauth2/token/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		base := srvURL(r) + "/t/acmecorp/oauth2"
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"jwks_uri":%q,"token_endpoint":%q}`, base+"/token", base+"/jwks", base+"/token")
	}))
	t.Cleanup(srv.Close)
	issuer := srv.URL + "/t/acmecorp/oauth2/token"
	viper.Set("idp.issuer", issuer)
	// Left over from a Thunder config: not read on Asgardeo.
	viper.Set("thunder.url", "http://thunder-service.thunder.svc.cluster.local:8090")
	viper.Set("thunder.public_url", "http://thunder.openchoreo.localhost:8080")

	idp, err := discoverPlatformIDP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if idp.Kind != "asgardeo" || idp.Public.Issuer != issuer || idp.Public.JWKSURL != srv.URL+"/t/acmecorp/oauth2/jwks" {
		t.Fatalf("public: %+v", idp)
	}
	if idp.InCluster != idp.Public {
		t.Fatalf("in-cluster endpoints %+v are not the public ones", idp.InCluster)
	}
	if idp.OrgHandle != "acmecorp" || idp.OrgNamespace != "default" {
		t.Fatalf("org: handle %q namespace %q, want acmecorp and the default namespace", idp.OrgHandle, idp.OrgNamespace)
	}
	viper.Set("oc.default_org_namespace", "engineering")
	if idp, err = discoverPlatformIDP(context.Background()); err != nil || idp.OrgNamespace != "engineering" {
		t.Fatalf("org namespace = %q, %v; want oc.default_org_namespace", idp.OrgNamespace, err)
	}
}

// The chart is told the kind, the public endpoints and the one org; a Thunder
// install is told no org.
func TestPlatformIDPOverrides_Asgardeo(t *testing.T) {
	got := strings.Join(platformIDPOverrides(testAsgardeoIDP), " ")
	for _, want := range []string{
		"--set idp.kind=asgardeo",
		"--set idp.orgHandle=acmecorp",
		"--set idp.orgNamespace=default",
		"--set thunder.jwksURL=https://api.asgardeo.io/t/acmecorp/oauth2/jwks",
		"--set thunder.tokenURL=https://api.asgardeo.io/t/acmecorp/oauth2/token",
		"--set aeStudio.idp.issuer=https://api.asgardeo.io/t/acmecorp/oauth2/token",
		"--set aeStudio.idp.jwksUrl=https://api.asgardeo.io/t/acmecorp/oauth2/jwks",
		"--set aeStudio.idp.tokenUrl=https://api.asgardeo.io/t/acmecorp/oauth2/token",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if thunder := strings.Join(platformIDPOverrides(testPlatformIDP), " "); strings.Contains(thunder, "idp.org") {
		t.Errorf("a Thunder install sets an org: %s", thunder)
	}
}

// The install's own IdP pairs: the issuer the console and aep-api use, and
// Thunder's in-cluster admin address only where there is a Thunder.
func TestPlatformIDPInstallArgs(t *testing.T) {
	t.Cleanup(viper.Reset)
	viper.Set("thunder.url", "http://thunder-service.thunder.svc.cluster.local:8090")
	thunder := strings.Join(platformIDPInstallArgs(testPlatformIDP), " ")
	for _, want := range []string{
		"--set console.thunderPublicURL=http://thunder.openchoreo.localhost:8080",
		"--set thunder.adminURL=http://thunder-service.thunder.svc.cluster.local:8090",
	} {
		if !strings.Contains(thunder, want) {
			t.Errorf("thunder: missing %s in %s", want, thunder)
		}
	}
	asgardeo := strings.Join(platformIDPInstallArgs(testAsgardeoIDP), " ")
	if !strings.Contains(asgardeo, "--set console.thunderPublicURL=https://api.asgardeo.io/t/acmecorp/oauth2/token") {
		t.Errorf("asgardeo: no issuer in %s", asgardeo)
	}
	if strings.Contains(asgardeo, "thunder.adminURL") {
		t.Errorf("asgardeo: sets a Thunder admin URL: %s", asgardeo)
	}
}

// An environment ThunderID is told to trust the platform IdP so that Agent
// Manager's token, minted there, is accepted. Agent Manager installs on a
// Thunder platform only, so on Asgardeo no caller presents such a token and
// no trust is written.
func TestEnvIDPPlatformTrust(t *testing.T) {
	if got := envIDPPlatformTrust(testPlatformIDP); got != testPlatformIDP.Public {
		t.Errorf("thunder: %+v, want the public endpoints", got)
	}
	if got := envIDPPlatformTrust(testAsgardeoIDP); got.Configured() {
		t.Errorf("asgardeo: %+v, want no trust", got)
	}
}

// The pod's NetworkPolicy already lets it reach public HTTPS endpoints, so
// an Asgardeo install opens no IdP namespace: only aep-api.
func TestAEStudioExtraEgress_AsgardeoOpensNoIDPNamespace(t *testing.T) {
	setAsgardeoConfig(t)
	// Left over from a Thunder config: not read on Asgardeo.
	viper.Set("thunder.namespace", "thunder")
	got := aeStudioExtraEgress("wso2-aep")
	if strings.Contains(got, `"thunder"`) || strings.Contains(got, "8090") {
		t.Errorf("an IdP namespace rule on Asgardeo: %s", got)
	}
	if !strings.Contains(got, `"app":"aep-api"`) || !strings.Contains(got, `"port":9090`) {
		t.Errorf("no aep-api rule: %s", got)
	}
}

// What the vault holds by kind. On Asgardeo aectl generates no client secret
// and writes no Thunder admin input; the four client secrets are the admin's
// to import, and are restored from the cluster like any imported key.
func TestPlatformSecrets_ByIDPKind(t *testing.T) {
	paths := func(secrets []generatedSecret) string {
		var out []string
		for _, s := range secrets {
			out = append(out, s.path)
		}
		return strings.Join(out, " ")
	}
	imported := []string{
		"aep/thunder-clients/aep-api-client", "aep/thunder-clients/ae-studio-internal",
		"aep/thunder-clients/org-publisher", "aep/thunder-clients/org-studio",
	}

	t.Run("thunder", func(t *testing.T) {
		t.Cleanup(viper.Reset)
		if got := paths(generatedPlatformSecrets()); !strings.Contains(got, "aep/thunder-clients/system-client") {
			t.Errorf("no generated Thunder client secrets: %s", got)
		}
		if got := strings.Join(platformInputPaths(), " "); !strings.Contains(got, "aep/thunder-admin/client-secret") {
			t.Errorf("no Thunder admin input: %s", got)
		}
		if got := strings.Join(requiredOpenBaoPaths(), " "); !strings.Contains(got, "aep/thunder-admin/client-id") {
			t.Errorf("the Thunder admin credentials are not required: %s", got)
		}
		if got := paths(importedPlatformSecrets()); strings.Contains(got, "org-publisher") {
			t.Errorf("a Thunder install imports an org client secret: %s", got)
		}
		for _, s := range importedClientSecrets() {
			t.Errorf("a Thunder install has imported client secret %s", s.path)
		}
	})

	t.Run("asgardeo", func(t *testing.T) {
		setAsgardeoConfig(t)
		generated := paths(generatedPlatformSecrets())
		if strings.Contains(generated, "thunder-clients") {
			t.Errorf("aectl generates a client secret on Asgardeo: %s", generated)
		}
		if !strings.Contains(generated, "aep/postgres-password") {
			t.Errorf("the IdP-neutral secrets are no longer generated: %s", generated)
		}
		if got := strings.Join(platformInputPaths(), " "); strings.Contains(got, "thunder") {
			t.Errorf("a Thunder input on Asgardeo: %s", got)
		}
		if got := strings.Join(requiredOpenBaoPaths(), " "); strings.Contains(got, "thunder-admin") || !strings.Contains(got, "aep/postgres-password") {
			t.Errorf("required paths: %s", got)
		}
		got := paths(importedClientSecrets())
		all := strings.Join(platformSecretPaths(), " ")
		for _, want := range imported {
			if !strings.Contains(got, want) {
				t.Errorf("imported client secrets %s lack %s", got, want)
			}
			if !strings.Contains(all, want) {
				t.Errorf("platformSecretPaths lacks %s, so its Secret is not synced before the pods are waited on", want)
			}
		}
		for _, s := range importedClientSecrets() {
			if s.generate != nil {
				t.Errorf("%s has a generator: an imported secret is never generated", s.path)
			}
		}
		if !strings.Contains(paths(importedPlatformSecrets()), "aep/thunder-clients/org-studio") {
			t.Error("the imported client secrets are not restored with the other imported keys")
		}
	})
}

// memSeedStore is a vault and, optionally, the cluster's last-synced copies.
type memSeedStore struct {
	vault   map[string]string
	cluster map[string]string
}

func (m *memSeedStore) store() seedStore {
	return seedStore{
		exists: func(path string) (bool, error) { _, ok := m.vault[path]; return ok, nil },
		put:    func(path, value string) error { m.vault[path] = value; return nil },
		recover: func(path string) (string, string, error) {
			if v, ok := m.cluster[path]; ok {
				return v, "wso2-aep/some-secret", nil
			}
			return "", "", nil
		},
	}
}

// Before Helm runs on Asgardeo, each imported client secret must be in the
// vault. One the vault lost but the cluster still holds is restored; one
// nothing holds stops the install with the command that imports it. No value
// is ever printed.
func TestRequireImportedClientSecrets(t *testing.T) {
	setAsgardeoConfig(t)
	all := map[string]string{
		"aep/thunder-clients/aep-api-client":     "s1",
		"aep/thunder-clients/ae-studio-internal": "s2",
		"aep/thunder-clients/org-publisher":      "s3",
		"aep/thunder-clients/org-studio":         "s4",
	}
	copyOf := func(m map[string]string, drop ...string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[k] = v
		}
		for _, d := range drop {
			delete(out, d)
		}
		return out
	}

	m := &memSeedStore{vault: copyOf(all)}
	if restored, err := requireImportedClientSecrets(m.store()); err != nil || len(restored) != 0 {
		t.Fatalf("all imported: restored %v, err %v", restored, err)
	}

	// The vault lost one; the cluster still holds it.
	m = &memSeedStore{vault: copyOf(all, "aep/thunder-clients/org-studio"), cluster: map[string]string{"aep/thunder-clients/org-studio": "s4"}}
	restored, err := requireImportedClientSecrets(m.store())
	if err != nil || len(restored) != 1 || m.vault["aep/thunder-clients/org-studio"] != "s4" {
		t.Fatalf("restore from the cluster: restored %v, err %v, vault %v", restored, err, m.vault["aep/thunder-clients/org-studio"])
	}

	// Two were never imported.
	m = &memSeedStore{vault: copyOf(all, "aep/thunder-clients/org-publisher", "aep/thunder-clients/aep-api-client")}
	_, err = requireImportedClientSecrets(m.store())
	if err == nil {
		t.Fatal("two missing imports were accepted")
	}
	msg := err.Error()
	for _, want := range []string{
		"aectl platform secret import --path aep/thunder-clients/org-publisher",
		"aectl platform secret import --path aep/thunder-clients/aep-api-client",
		"ae-asgardeo-apps.sh",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "org-studio") || strings.Contains(msg, "s1") {
		t.Errorf("error names a present secret or a value: %s", msg)
	}
	if _, generated := m.vault["aep/thunder-clients/org-publisher"]; generated {
		t.Error("a missing imported secret was generated")
	}
}

// On a Thunder install there is nothing to import, so nothing is required.
func TestRequireImportedClientSecrets_ThunderRequiresNone(t *testing.T) {
	t.Cleanup(viper.Reset)
	m := &memSeedStore{vault: map[string]string{}}
	if restored, err := requireImportedClientSecrets(m.store()); err != nil || len(restored) != 0 {
		t.Fatalf("restored %v, err %v", restored, err)
	}
}

// sync-clients on Asgardeo restores and checks the imported secrets and
// syncs the cluster's Secrets; it registers nothing, since aectl administers
// nothing on the IdP.
func TestSyncClients_AsgardeoRegistersNothing(t *testing.T) {
	r := &syncRecorder{}
	steps := r.steps(nil, nil, nil)
	steps.setupThunder = nil
	if err := syncClients(context.Background(), steps); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.order, ","); got != "seed,sync,postgres" {
		t.Fatalf("order = %s", got)
	}
}

// `sre install` registers OpenChoreo's observer and RCA clients on the
// platform IdP, which on Asgardeo are the cluster owner's: it refuses there.
func TestSreInstall_RefusedOnAsgardeo(t *testing.T) {
	setAsgardeoConfig(t)
	err := sreIDPSupported()
	if err == nil || !strings.Contains(err.Error(), "asgardeo") {
		t.Fatalf("err = %v, want a refusal naming the kind", err)
	}
	viper.Set("idp.kind", "thunder")
	if err := sreIDPSupported(); err != nil {
		t.Fatalf("thunder: %v", err)
	}
}
