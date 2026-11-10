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
		// Step 2 adds asgardeo; until then an install naming it must not boot
		// on the Thunder rules by accident.
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

func TestDialect_Kind(t *testing.T) {
	if got := ForKind(KindThunder).Kind(); got != KindThunder {
		t.Fatalf("Kind() = %q", got)
	}
}
