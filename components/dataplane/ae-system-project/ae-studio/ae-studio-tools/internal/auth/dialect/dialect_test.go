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
		// A kind this build does not know must not boot on Thunder's rules.
		{"asgardeo", "", false},
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
