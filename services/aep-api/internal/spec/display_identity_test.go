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

package spec

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// UNIT tier: the display name rule over VERIFIED claims — the same branches
// the raw-header projection has (explicit name, given+family assembly, the
// "User" surname scrub, subject fallback).
func TestDisplayIdentity(t *testing.T) {
	cases := []struct {
		name      string
		claims    *auth.Claims
		wantName  string
		wantEmail string
	}{
		{name: "no claims", claims: nil},
		{name: "explicit name wins", claims: &auth.Claims{Subject: "u", Name: "Ada Lovelace", GivenName: "A", Email: "ada@x.io"},
			wantName: "Ada Lovelace", wantEmail: "ada@x.io"},
		{name: "given and family assemble", claims: &auth.Claims{Subject: "u", GivenName: " Grace ", FamilyName: " Hopper "},
			wantName: "Grace Hopper"},
		{name: "the placeholder surname User is dropped", claims: &auth.Claims{Subject: "u", GivenName: "Alan", FamilyName: "user"},
			wantName: "Alan"},
		{name: "subject is the last resort", claims: &auth.Claims{Subject: "u-42", Email: "x@y.io"},
			wantName: "u-42", wantEmail: "x@y.io"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, email := displayIdentity(tc.claims)
			if name != tc.wantName || email != tc.wantEmail {
				t.Fatalf("displayIdentity = (%q,%q), want (%q,%q)", name, email, tc.wantName, tc.wantEmail)
			}
		})
	}
}

// The credit a pod turn carries is the verified caller: its subject, and the
// display identity of the same claims. No caller (a background path) is no
// credit, never an invented one.
func TestCreditFrom(t *testing.T) {
	ctx := auth.WithClaims(context.Background(), &auth.Claims{Subject: "u-1", GivenName: "Ada", FamilyName: "Lovelace", Email: "ada@x.io"})
	if got, want := creditFrom(ctx), (aestudiotools.Credit{UserID: "u-1", Name: "Ada Lovelace", Email: "ada@x.io"}); got != want {
		t.Fatalf("creditFrom = %+v, want %+v", got, want)
	}
	if got := creditFrom(context.Background()); got != (aestudiotools.Credit{}) {
		t.Fatalf("creditFrom without claims = %+v, want empty", got)
	}
}
