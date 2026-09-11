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

package thunder

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// The identity-claim contract is written down in two places for two different
// audiences: a bootstrap YAML document, imported at install time to register
// the platform console, and this package, which registers an app per generated
// application. They must agree, and until 2026-09-06 they did not.
//
// The console document had the shape ThunderID 1.0.0 actually stores
// (accessToken.userConfig.attributes); this package had the shape it silently
// drops (accessToken.userAttributes). Both were "tested": the console by a
// working console, this package by a fake server that echoed back whatever it
// was sent. Nothing compared the two, so a shape someone had already learned
// the hard way never reached the code that provisions apps for USERS' apps —
// and every generated SPA shipped with an access token carrying no `groups`,
// which the API reads (through the gateway's X-User-Groups mapping) to
// authorize. Login worked; every authorated call answered 403.
//
// This test is the comparison. It reads the console's document off disk and
// asserts that what this package sends matches it, so the next person to learn
// something about ThunderID's wire shape only has to write it down once.
const consoleAppDocument = "../../../../../thunder-resources/87-aep-console-app.yaml"

type consoleAppDoc struct {
	InboundAuthConfig []struct {
		Config struct {
			Token struct {
				AccessToken struct {
					UserConfig struct {
						ValidityPeriod int      `yaml:"validityPeriod"`
						Attributes     []string `yaml:"attributes"`
					} `yaml:"userConfig"`
				} `yaml:"accessToken"`
				IDToken struct {
					ValidityPeriod int      `yaml:"validityPeriod"`
					UserAttributes []string `yaml:"userAttributes"`
				} `yaml:"idToken"`
			} `yaml:"token"`
			ScopeClaims map[string][]string `yaml:"scopeClaims"`
		} `yaml:"config"`
	} `yaml:"inboundAuthConfig"`
}

func loadConsoleAppDoc(t *testing.T) consoleAppDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(consoleAppDocument))
	if err != nil {
		t.Fatalf("read %s: %v — the platform console's registration is the "+
			"authoritative copy of this contract; if it moved, move this test with it",
			consoleAppDocument, err)
	}
	var doc consoleAppDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", consoleAppDocument, err)
	}
	if len(doc.InboundAuthConfig) == 0 {
		t.Fatalf("%s has no inboundAuthConfig", consoleAppDocument)
	}
	return doc
}

// The two writers must put the attributes at the same PATHS. A mismatch here is
// the exact defect this test exists for: one of them is being silently dropped.
func TestTokenClaimConfig_UsesTheSamePathsAsTheConsoleDocument(t *testing.T) {
	doc := loadConsoleAppDoc(t)
	tokenCfg := doc.InboundAuthConfig[0].Config.Token

	if len(tokenCfg.AccessToken.UserConfig.Attributes) == 0 {
		t.Fatalf("%s declares no accessToken.userConfig.attributes — either the "+
			"console lost its claims or ThunderID's contract moved; resolve that "+
			"before changing this package", consoleAppDocument)
	}
	if len(tokenCfg.IDToken.UserAttributes) == 0 {
		t.Fatalf("%s declares no idToken.userAttributes", consoleAppDocument)
	}

	// Build what this package sends, then read it back with the same accessors
	// the read-back verification uses, so the test exercises the real paths
	// rather than re-describing them.
	cfg := map[string]any{"token": jsonRoundTrip(t, tokenClaimConfig())}

	if got := accessTokenAttributes(cfg); len(got) == 0 {
		t.Error("tokenClaimConfig writes no access-token attributes at " +
			"token.accessToken.userConfig.attributes — ThunderID stores nothing else, " +
			"so every provisioned app would 403 on authorated calls")
	}
	if got := idTokenAttributes(cfg); len(got) == 0 {
		t.Error("tokenClaimConfig writes no id-token attributes at token.idToken.userAttributes")
	}
}

// Every attribute the console releases must also be released by the apps this
// operator provisions. The console may legitimately carry more (it is the
// platform's own client); it must never carry something a generated app needs
// and this package forgets.
func TestIdentityUserAttributes_CoverTheConsoleDocument(t *testing.T) {
	doc := loadConsoleAppDoc(t)
	ours := make(map[string]struct{}, len(identityUserAttributes))
	for _, a := range identityUserAttributes {
		ours[a] = struct{}{}
	}
	for _, want := range doc.InboundAuthConfig[0].Config.Token.AccessToken.UserConfig.Attributes {
		if _, ok := ours[want]; !ok {
			t.Errorf("the console releases %q but identityUserAttributes does not — "+
				"a generated app's token would be missing a claim the platform's own is not",
				want)
		}
	}
}

// `group -> groups` is the mapping that puts the claim in the token at all; the
// attribute lists above are inert without it.
func TestScopeClaimConfig_MatchesTheConsoleDocument(t *testing.T) {
	doc := loadConsoleAppDoc(t)
	consoleClaims := doc.InboundAuthConfig[0].Config.ScopeClaims
	if len(consoleClaims) == 0 {
		t.Skipf("%s declares no scopeClaims to compare against", consoleAppDocument)
	}
	ours := scopeClaimConfig()
	for scope, claims := range consoleClaims {
		got, present := ours[scope]
		if !present {
			t.Errorf("the console maps scope %q to %v; scopeClaimConfig has no entry for it", scope, claims)
			continue
		}
		gotList, _ := got.([]string)
		for _, claim := range claims {
			if !containsString(gotList, claim) {
				t.Errorf("scope %q: the console releases claim %q, scopeClaimConfig releases %v", scope, claim, gotList)
			}
		}
	}
}

// jsonRoundTrip converts a payload the way encoding/json would on the wire, so
// the accessors under test see []any rather than []string — exactly what they
// meet when reading a response back.
func jsonRoundTrip(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	raw, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return normalizeToAny(out)
}

// normalizeToAny rewrites every []string into []any, mirroring encoding/json's
// decode shape.
func normalizeToAny(v map[string]any) map[string]any {
	for k, val := range v {
		switch typed := val.(type) {
		case map[string]any:
			v[k] = normalizeToAny(typed)
		case []string:
			list := make([]any, 0, len(typed))
			for _, s := range typed {
				list = append(list, s)
			}
			v[k] = list
		}
	}
	return v
}
