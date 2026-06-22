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

package api

import (
	"strings"
	"testing"
)

// TestHumaRegistration_NoDupAndComplete registers every migrated feature on a
// single Huma API (zero deps — registration never calls them) to prove there is
// no duplicate operation/path panic and that the generated spec carries a
// representative operation per feature plus the security schemes. This is the
// composition-root sanity check short of standing up the full handler.
func TestHumaRegistration_NoDupAndComplete(t *testing.T) {
	spec, err := GenerateOpenAPIYAML()
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	s := string(spec)

	wantOps := []string{
		"list-projects", "list-organizations", "list-components", "get-component-config",
		"get-requirements", "get-collab-session", "get-design", "list-tasks", "get-board",
		"get-idp-profile", "get-github-status", "get-anthropic-status",
	}
	for _, op := range wantOps {
		if !strings.Contains(s, op) {
			t.Errorf("spec missing operation %q", op)
		}
	}
	for _, scheme := range []string{"userJWT", "bearerFormat"} {
		if !strings.Contains(s, scheme) {
			t.Errorf("spec missing security scheme marker %q", scheme)
		}
	}
}
