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
	"errors"
	"testing"
)

func TestParseKind(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Kind
		ok   bool
	}{
		{"", KindThunder, true},
		{"thunder", KindThunder, true},
		{" Thunder ", KindThunder, true},
		{"asgardeo", KindAsgardeo, true},
		{" Asgardeo ", KindAsgardeo, true},
		// A kind this build does not know must not boot on another kind's
		// rules by accident.
		{"okta", "", false},
	} {
		got, err := ParseKind(tc.in)
		if tc.ok != (err == nil) {
			t.Fatalf("ParseKind(%q): err=%v, want ok=%v", tc.in, err, tc.ok)
		}
		if got != tc.want {
			t.Fatalf("ParseKind(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The Thunder org precedence is the one the console mirrors: ouHandle, then
// ouName, then ouId.
func TestThunder_Org(t *testing.T) {
	d := ForKind(KindThunder)
	for name, tc := range map[string]struct {
		raw  Raw
		want Org
	}{
		"handle wins": {
			raw:  Raw{OuHandle: "acme", OuName: "Acme Inc", OuID: "11111111-1111-1111-1111-111111111111"},
			want: Org{Handle: "acme", Name: "Acme Inc", ID: "11111111-1111-1111-1111-111111111111", HandleDeclared: true},
		},
		"name when no handle": {
			raw:  Raw{OuName: "Acme Inc", OuID: "11111111-1111-1111-1111-111111111111"},
			want: Org{Handle: "Acme Inc", Name: "Acme Inc", ID: "11111111-1111-1111-1111-111111111111"},
		},
		"id when nothing else": {
			raw:  Raw{OuID: "11111111-1111-1111-1111-111111111111"},
			want: Org{Handle: "11111111-1111-1111-1111-111111111111", ID: "11111111-1111-1111-1111-111111111111"},
		},
		"no org claims": {raw: Raw{GrantType: "authorization_code"}, want: Org{}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := d.Org(tc.raw); got != tc.want {
				t.Fatalf("Org(%+v) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

// Thunder says what grant a token was issued under; client_credentials is a
// machine's. A token without the claim answers neither, so a user-only route
// can refuse it rather than take it for a user's.
func TestThunder_Machine(t *testing.T) {
	d := ForKind(KindThunder)
	for name, tc := range map[string]struct {
		raw     Raw
		machine bool
		err     error
	}{
		"client_credentials is a machine": {raw: Raw{GrantType: "client_credentials"}, machine: true},
		"authorization_code is a user":    {raw: Raw{GrantType: "authorization_code"}, machine: false},
		"refresh_token is a user":         {raw: Raw{GrantType: "refresh_token"}, machine: false},
		"no claim is an error":            {raw: Raw{OuHandle: "acme"}, err: ErrNoMachineClaim},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := d.Machine(tc.raw)
			if !errors.Is(err, tc.err) {
				t.Fatalf("Machine(%+v): err=%v, want %v", tc.raw, err, tc.err)
			}
			if got != tc.machine {
				t.Fatalf("Machine(%+v) = %v, want %v", tc.raw, got, tc.machine)
			}
		})
	}
}

// Asgardeo names the org in org_handle / org_id / org_name on every token.
// There is no fallback: a token without org_handle names no handle.
func TestAsgardeo_Org(t *testing.T) {
	d := ForKind(KindAsgardeo)
	for name, tc := range map[string]struct {
		raw  Raw
		want Org
	}{
		"the three org claims": {
			raw:  Raw{OrgHandle: "acme", OrgName: "Acme Inc", OrgID: "22222222-2222-2222-2222-222222222222"},
			want: Org{Handle: "acme", Name: "Acme Inc", ID: "22222222-2222-2222-2222-222222222222", HandleDeclared: true},
		},
		"no handle, no fallback to the name or the id": {
			raw:  Raw{OrgName: "Acme Inc", OrgID: "22222222-2222-2222-2222-222222222222"},
			want: Org{Name: "Acme Inc", ID: "22222222-2222-2222-2222-222222222222"},
		},
		"Thunder's claims name no org here": {
			raw:  Raw{OuHandle: "acme", OuName: "Acme Inc", OuID: "11111111-1111-1111-1111-111111111111", Aut: "APPLICATION_USER"},
			want: Org{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := d.Org(tc.raw); got != tc.want {
				t.Fatalf("Org(%+v) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

// Asgardeo says who a token was issued to in aut, which has two values. A
// token without the claim, or with a value that is neither, answers neither.
func TestAsgardeo_Machine(t *testing.T) {
	d := ForKind(KindAsgardeo)
	for name, tc := range map[string]struct {
		raw     Raw
		machine bool
		err     error
	}{
		"APPLICATION is a machine":              {raw: Raw{Aut: "APPLICATION"}, machine: true},
		"APPLICATION_USER is a user":            {raw: Raw{Aut: "APPLICATION_USER"}, machine: false},
		"no claim is an error":                  {raw: Raw{OrgHandle: "acme"}, err: ErrNoMachineClaim},
		"an unknown value is an error":          {raw: Raw{Aut: "DEVICE"}, err: ErrUnknownMachineClaim},
		"Thunder's grant_type is not read":      {raw: Raw{GrantType: "authorization_code"}, err: ErrNoMachineClaim},
		"the value is matched case-sensitively": {raw: Raw{Aut: "application"}, err: ErrUnknownMachineClaim},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := d.Machine(tc.raw)
			if !errors.Is(err, tc.err) {
				t.Fatalf("Machine(%+v): err=%v, want %v", tc.raw, err, tc.err)
			}
			if got != tc.machine {
				t.Fatalf("Machine(%+v) = %v, want %v", tc.raw, got, tc.machine)
			}
		})
	}
}

// Thunder does not read Asgardeo's claims either: a token of the other kind
// names no org and cannot be placed.
func TestThunder_DoesNotReadAsgardeoClaims(t *testing.T) {
	d := ForKind(KindThunder)
	raw := Raw{OrgHandle: "acme", OrgID: "22222222-2222-2222-2222-222222222222", OrgName: "Acme Inc", Aut: "APPLICATION_USER"}
	if got := d.Org(raw); got != (Org{}) {
		t.Fatalf("Org = %+v, want none", got)
	}
	if _, err := d.Machine(raw); !errors.Is(err, ErrNoMachineClaim) {
		t.Fatalf("Machine: err=%v, want %v", err, ErrNoMachineClaim)
	}
}

// A Dialect nobody gave a kind (the zero value, a wiring mistake) and a kind
// this build does not know read nothing: no org, and no token placed as a
// user's or a machine's, so every gate that asks refuses.
func TestUnknownKind_ReadsNothing(t *testing.T) {
	raw := Raw{
		OuHandle: "acme", OuID: "11111111-1111-1111-1111-111111111111", GrantType: "authorization_code",
		OrgHandle: "acme", OrgID: "22222222-2222-2222-2222-222222222222", Aut: "APPLICATION_USER",
	}
	for name, d := range map[string]Dialect{"zero value": {}, "unknown kind": ForKind(Kind("okta"))} {
		t.Run(name, func(t *testing.T) {
			if got := d.Org(raw); got != (Org{}) {
				t.Fatalf("Org = %+v, want none", got)
			}
			if machine, err := d.Machine(raw); machine || !errors.Is(err, ErrNoMachineClaim) {
				t.Fatalf("Machine = %v, %v; want false, %v", machine, err, ErrNoMachineClaim)
			}
		})
	}
}

// An IdP that serves one org names it by its own handle (Asgardeo: the
// tenant's), not by the platform's name for it (the OpenChoreo namespace).
// WithSingleOrg maps the one to the other: a token declaring the IdP handle
// reads as the platform's org, and any other token reads as no org at all.
func TestWithSingleOrg(t *testing.T) {
	const orgID = "22222222-2222-2222-2222-222222222222"
	d := ForKind(KindAsgardeo).WithSingleOrg("acmecorp", "default")
	for name, tc := range map[string]struct {
		raw  Raw
		want Org
	}{
		"the IdP's handle maps to the platform's": {
			raw:  Raw{OrgHandle: "acmecorp", OrgName: "Acme Corp", OrgID: orgID},
			want: Org{Handle: "default", Name: "Acme Corp", ID: orgID, HandleDeclared: true},
		},
		"another org of the IdP is no org": {
			raw:  Raw{OrgHandle: "globex", OrgName: "Globex", OrgID: "33333333-3333-3333-3333-333333333333"},
			want: Org{},
		},
		"the platform's own name in the token is no org": {
			raw:  Raw{OrgHandle: "default", OrgID: orgID},
			want: Org{},
		},
		"no handle is no org, whatever the id": {
			raw:  Raw{OrgName: "Acme Corp", OrgID: orgID},
			want: Org{},
		},
		"the match is exact": {
			raw:  Raw{OrgHandle: "AcmeCorp", OrgID: orgID},
			want: Org{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := d.Org(tc.raw); got != tc.want {
				t.Fatalf("Org(%+v) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}

	// The machine-token rule is not the mapping's business.
	if machine, err := d.Machine(Raw{Aut: "APPLICATION"}); err != nil || !machine {
		t.Fatalf("Machine = %v, %v; want true", machine, err)
	}
	// The kind is kept, and a dialect without the mapping reads the handle as it is.
	if d.Kind() != KindAsgardeo {
		t.Fatalf("Kind() = %q", d.Kind())
	}
	plain := ForKind(KindAsgardeo).Org(Raw{OrgHandle: "acmecorp", OrgID: orgID})
	if plain.Handle != "acmecorp" {
		t.Fatalf("unmapped handle = %q, want acmecorp", plain.Handle)
	}
}

// A handle filled from a fallback claim is not the IdP's handle: only a
// declared one maps.
func TestWithSingleOrg_RequiresTheDeclaredHandle(t *testing.T) {
	d := ForKind(KindThunder).WithSingleOrg("acmecorp", "default")
	if got := d.Org(Raw{OuName: "acmecorp", OuID: "ou-1"}); got != (Org{}) {
		t.Fatalf("Org = %+v, want none", got)
	}
	want := Org{Handle: "default", ID: "ou-1", HandleDeclared: true}
	if got := d.Org(Raw{OuHandle: "acmecorp", OuID: "ou-1"}); got != want {
		t.Fatalf("Org = %+v, want %+v", got, want)
	}
}

func TestDialect_Kind(t *testing.T) {
	for _, k := range []Kind{KindThunder, KindAsgardeo} {
		if got := ForKind(k).Kind(); got != k {
			t.Fatalf("Kind() = %q, want %q", got, k)
		}
	}
}
