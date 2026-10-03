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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSREHandoffVerifier_DisabledWithoutToken(t *testing.T) {
	if v := NewSREHandoffVerifier(""); v != nil {
		t.Fatalf("want nil verifier, got %+v", v)
	}
}

func TestSREHandoffVerifier_Verify(t *testing.T) {
	t.Parallel()
	v := NewSREHandoffVerifier("s3cr3t")
	for name, tc := range map[string]struct {
		bearer string
		want   bool
	}{
		"matching bearer":           {"Bearer s3cr3t", true},
		"wrong key":                 {"Bearer wrong", false},
		"key prefix only":           {"Bearer s3cr3", false},
		"missing Bearer prefix":     {"s3cr3t", false},
		"empty header":              {"", false},
		"Bearer with an empty key":  {"Bearer ", false},
		"lower-case scheme refused": {"bearer s3cr3t", false},
	} {
		if got := v.Verify(tc.bearer); got != tc.want {
			t.Errorf("%s: Verify(%q) = %v, want %v", name, tc.bearer, got, tc.want)
		}
	}

	var nilVerifier *SREHandoffVerifier
	if nilVerifier.Verify("Bearer s3cr3t") {
		t.Error("a nil verifier must reject")
	}
}

func TestSREHandoffVerifier_Middleware(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := NewSREHandoffVerifier("s3cr3t").Middleware(next)
	for bearer, want := range map[string]int{"Bearer s3cr3t": http.StatusNoContent, "Bearer wrong": http.StatusUnauthorized, "": http.StatusUnauthorized} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if bearer != "" {
			r.Header.Set("Authorization", bearer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("bearer %q: status = %d, want %d", bearer, w.Code, want)
		}
	}
}
