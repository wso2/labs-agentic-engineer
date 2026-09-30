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
	"context"
	"errors"
	"testing"
)

// fakeTokenGetter is a TokenGetter test double standing in for
// sreagent.Tokens: token/ok/err are returned verbatim from Get, regardless of
// the org asked for, since these tests only ever exercise one org.
type fakeTokenGetter struct {
	token string
	ok    bool
	err   error
}

func (f fakeTokenGetter) Get(_ context.Context, _ string) (string, bool, error) {
	return f.token, f.ok, f.err
}

func TestNewSREHandoffVerifier_DisabledWhenUnconfigured(t *testing.T) {
	t.Parallel()
	tokens := fakeTokenGetter{token: "s3cr3t", ok: true}

	t.Run("org empty", func(t *testing.T) {
		if v := NewSREHandoffVerifier("", tokens); v != nil {
			t.Fatalf("want nil verifier, got %+v", v)
		}
	})

	t.Run("tokens nil", func(t *testing.T) {
		if v := NewSREHandoffVerifier("acme", nil); v != nil {
			t.Fatalf("want nil verifier, got %+v", v)
		}
	})
}

func TestSREHandoffVerifier_Verify(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("matching bearer binds the configured org", func(t *testing.T) {
		v := NewSREHandoffVerifier("acme", fakeTokenGetter{token: "s3cr3t", ok: true})
		claims, ok := v.Verify(ctx, "Bearer s3cr3t")
		if !ok {
			t.Fatal("want verified, got rejected")
		}
		if claims.OuHandle != "acme" {
			t.Fatalf("OuHandle = %q, want acme", claims.OuHandle)
		}
	})

	t.Run("wrong token is rejected", func(t *testing.T) {
		v := NewSREHandoffVerifier("acme", fakeTokenGetter{token: "s3cr3t", ok: true})
		if _, ok := v.Verify(ctx, "Bearer wrong"); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("missing Bearer prefix is rejected", func(t *testing.T) {
		v := NewSREHandoffVerifier("acme", fakeTokenGetter{token: "s3cr3t", ok: true})
		if _, ok := v.Verify(ctx, "s3cr3t"); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("empty header is rejected", func(t *testing.T) {
		v := NewSREHandoffVerifier("acme", fakeTokenGetter{token: "s3cr3t", ok: true})
		if _, ok := v.Verify(ctx, ""); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("no token minted yet is rejected", func(t *testing.T) {
		v := NewSREHandoffVerifier("acme", fakeTokenGetter{ok: false})
		if _, ok := v.Verify(ctx, "Bearer anything"); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("token store error is rejected", func(t *testing.T) {
		v := NewSREHandoffVerifier("acme", fakeTokenGetter{err: errors.New("store unavailable")})
		if _, ok := v.Verify(ctx, "Bearer anything"); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("nil verifier always rejects", func(t *testing.T) {
		var nilVerifier *SREHandoffVerifier
		if _, ok := nilVerifier.Verify(ctx, "Bearer s3cr3t"); ok {
			t.Fatal("want rejected, got verified")
		}
	})
}
