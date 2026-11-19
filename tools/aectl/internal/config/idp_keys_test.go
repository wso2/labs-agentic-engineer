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
	"idp.org_handle":          "default",
}

var asgardeoKeys = map[string]string{
	"idp.clients":    "imported",
	"idp.kind":       "asgardeo",
	"idp.issuer":     "https://api.asgardeo.io/t/acmecorp/oauth2/token",
	"idp.org_handle": "acmecorp",
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

// A Thunder install needs the four thunder.* keys and the handle of the org
// it serves, whether idp.kind is unset or says thunder.
func TestValidate_ThunderKeysRequiredUnderThunder(t *testing.T) {
	for _, kind := range []string{"", "thunder"} {
		if errs := validateViper(idpConfig(with(thunderKeys, "idp.kind", kind))); len(errs) != 0 {
			t.Fatalf("kind %q: a complete Thunder config is invalid: %v", kind, errs)
		}
		for key := range thunderKeys {
			errs := validateViper(idpConfig(with(without(thunderKeys, key), "idp.kind", kind)))
			if !hasErrorNaming(errs, key) {
				t.Errorf("kind %q without %s: errors %v do not name it", kind, key, errs)
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
	for _, key := range []string{"idp.issuer", "idp.org_handle"} {
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
		if key == "idp.org_handle" {
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
		if errs := validateViper(idpConfig(with(thunderKeys, "idp.issuer", issuer))); len(errs) != 0 {
			t.Errorf("issuer %q under thunder: %v", issuer, errs)
		}
	}
}

func TestValidate_UnknownClientsRefused(t *testing.T) {
	errs := validateViper(idpConfig(with(thunderKeys, "idp.clients", "managed")))
	if !hasErrorNaming(errs, "idp.clients") {
		t.Fatalf("errors %v do not refuse the value", errs)
	}
}

// With imported clients the issuer is a public endpoint tokens are verified
// against: plain http is refused, as is anything that is not an absolute URL.
func TestValidate_ImportedClientsIssuerMustBeHTTPS(t *testing.T) {
	for _, issuer := range []string{"http://api.asgardeo.io/t/acmecorp/oauth2/token", "api.asgardeo.io/t/acmecorp", "not a url"} {
		errs := validateViper(idpConfig(with(asgardeoKeys, "idp.issuer", issuer)))
		if !hasErrorNaming(errs, "idp.issuer") {
			t.Errorf("issuer %q: errors %v do not refuse it", issuer, errs)
		}
	}
}

func TestValidate_UnknownKindRefused(t *testing.T) {
	errs := validateViper(idpConfig(with(thunderKeys, "idp.kind", "okta")))
	if !hasErrorNaming(errs, "idp.kind") {
		t.Fatalf("errors %v do not refuse the kind", errs)
	}
}

// Every key validation knows is persisted, and the IdP's are among them.
func TestIDPKeys_ArePersisted(t *testing.T) {
	persisted := map[string]bool{}
	for _, k := range ConfigMapKeys {
		persisted[k] = true
	}
	for _, k := range []string{"idp.clients", "idp.kind", "idp.issuer", "idp.org_handle"} {
		if !persisted[k] {
			t.Errorf("%s is not in ConfigMapKeys", k)
		}
		if _, ok := keyRegistry[k]; !ok {
			t.Errorf("%s is not in keyRegistry", k)
		}
	}
}
