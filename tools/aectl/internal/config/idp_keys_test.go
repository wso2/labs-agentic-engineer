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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// idpConfig is a viper holding the keys every install needs, plus extra.
func idpConfig(extra map[string]string) *viper.Viper {
	v := viper.New()
	v.Set("oc.api_url", "http://openchoreo-api.openchoreo-control-plane.svc.cluster.local:8080")
	v.Set("oc.system_namespace", "openchoreo-control-plane")
	for k, val := range extra {
		v.Set(k, val)
	}
	return v
}

var thunderKeys = map[string]string{
	"thunder.namespace":       "thunder",
	"thunder.url":             "http://thunder-service.thunder.svc.cluster.local:8090",
	"thunder.admin_client_id": "ae-install-client",
	"thunder.public_url":      "http://thunder.openchoreo.localhost:8080",
	"platform.idp.org_handle": "default",
}

var asgardeoKeys = map[string]string{
	"platform.idp.clients":       "imported",
	"platform.idp.claims.preset": "asgardeo",
	"platform.idp.issuer":        "https://api.asgardeo.io/t/acmecorp/oauth2/token",
	"platform.idp.org_handle":    "acmecorp",
}

func without(m map[string]string, key string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

func with(m map[string]string, key, value string) map[string]string {
	out := without(m, key)
	out[key] = value
	return out
}

func hasErrorNaming(errs []string, key string) bool {
	for _, e := range errs {
		if strings.HasPrefix(e, key+":") {
			return true
		}
	}
	return false
}

// An install where aectl registers the clients needs the four thunder.* keys
// and the handle of the org it serves, whether platform.idp.clients is unset or says
// thunder.
func TestValidate_ThunderKeysRequiredUnderThunder(t *testing.T) {
	for _, clients := range []string{"", "thunder"} {
		if errs := validateViper(idpConfig(with(thunderKeys, "platform.idp.clients", clients))); len(errs) != 0 {
			t.Fatalf("clients %q: a complete Thunder config is invalid: %v", clients, errs)
		}
		for key := range thunderKeys {
			errs := validateViper(idpConfig(with(without(thunderKeys, key), "platform.idp.clients", clients)))
			if !hasErrorNaming(errs, key) {
				t.Errorf("clients %q without %s: errors %v do not name it", clients, key, errs)
			}
		}
	}
}

// An install with imported clients (an Asgardeo organization here) has no
// in-cluster IdP to name: it needs the issuer and the org's handle, and none
// of the thunder.* keys.
func TestValidate_ImportedClientsNeedTheIssuerAndNoThunderKeys(t *testing.T) {
	if errs := validateViper(idpConfig(asgardeoKeys)); len(errs) != 0 {
		t.Fatalf("a complete Asgardeo config is invalid: %v", errs)
	}
	for _, key := range []string{"platform.idp.issuer", "platform.idp.org_handle"} {
		errs := validateViper(idpConfig(without(asgardeoKeys, key)))
		if !hasErrorNaming(errs, key) {
			t.Errorf("without %s: errors %v do not name it", key, errs)
		}
	}
}

// The thunder.* keys describe a ThunderID aectl administers. With imported
// clients nothing reads them: a value there would look as if it took effect,
// so each is refused.
func TestValidate_ThunderKeysRefusedWithImportedClients(t *testing.T) {
	for key, val := range thunderKeys {
		if key == "platform.idp.org_handle" {
			continue
		}
		errs := validateViper(idpConfig(with(asgardeoKeys, key, val)))
		if !hasErrorNaming(errs, key) {
			t.Errorf("%s with imported clients: errors %v do not refuse it", key, errs)
		}
	}
}

// Where aectl registers the clients the issuer is optional (it is
// thunder.public_url when unset) and may be the local ThunderID's http URL.
func TestValidate_IssuerIsOptionalWhereAectlRegistersClients(t *testing.T) {
	for _, issuer := range []string{"http://thunder.openchoreo.localhost:8080", "https://idp.example.com"} {
		if errs := validateViper(idpConfig(with(thunderKeys, "platform.idp.issuer", issuer))); len(errs) != 0 {
			t.Errorf("issuer %q under thunder: %v", issuer, errs)
		}
	}
}

func TestValidate_UnknownClientsRefused(t *testing.T) {
	errs := validateViper(idpConfig(with(thunderKeys, "platform.idp.clients", "managed")))
	if !hasErrorNaming(errs, "platform.idp.clients") {
		t.Fatalf("errors %v do not refuse the value", errs)
	}
}

// With imported clients the issuer is a public endpoint tokens are verified
// against: plain http is refused, as is anything that is not an absolute URL.
func TestValidate_ImportedClientsIssuerMustBeHTTPS(t *testing.T) {
	for _, issuer := range []string{"http://api.asgardeo.io/t/acmecorp/oauth2/token", "api.asgardeo.io/t/acmecorp", "not a url"} {
		errs := validateViper(idpConfig(with(asgardeoKeys, "platform.idp.issuer", issuer)))
		if !hasErrorNaming(errs, "platform.idp.issuer") {
			t.Errorf("issuer %q: errors %v do not refuse it", issuer, errs)
		}
	}
}

// asgardeoClientsKeys is a config where aectl creates the clients in an
// Asgardeo organization (platform.idp.clients asgardeo). asgardeoKeys above is the
// same organization with imported clients.
var asgardeoClientsKeys = with(with(asgardeoKeys, "platform.idp.clients", "asgardeo"),
	"asgardeo.api_url", "https://api.asgardeo.io/t/acmecorp/api/server/v1")

// Where aectl creates the clients in an Asgardeo organization it needs the
// issuer, the org's handle and the address of the management API, and none
// of the thunder.* keys.
func TestValidate_AsgardeoClientsNeedTheIssuerAndTheAPIAddress(t *testing.T) {
	if errs := validateViper(idpConfig(asgardeoClientsKeys)); len(errs) != 0 {
		t.Fatalf("a complete config with platform.idp.clients asgardeo is invalid: %v", errs)
	}
	for _, key := range []string{"platform.idp.issuer", "platform.idp.org_handle", "asgardeo.api_url"} {
		errs := validateViper(idpConfig(without(asgardeoClientsKeys, key)))
		if !hasErrorNaming(errs, key) {
			t.Errorf("without %s: errors %v do not name it", key, errs)
		}
	}
	for key, val := range thunderKeys {
		if key == "platform.idp.org_handle" {
			continue
		}
		errs := validateViper(idpConfig(with(asgardeoClientsKeys, key, val)))
		if !hasErrorNaming(errs, key) {
			t.Errorf("%s with platform.idp.clients asgardeo: errors %v do not refuse it", key, errs)
		}
	}
}

// The management API is sent the token of the admin credential: plain http is
// refused, as is anything that is not an absolute URL. So is a plain http
// issuer.
func TestValidate_AsgardeoClientsAddressesMustBeHTTPS(t *testing.T) {
	for _, key := range []string{"asgardeo.api_url", "platform.idp.issuer"} {
		for _, val := range []string{"http://api.asgardeo.io/t/acmecorp/api/server/v1", "api.asgardeo.io/t/acmecorp", "not a url"} {
			errs := validateViper(idpConfig(with(asgardeoClientsKeys, key, val)))
			if !hasErrorNaming(errs, key) {
				t.Errorf("%s %q: errors %v do not refuse it", key, val, errs)
			}
		}
	}
}

// asgardeo.api_url is read only where aectl creates the clients in Asgardeo.
// Under another value it would look as if it took effect, so it is refused.
func TestValidate_AsgardeoAPIAddressRefusedUnderAnotherValue(t *testing.T) {
	const api = "https://api.asgardeo.io/t/acmecorp/api/server/v1"
	for name, cfg := range map[string]map[string]string{
		"imported": with(asgardeoKeys, "asgardeo.api_url", api),
		"thunder":  with(thunderKeys, "asgardeo.api_url", api),
	} {
		errs := validateViper(idpConfig(cfg))
		if !hasErrorNaming(errs, "asgardeo.api_url") {
			t.Errorf("%s: errors %v do not refuse asgardeo.api_url", name, errs)
		}
	}
}

// A preset aectl does not ship is refused by name, once.
func TestValidate_UnknownPresetRefused(t *testing.T) {
	errs := validateViper(idpConfig(with(thunderKeys, "platform.idp.claims.preset", "okta")))
	if !hasErrorNaming(errs, "platform.idp.claims.preset") || len(errs) != 1 {
		t.Fatalf("errors %v, want one refusing the preset", errs)
	}
}

// The platform.idp.claims block has to be complete after the preset and the config's
// own keys are layered. A preset alone is complete; a config key replaces a
// preset value; no key can blank a required one, since an empty value sets
// nothing.
func TestValidate_ClaimsAreLayeredOverThePreset(t *testing.T) {
	for name, extra := range map[string]map[string]string{
		"no claims keys: the default preset": {},
		"the asgardeo preset":                {"platform.idp.claims.preset": "asgardeo"},
		"a key over the preset":              {"platform.idp.claims.org.handle": "tenant", "platform.idp.claims.caller_type.user": "authorization_code"},
		"an empty key sets nothing":          {"platform.idp.claims.org.id": ""},
	} {
		cfg := map[string]string{}
		for k, v := range thunderKeys {
			cfg[k] = v
		}
		for k, v := range extra {
			cfg[k] = v
		}
		if errs := validateViper(idpConfig(cfg)); len(errs) != 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
}

// With no preset, every required value has to be in the config: one left
// out is an error that names its key.
func TestValidate_NoPresetRequiresEveryClaim(t *testing.T) {
	cfg := with(with(with(thunderKeys, "platform.idp.claims.preset", "none"), "platform.idp.claims.org.handle", "tenant"), "platform.idp.claims.org.id", "tenant_id")
	errs := validateViper(idpConfig(cfg))
	if len(errs) != 1 || !strings.Contains(errs[0], "platform.idp.claims.caller_type.claim") || !strings.Contains(errs[0], "platform.idp.claims.caller_type.machine") {
		t.Fatalf("errors %v, want one naming the two missing keys", errs)
	}
}

// A config that names no preset starts from the default one, ThunderID's.
// An Asgardeo config names its preset.
func TestClaims_AnUnsetPresetIsThunder(t *testing.T) {
	got, err := ResolveClaims(idpConfig(thunderKeys))
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != "thunder" || got.Claims().OrgHandle != "ouHandle" {
		t.Fatalf("no preset: preset %q, org handle claim %q; want thunder, ouHandle", got.Preset, got.Claims().OrgHandle)
	}
	v := idpConfig(asgardeoKeys)
	if errs := validateViper(v); len(errs) != 0 {
		t.Fatalf("an Asgardeo config that names its preset is invalid: %v", errs)
	}
	got, err = ResolveClaims(v)
	if err != nil || got.Claims().OrgHandle != "org_handle" {
		t.Fatalf("the asgardeo preset: org handle claim %q, err %v; want org_handle", got.Claims().OrgHandle, err)
	}
}

// Every key validation knows is persisted, and the IdP's are among them.
func TestIDPKeys_ArePersisted(t *testing.T) {
	persisted := map[string]bool{}
	for _, k := range ConfigMapKeys {
		persisted[k] = true
	}
	for _, k := range []string{"platform.idp.clients", "platform.idp.claims.preset", "platform.idp.claims.org.handle", "platform.idp.claims.caller_type.user", "platform.idp.issuer", "platform.idp.org_handle", "asgardeo.api_url"} {
		if !persisted[k] {
			t.Errorf("%s is not in ConfigMapKeys", k)
		}
		if _, ok := keyRegistry[k]; !ok {
			t.Errorf("%s is not in keyRegistry", k)
		}
	}
	// idp.kind is no longer a key: it is neither validated nor stored, and a
	// config that still has the line is valid, as with any unknown key.
	if _, ok := keyRegistry["idp.kind"]; ok || persisted["idp.kind"] {
		t.Error("idp.kind is still a key")
	}
	if errs := validateViper(idpConfig(with(thunderKeys, "idp.kind", "thunder"))); len(errs) != 0 {
		t.Errorf("a config with the old idp.kind line: %v", errs)
	}
}

// A claim value is not taken from the environment. Every command that
// validates a config stops on an AEP_PLATFORM_IDP_CLAIMS_* variable first, the
// loaded config and a config file alike, and names the variable.
func TestValidate_ClaimsFromTheEnvironmentAreRefused(t *testing.T) {
	if err := ClaimsEnvRefusal(); err != nil {
		t.Fatalf("no variable is set: %v", err)
	}
	got := claimsEnvSet([]string{"AEP_PLATFORM_IDP_CLAIMS_ORG_HANDLE=x", "AEP_PLATFORM_IDP_ORG_HANDLE=default", "AEP_PLATFORM_IDP_CLAIMS_PRESET=", "PATH=/bin"})
	if strings.Join(got, ",") != "AEP_PLATFORM_IDP_CLAIMS_ORG_HANDLE,AEP_PLATFORM_IDP_CLAIMS_PRESET" {
		t.Fatalf("claimsEnvSet = %v, want the two claim variables, the blank one included", got)
	}

	t.Setenv("AEP_PLATFORM_IDP_CLAIMS_PRESET", "okta")
	err := ClaimsEnvRefusal()
	if err == nil || !strings.Contains(err.Error(), "AEP_PLATFORM_IDP_CLAIMS_PRESET") || !strings.Contains(err.Error(), "platform.idp.claims") {
		t.Fatalf("err = %v, want one naming the variable and platform.idp.claims", err)
	}
	// The refusal comes first: the variable can cause the other errors.
	errs := withClaimsEnvRefusal([]string{"platform.idp.claims.preset: unknown"})
	if len(errs) != 2 || !strings.HasPrefix(errs[0], "AEP_PLATFORM_IDP_CLAIMS_PRESET:") {
		t.Fatalf("errors %v, want the refusal first", errs)
	}
	// A config file is read alone, and is refused too.
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("platform:\n  idp:\n    org_handle: default\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := ValidateFile(path); len(errs) == 0 || !strings.HasPrefix(errs[0], "AEP_PLATFORM_IDP_CLAIMS_PRESET:") {
		t.Fatalf("ValidateFile errors %v, want the refusal first", errs)
	}
}
