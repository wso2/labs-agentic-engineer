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

type fakeOUValidator struct {
	exists bool
	err    error
}

func (f fakeOUValidator) OUExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.err
}

// TestOUIsTrustworthy locks the guard that prevents a stale/phantom JWT ouId
// from poisoning the org→OU mapping (the root cause of the runner publisher
// cc-token invalid_client). The rule: reject ONLY when a wired validator
// positively says the OU does not exist; otherwise fail-open.
func TestOUIsTrustworthy(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		ouID      string
		validator OUValidator
		want      bool
	}{
		{"empty id is trusted", "", fakeOUValidator{exists: false}, true},
		{"no validator wired → trust (legacy behavior)", "ou-x", nil, true},
		{"validator confirms OU exists → trust", "ou-real", fakeOUValidator{exists: true}, true},
		{"validator says OU does NOT exist → reject (the phantom case)", "ou-phantom", fakeOUValidator{exists: false}, false},
		{"validator errors → fail-open (don't block org-ensure on a Thunder hiccup)", "ou-x", fakeOUValidator{err: errors.New("thunder down")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &organizationService{ouValidator: tc.validator}
			if got := s.ouIsTrustworthy(ctx, tc.ouID); got != tc.want {
				t.Fatalf("ouIsTrustworthy(%q) = %v, want %v", tc.ouID, got, tc.want)
			}
		})
	}
}
