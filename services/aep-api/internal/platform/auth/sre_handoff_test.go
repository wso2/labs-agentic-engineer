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

import "testing"

func TestNewSREHandoffVerifier_DisabledWhenUnconfigured(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, secret, org string
	}{
		{"both empty", "", ""},
		{"secret empty", "", "acme"},
		{"org empty", "s3cr3t", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if v := NewSREHandoffVerifier(tc.secret, tc.org); v != nil {
				t.Fatalf("want nil verifier, got %+v", v)
			}
		})
	}
}

func TestSREHandoffVerifier_Verify(t *testing.T) {
	t.Parallel()
	v := NewSREHandoffVerifier("s3cr3t", "acme")

	t.Run("matching bearer binds the configured org", func(t *testing.T) {
		claims, ok := v.Verify("Bearer s3cr3t")
		if !ok {
			t.Fatal("want verified, got rejected")
		}
		if claims.OuHandle != "acme" {
			t.Fatalf("OuHandle = %q, want acme", claims.OuHandle)
		}
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		if _, ok := v.Verify("Bearer wrong"); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("missing Bearer prefix is rejected", func(t *testing.T) {
		if _, ok := v.Verify("s3cr3t"); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("empty header is rejected", func(t *testing.T) {
		if _, ok := v.Verify(""); ok {
			t.Fatal("want rejected, got verified")
		}
	})

	t.Run("nil verifier always rejects", func(t *testing.T) {
		var nilVerifier *SREHandoffVerifier
		if _, ok := nilVerifier.Verify("Bearer s3cr3t"); ok {
			t.Fatal("want rejected, got verified")
		}
	})
}
