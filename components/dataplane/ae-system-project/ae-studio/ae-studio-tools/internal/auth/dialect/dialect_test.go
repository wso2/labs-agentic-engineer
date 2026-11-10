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
		// A kind this build does not know must not boot on another kind's rules.
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

// The Thunder org precedence, and whether the handle was the token's own.
func TestThunder_Org(t *testing.T) {
	d := ForKind(KindThunder)
	for name, tc := range map[string]struct {
		raw  Raw
		want Org
	}{
		"handle wins": {
			raw:  Raw{OuHandle: "acme", OuName: "Acme Inc", OuID: "ou-1"},
			want: Org{Handle: "acme", Name: "Acme Inc", ID: "ou-1", HandleDeclared: true},
		},
		"name when no handle": {
			raw:  Raw{OuName: "Acme Inc", OuID: "ou-1"},
			want: Org{Handle: "Acme Inc", Name: "Acme Inc", ID: "ou-1"},
		},
		"id when nothing else": {
			raw:  Raw{OuID: "ou-1"},
			want: Org{Handle: "ou-1", ID: "ou-1"},
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

// client_credentials is a machine's; any other grant a user's; no grant
// answers neither, so a gate can refuse it rather than guess.
func TestThunder_Machine(t *testing.T) {
	d := ForKind(KindThunder)
	for name, tc := range map[string]struct {
		raw     Raw
		machine bool
		err     error
	}{
		"client_credentials is a machine": {raw: Raw{GrantType: "client_credentials"}, machine: true},
		"authorization_code is a user":    {raw: Raw{GrantType: "authorization_code"}},
		"refresh_token is a user":         {raw: Raw{GrantType: "refresh_token"}},
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

// Asgardeo names the org in org_handle / org_id / org_name, with no fallback.
func TestAsgardeo_Org(t *testing.T) {
	d := ForKind(KindAsgardeo)
	for name, tc := range map[string]struct {
		raw  Raw
		want Org
	}{
		"the three org claims": {
			raw:  Raw{OrgHandle: "acme", OrgName: "Acme Inc", OrgID: "org-1"},
			want: Org{Handle: "acme", Name: "Acme Inc", ID: "org-1", HandleDeclared: true},
		},
		"no handle, no fallback to the name or the id": {
			raw:  Raw{OrgName: "Acme Inc", OrgID: "org-1"},
			want: Org{Name: "Acme Inc", ID: "org-1"},
		},
		"Thunder's claims name no org here": {
			raw:  Raw{OuHandle: "acme", OuName: "Acme Inc", OuID: "ou-1", Aut: "APPLICATION_USER"},
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

// aut has two values: APPLICATION is a machine's, APPLICATION_USER a user's.
// No claim, or a value that is neither, answers neither.
func TestAsgardeo_Machine(t *testing.T) {
	d := ForKind(KindAsgardeo)
	for name, tc := range map[string]struct {
		raw     Raw
		machine bool
		err     error
	}{
		"APPLICATION is a machine":              {raw: Raw{Aut: "APPLICATION"}, machine: true},
		"APPLICATION_USER is a user":            {raw: Raw{Aut: "APPLICATION_USER"}},
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

// Thunder does not read Asgardeo's claims either.
func TestThunder_DoesNotReadAsgardeoClaims(t *testing.T) {
	d := ForKind(KindThunder)
	raw := Raw{OrgHandle: "acme", OrgID: "org-1", OrgName: "Acme Inc", Aut: "APPLICATION_USER"}
	if got := d.Org(raw); got != (Org{}) {
		t.Fatalf("Org = %+v, want none", got)
	}
	if _, err := d.Machine(raw); !errors.Is(err, ErrNoMachineClaim) {
		t.Fatalf("Machine: err=%v, want %v", err, ErrNoMachineClaim)
	}
}

// The install-wide client's token: Thunder registers that client outside any
// org, so its token must carry none; Asgardeo stamps the tenant's org on
// every token, so there it must be the pod's.
func TestInstallClientOrgOK(t *testing.T) {
	for name, tc := range map[string]struct {
		d    Dialect
		org  Org
		want bool
	}{
		"thunder: no org":             {d: ForKind(KindThunder), org: Org{}, want: true},
		"thunder: the pod's org":      {d: ForKind(KindThunder), org: Org{ID: "org-1"}, want: false},
		"thunder: another org":        {d: ForKind(KindThunder), org: Org{ID: "org-2"}, want: false},
		"asgardeo: the pod's org":     {d: ForKind(KindAsgardeo), org: Org{ID: "org-1", Handle: "acme", HandleDeclared: true}, want: true},
		"asgardeo: another org":       {d: ForKind(KindAsgardeo), org: Org{ID: "org-2"}, want: false},
		"asgardeo: no org":            {d: ForKind(KindAsgardeo), org: Org{}, want: false},
		"unknown kind: no org":        {d: Dialect{}, org: Org{}, want: false},
		"unknown kind: the pod's org": {d: Dialect{}, org: Org{ID: "org-1"}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.d.InstallClientOrgOK(tc.org, "org-1"); got != tc.want {
				t.Fatalf("InstallClientOrgOK(%+v) = %v, want %v", tc.org, got, tc.want)
			}
		})
	}
}

// A Dialect nobody gave a kind and a kind this build does not know read
// nothing, so every gate that asks refuses.
func TestUnknownKind_ReadsNothing(t *testing.T) {
	raw := Raw{
		OuHandle: "acme", OuID: "ou-1", GrantType: "authorization_code",
		OrgHandle: "acme", OrgID: "org-1", Aut: "APPLICATION_USER",
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
