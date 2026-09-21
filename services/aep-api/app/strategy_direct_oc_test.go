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
	"bytes"
	"context"
	"log/slog"
	"strings"
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

// A tokenless, unmarked call still resolves to the BFF's own identity — the
// webhook and dispatch paths depend on it — but it is the fail-open branch, so
// it must announce itself. The WARN is what will tell us the branch is unused
// before it is turned into a refusal; a silent fallback would leave that
// unknowable.
func TestDirectOCStrategy_M2MWhenNoUserJWT_WarnsAboutTheFallback(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if got := (DirectOCStrategy{}).Decide(context.Background()); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M", got)
	}
	if !strings.Contains(buf.String(), "no service-identity marker") {
		t.Fatalf("the fail-open fallback must warn; log was %q", buf.String())
	}
}

// ...and the deliberate service-identity path must NOT warn, or the signal is
// buried under every watcher tick.
func TestDirectOCStrategy_MarkedServiceIdentityIsSilent(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := authn.WithServiceIdentity(context.Background())
	if got := (DirectOCStrategy{}).Decide(ctx); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("an explicitly marked service-identity call must not warn; log was %q", buf.String())
	}
}

func TestDirectOCStrategy_M2MWhenServiceIdentityMarked(t *testing.T) {
	ctx := authn.WithAuthToken(context.Background(), "user-jwt")
	ctx = authn.WithServiceIdentity(ctx)
	if got := (DirectOCStrategy{}).Decide(ctx); got != ocauth.AuthModeServiceM2M {
		t.Fatalf("got %v, want AuthModeServiceM2M even with a user JWT present", got)
	}
}
