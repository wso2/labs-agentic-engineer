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
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/authz"
)

func TestClaimsPermissions(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		want  []authz.Permission
	}{
		{
			name:  "mixed known and unknown scopes",
			scope: "openid profile ae:build ae:build-view system",
			want:  []authz.Permission{authz.PermissionBuild, authz.PermissionBuildView},
		},
		{
			name:  "no ae: scopes at all",
			scope: "openid profile system",
			want:  nil,
		},
		{
			name:  "empty scope",
			scope: "",
			want:  nil,
		},
		{
			name:  "whitespace-only scope",
			scope: "   \t  ",
			want:  nil,
		},
		{
			name:  "extra whitespace between scopes",
			scope: "  ae:build   ae:skill-config  ",
			want:  []authz.Permission{authz.PermissionBuild, authz.PermissionSkillConfig},
		},
		{
			name:  "a single unknown ae:-shaped token is not a permission",
			scope: "ae:not-a-real-permission",
			want:  nil,
		},
		{
			name:  "duplicate scope entries are not deduplicated",
			scope: "ae:build ae:build",
			want:  []authz.Permission{authz.PermissionBuild, authz.PermissionBuild},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := &Claims{Scope: tc.scope}
			got := claims.Permissions()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Permissions() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClaimsPermissions_NilReceiver(t *testing.T) {
	var claims *Claims
	if got := claims.Permissions(); got != nil {
		t.Fatalf("Permissions() on a nil *Claims = %v, want nil", got)
	}
}
