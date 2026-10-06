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

package organization

import (
	"context"
	"errors"
	"testing"
)

// A BYO IdP (custom or asgardeo) is only trusted through its issuer: a deploy
// pins the org's protected APIs to it, and with none they trust every
// keymanager on the cluster. So SetProfile refuses one without an issuer as a
// validation error (400 body.idp at the edge) and writes nothing.
func TestSetProfile_BYOKindWithoutIssuerIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, kind, issuer string }{
		{"custom, empty", "custom", ""},
		{"custom, blank", "custom", "  \t"},
		{"asgardeo, empty", "asgardeo", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newMemIDPRepo()
			svc := NewIDPService(repo, stubOrgRepo{}, &fakeThunder{}, PlatformIDPConfig{Issuer: "https://platform.example"})

			_, err := svc.SetProfile(context.Background(), "acme", "ada", tc.kind, tc.issuer, "")
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("SetProfile error = %v; want a *ValidationError", err)
			}
			if len(repo.profiles) != 0 || len(repo.audits) != 0 {
				t.Fatalf("a refused profile wrote %d rows, %d audits; want none", len(repo.profiles), len(repo.audits))
			}
		})
	}
}

// The platform kind takes the cluster's issuer whatever the caller sent, so it
// needs none in the request.
func TestSetProfile_PlatformKindNeedsNoIssuer(t *testing.T) {
	t.Parallel()
	svc := NewIDPService(newMemIDPRepo(), stubOrgRepo{}, &fakeThunder{}, PlatformIDPConfig{Issuer: "https://platform.example"})

	got, err := svc.SetProfile(context.Background(), "acme", "ada", "platform", "", "")
	if err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if got.Issuer != "https://platform.example" {
		t.Fatalf("issuer = %q; want the platform default", got.Issuer)
	}
}
