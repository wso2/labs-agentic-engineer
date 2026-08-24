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

package app

import (
	"context"
	"testing"

	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/ocauth"
)

func TestUserJWTStrategy_ForwardsWhenUserJWTPresent(t *testing.T) {
	ctx := authn.WithAuthToken(context.Background(), "user-jwt")
	if got := (UserJWTStrategy{}).Decide(ctx); got != ocauth.AuthModeUserJWT {
		t.Fatalf("got %v, want AuthModeUserJWT", got)
	}
}

func TestUserJWTStrategy_FailsClosedWithoutTokenOrMarker(t *testing.T) {
	if got := (UserJWTStrategy{}).Decide(context.Background()); got != ocauth.AuthModeNone {
		t.Fatalf("got %v, want AuthModeNone (no implicit M2M grant)", got)
	}
}

func TestUserJWTStrategy_FallsBackToM2MForServiceIdentity(t *testing.T) {
	ctx := authn.WithAuthToken(context.Background(), "user-jwt")
	ctx = authn.WithServiceIdentity(ctx)
	if got := (UserJWTStrategy{}).Decide(ctx); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M", got)
	}
}

func TestUserJWTStrategy_ServiceIdentityMarkerAloneGrantsM2M(t *testing.T) {
	// The legitimate no-user-JWT path: webhook handlers / Temporal activities
	// mark the context explicitly, with no token present at all.
	ctx := authn.WithServiceIdentity(context.Background())
	if got := (UserJWTStrategy{}).Decide(ctx); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M", got)
	}
}
