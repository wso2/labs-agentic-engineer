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

package auth

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// The verified projection carries the display claims, so a caller that
// credits a user (a kickoff turn) reads them off the verified token rather
// than re-parsing the raw header unverified.
func TestClaimsOf_ProjectsTheVerifiedDisplayClaims(t *testing.T) {
	got := claimsOf(&jwtassertion.TokenClaims{
		Sub: "u-1", ClientID: "console", OuHandle: "acme", OuName: "Acme", OuId: "ou-1",
		Name: "Ada Lovelace", Email: "ada@acme.io", GivenName: "Ada", FamilyName: "Lovelace",
	})
	want := Claims{
		Subject: "u-1", ClientID: "console", OuHandle: "acme", OuName: "Acme", OuId: "ou-1",
		Name: "Ada Lovelace", Email: "ada@acme.io", GivenName: "Ada", FamilyName: "Lovelace",
	}
	if *got != want {
		t.Fatalf("claims = %+v, want %+v", *got, want)
	}
}
