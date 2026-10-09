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
	"errors"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// A 409 keeps its refusal's own code on the wire, like every other status: a
// client branches on ae_studio_setup_incomplete, not on a generic "conflict".
func TestMapConfigError_ConflictKeepsItsCode(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{"coded conflict", AEStudioSetupIncompleteCode, AEStudioSetupIncompleteCode},
		{"uncoded conflict", "", apierr.CodeConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := MapConfigError(&SectionError{Section: "gitProvider", Status: http.StatusConflict, Code: tc.code, Message: "m"})
			var ae *apierr.Error
			if !errors.As(err, &ae) {
				t.Fatalf("err = %T, want *apierr.Error", err)
			}
			if ae.Status != http.StatusConflict || ae.Code != tc.want {
				t.Fatalf("status=%d code=%q, want 409 %q", ae.Status, ae.Code, tc.want)
			}
		})
	}
}
