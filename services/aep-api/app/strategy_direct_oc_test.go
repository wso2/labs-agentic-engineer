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

func TestDirectOCStrategy_ForwardsUserJWTWhenPresent(t *testing.T) {
	ctx := authn.WithAuthToken(context.Background(), "user-jwt")
	if got := (DirectOCStrategy{}).Decide(ctx); got != ocauth.AuthModeUserJWT {
		t.Fatalf("got %v, want AuthModeUserJWT", got)
	}
}

func TestDirectOCStrategy_M2MWhenNoUserJWT(t *testing.T) {
	if got := (DirectOCStrategy{}).Decide(context.Background()); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M", got)
	}
}

func TestDirectOCStrategy_M2MWhenServiceIdentityMarked(t *testing.T) {
	ctx := authn.WithAuthToken(context.Background(), "user-jwt")
	ctx = authn.WithServiceIdentity(ctx)
	if got := (DirectOCStrategy{}).Decide(ctx); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M even with a user JWT present", got)
	}
}
