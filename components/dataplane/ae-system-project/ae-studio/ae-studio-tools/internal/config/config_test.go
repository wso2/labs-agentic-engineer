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
	"errors"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{
		"AE_ORG_ID": "ou-1", "AE_ORG_HANDLE": "default",
		"AE_IDP_ISSUER":     "http://thunder.openchoreo.localhost:8080",
		"AE_IDP_JWKS_URL":   "http://thunder:8090/oauth2/jwks",
		"AE_USER_AUDIENCES": "aep-console-client, other",
		"AE_M2M_CLIENT_ID":  "ae-studio-internal-client",
		"GITHUB_PAT":        "x", "GITHUB_WEBHOOK_SECRET": "y",
		"AE_IDP_TOKEN_URL":       "http://thunder:8090/oauth2/token",
		"AE_PUBLISHER_CLIENT_ID": "aep-publisher-default", "AE_PUBLISHER_CLIENT_SECRET": "publisher-secret-value",
		"AEP_API_BASE_URL": "http://aep-api.aep.svc.cluster.local:9090",
	}
}

func TestLoad_Defaults(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenPort != 8082 || c.HealthPort != 9082 {
		t.Fatalf("ports = %d/%d", c.ListenPort, c.HealthPort)
	}
	if len(c.UserAudiences) != 2 || c.UserAudiences[1] != "other" {
		t.Fatalf("audiences = %v", c.UserAudiences)
	}
}

func TestLoad_PortOverrides(t *testing.T) {
	m := base()
	m["AE_LISTEN_PORT"], m["AE_HEALTH_PORT"] = "18082", "19082"
	c, err := Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenPort != 18082 || c.HealthPort != 19082 {
		t.Fatalf("ports = %d/%d", c.ListenPort, c.HealthPort)
	}
}

func TestLoad_InvalidPortIsAnError(t *testing.T) {
	for _, v := range []string{"abc", "0", "70000", "-1"} {
		m := base()
		m["AE_HEALTH_PORT"] = v
		if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "AE_HEALTH_PORT") {
			t.Fatalf("AE_HEALTH_PORT=%q: err = %v", v, err)
		}
	}
}

func TestLoad_AudienceListWithOnlySeparatorsIsMissing(t *testing.T) {
	m := base()
	m["AE_USER_AUDIENCES"] = " , ,"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "AE_USER_AUDIENCES") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoad_MissingRequiredNamesEveryKey(t *testing.T) {
	_, err := Load(env(map[string]string{}))
	for _, k := range []string{"AE_ORG_ID", "AE_ORG_HANDLE", "AE_IDP_ISSUER", "AE_IDP_JWKS_URL", "AE_USER_AUDIENCES", "AE_M2M_CLIENT_ID", "GITHUB_PAT", "GITHUB_WEBHOOK_SECRET",
		"AE_IDP_TOKEN_URL", "AE_PUBLISHER_CLIENT_ID", "AE_PUBLISHER_CLIENT_SECRET", "AEP_API_BASE_URL"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Fatalf("error %v does not name %s", err, k)
		}
	}
}

func TestLoad_PublisherAndAEPAPIKeys(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.IDPTokenURL != "http://thunder:8090/oauth2/token" || c.PublisherClientID != "aep-publisher-default" ||
		c.PublisherClientSecret != "publisher-secret-value" || c.AEPAPIBaseURL != "http://aep-api.aep.svc.cluster.local:9090" {
		t.Fatalf("publisher/aep-api keys not read")
	}
}

// Each key is required on its own (fail closed at boot), and the error names
// the key, never the value of any other key.
func TestLoad_EachPublisherKeyIsRequired(t *testing.T) {
	for _, k := range []string{"AE_IDP_TOKEN_URL", "AE_PUBLISHER_CLIENT_ID", "AE_PUBLISHER_CLIENT_SECRET", "AEP_API_BASE_URL"} {
		m := base()
		m[k] = "  "
		_, err := Load(env(m))
		if err == nil || !strings.Contains(err.Error(), "missing "+k) {
			t.Fatalf("%s blank: err = %v", k, err)
		}
		if strings.Contains(err.Error(), "publisher-secret-value") {
			t.Fatalf("%s blank: error leaks the publisher secret", k)
		}
	}
}

func TestLoad_URLKeysMustBeAbsoluteHTTP(t *testing.T) {
	for _, k := range []string{"AE_IDP_TOKEN_URL", "AEP_API_BASE_URL"} {
		for _, v := range []string{"aep-api:9090", "/internal", "ftp://x", "http://"} {
			m := base()
			m[k] = v
			if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "invalid "+k) {
				t.Fatalf("%s=%q: err = %v", k, v, err)
			}
		}
	}
}

func TestCheckSecretRev(t *testing.T) {
	cases := []struct {
		got, want string
		ok        bool
	}{
		{"r1", "r1", true}, {"r0", "r1", false}, {"", "r1", false}, {"", "", true},
	}
	for _, c := range cases {
		err := CheckSecretRev(env(map[string]string{"AE_SECRET_REV": c.got, "AE_EXPECTED_SECRET_REV": c.want}))
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrSecretRevMismatch)) {
			t.Fatalf("%+v: err=%v", c, err)
		}
	}
}
