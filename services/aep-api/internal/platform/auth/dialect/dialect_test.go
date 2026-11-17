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

package dialect

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureDir holds the sample tokens every reader of the platform IdP's
// tokens is tested against: this package, the AE Studio pod's Go twin, the
// TypeScript package that owns the files, and the console.
const fixtureDir = "../../../../../../packages/platform-idp-auth/test/fixtures/idp-claims"

type fixture struct {
	IDP    string `json:"idp"`
	Claims struct {
		Org struct {
			Handle string `json:"handle"`
			ID     string `json:"id"`
			Name   string `json:"name"`
		} `json:"org"`
		CallerType struct {
			Claim   string `json:"claim"`
			Machine string `json:"machine"`
			User    string `json:"user"`
		} `json:"callerType"`
	} `json:"claims"`
	Tokens []struct {
		Name    string `json:"name"`
		Payload Raw    `json:"payload"`
		Org     struct {
			Handle string `json:"handle"`
			ID     string `json:"id"`
			Name   string `json:"name"`
		} `json:"org"`
		Caller string `json:"caller"`
	} `json:"tokens"`
}

func (f fixture) claims() Claims {
	return Claims{
		OrgHandle: f.Claims.Org.Handle, OrgID: f.Claims.Org.ID, OrgName: f.Claims.Org.Name,
		CallerType: f.Claims.CallerType.Claim, Machine: f.Claims.CallerType.Machine, User: f.Claims.CallerType.User,
	}
}

func loadFixture(t *testing.T, idp string) fixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, idp+".json"))
	if err != nil {
		t.Fatalf("read the shared fixture: %v", err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse %s.json: %v", idp, err)
	}
	if len(f.Tokens) == 0 {
		t.Fatalf("%s.json holds no tokens", idp)
	}
	return f
}

// callerOf is how the fixtures word Machine's answer.
func callerOf(d Dialect, r Raw) string {
	machine, err := d.Machine(r)
	switch {
	case err != nil:
		return "refused"
	case machine:
		return "machine"
	default:
		return "user"
	}
}

// Every sample token of both IdPs reads as the shared fixtures say. The same
// files pin the pod's Go twin, the TypeScript package and the console, so the
// four cannot drift apart.
func TestDialect_ReadsTheSharedFixtures(t *testing.T) {
	for _, idp := range []string{"thunder", "asgardeo"} {
		f := loadFixture(t, idp)
		d, err := New(f.claims())
		if err != nil {
			t.Fatalf("%s: %v", idp, err)
		}
		for _, tok := range f.Tokens {
			t.Run(idp+": "+tok.Name, func(t *testing.T) {
				if got, want := d.Org(tok.Payload), (Org{Handle: tok.Org.Handle, ID: tok.Org.ID, Name: tok.Org.Name}); got != want {
					t.Errorf("org = %+v, want %+v", got, want)
				}
				if got := callerOf(d, tok.Payload); got != tok.Caller {
					t.Errorf("caller = %s, want %s", got, tok.Caller)
				}
			})
		}
	}
}

// The claims the test-support helpers hand out are the fixtures', so a test
// built on them runs on the same names every reader is pinned to.
func TestForTestClaimsMatchTheSharedFixtures(t *testing.T) {
	for idp, got := range map[string]Claims{"thunder": ThunderClaimsForTest(), "asgardeo": AsgardeoClaimsForTest()} {
		if want := loadFixture(t, idp).claims(); got != want {
			t.Errorf("%s: test-support claims %+v, fixture %+v", idp, got, want)
		}
	}
}

// A dialect that could not find the org or tell a machine from a user does
// not get built: each missing name or value is named.
func TestNew_RefusesIncompleteClaims(t *testing.T) {
	full := AsgardeoClaimsForTest()
	for name, unset := range map[string]func(*Claims){
		"org.handle":          func(c *Claims) { c.OrgHandle = "" },
		"org.id":              func(c *Claims) { c.OrgID = " " },
		"caller_type.claim":   func(c *Claims) { c.CallerType = "" },
		"caller_type.machine": func(c *Claims) { c.Machine = "" },
	} {
		c := full
		unset(&c)
		if _, err := New(c); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: err = %v, want one naming it", name, err)
		}
	}
	// The org's display name and the user value are optional.
	c := full
	c.OrgName, c.User = "", ""
	if _, err := New(c); err != nil {
		t.Errorf("without the optional name and user value: %v", err)
	}
}

// The zero value reads nothing: no org, and no token placed as a user's or a
// machine's. A verifier handed no dialect refuses, it does not guess.
func TestZeroDialect_ReadsNothing(t *testing.T) {
	var d Dialect
	r := Raw{"ouHandle": "default", "ouId": "ou-1", "grant_type": "client_credentials", "org_handle": "acme", "aut": "APPLICATION"}
	if got := d.Org(r); got != (Org{}) {
		t.Errorf("org = %+v, want none", got)
	}
	if _, err := d.Machine(r); !errors.Is(err, ErrNoMachineClaim) {
		t.Errorf("machine err = %v, want ErrNoMachineClaim", err)
	}
}

// With a user value configured a third value is refused; with none, any
// value but the machine's is a user's.
func TestMachine_UserValueDecidesHowAThirdValueReads(t *testing.T) {
	strict := MustForTest(AsgardeoClaimsForTest())
	if _, err := strict.Machine(Raw{"aut": "DEVICE"}); !errors.Is(err, ErrUnknownMachineClaim) {
		t.Errorf("strict: err = %v, want ErrUnknownMachineClaim", err)
	}
	loose := AsgardeoClaimsForTest()
	loose.User = ""
	if machine, err := MustForTest(loose).Machine(Raw{"aut": "DEVICE"}); err != nil || machine {
		t.Errorf("no user value: machine=%v err=%v, want a user's", machine, err)
	}
	// A caller type claim that is not text is no claim.
	if _, err := strict.Machine(Raw{"aut": true}); !errors.Is(err, ErrNoMachineClaim) {
		t.Errorf("non-text claim: err = %v, want ErrNoMachineClaim", err)
	}
}

// One org under the IdP's own handle: a token declaring exactly that handle
// reads as the platform's name for the org, with the IdP's id and name kept;
// any other token reads as no org.
func TestWithSingleOrg(t *testing.T) {
	d := MustForTest(AsgardeoClaimsForTest()).WithSingleOrg("acmecorp", "default")
	own := Raw{"org_handle": "acmecorp", "org_id": "org-1", "org_name": "Acme Corp"}
	if got, want := d.Org(own), (Org{Handle: "default", ID: "org-1", Name: "Acme Corp"}); got != want {
		t.Errorf("the IdP's org = %+v, want %+v", got, want)
	}
	for name, r := range map[string]Raw{
		"another org of the IdP":     {"org_handle": "globex", "org_id": "org-2"},
		"the platform's own name":    {"org_handle": "default", "org_id": "org-1"},
		"no handle claim":            {"org_id": "org-1", "org_name": "acmecorp"},
		"a handle that is not text":  {"org_handle": []any{"acmecorp"}, "org_id": "org-1"},
		"another IdP's handle claim": {"ouHandle": "acmecorp", "ouId": "org-1"},
	} {
		if got := d.Org(r); got != (Org{}) {
			t.Errorf("%s: org = %+v, want none", name, got)
		}
	}
	// An install whose handle setting is empty maps nothing.
	if got := MustForTest(AsgardeoClaimsForTest()).WithSingleOrg("", "default").Org(Raw{"org_id": "org-1"}); got != (Org{}) {
		t.Errorf("empty handle setting: org = %+v, want none", got)
	}
}

// A wire struct decodes its typed claims and keeps the whole payload, and
// encodes the two back into one payload.
func TestDecodeEncode_KeepTheWholePayload(t *testing.T) {
	type typed struct {
		Sub string `json:"sub"`
	}
	payload := []byte(`{"sub":"u1","ouHandle":"default","nested":{"a":1}}`)
	var got typed
	raw, err := Decode(payload, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sub != "u1" || raw.text("ouHandle") != "default" || raw.text("nested") != "" {
		t.Fatalf("typed %+v raw %v", got, raw)
	}
	out, err := Encode(typed{Sub: "u2"}, Raw{"org_handle": "acme"})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back["sub"] != "u2" || back["org_handle"] != "acme" {
		t.Fatalf("encoded %s", out)
	}
}
