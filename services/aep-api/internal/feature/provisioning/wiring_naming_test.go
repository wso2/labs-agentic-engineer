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

package provisioning

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
)

// Test_envVarName_matchesSharedHelper is the cross-package naming-consistency
// guard: wiring.go's pod env-var name for a dep+output MUST equal the key
// runtimeconfig emits into window._env_ for the same dep+output. Both derive
// through resources.EnvVarName (the single source of truth); this test fails
// the moment anyone re-inlines a divergent copy on either side, so the coding
// agent (pod env) and the browser (window._env_) can never drift apart.
func Test_envVarName_matchesSharedHelper(t *testing.T) {
	t.Parallel()
	cases := []struct{ dep, out string }{
		{"user-auth", "client_id"},
		{"user-auth", "issuer"},
		{"user-auth", "jwks_url"},
		{"user-auth", "scopes"},
		{"orders-db", "host"},
		{"orders-db", "port"},
		{"weird.name", "OUT-put"},
		{"", ""},
	}
	for _, c := range cases {
		if got, want := envVarName(c.dep, c.out), resources.EnvVarName(c.dep, c.out); got != want {
			t.Errorf("envVarName(%q,%q) = %q; want %q (must match the shared resources.EnvVarName runtimeconfig uses)", c.dep, c.out, got, want)
		}
	}
}
