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
		"get-requirements", "get-spec-collab-session", "get-design-bundle",
		// Tasks-github-native surface (§9.1): the board/dispatch/retry ops are gone.
		"list-tasks", "get-task", "plan-tasks", "execute-task", "hold-task", "unhold-task",
		// The task-log endpoint is now one SSE stream (status + executions +
		// unified timeline), not the cursor-poll it replaced.
		"stream-task-log",
		// Consolidated org-config surface (replaces the Anthropic/GitHub/IDP tag groups).
		"get-config", "update-config", "list-skills",
		// Single-tag build surface (the contract's build-project/get-project-build).
		"build-project", "get-project-build", "list-project-tags",
		// Alerts (console issues #154, #155, BE handshake #156).
		"list-rca-agent-reports", "create-rca-agent-report", "get-rca-agent-report",
	}
	for _, op := range wantOps {
		if !strings.Contains(s, op) {
			t.Errorf("spec missing operation %q", op)
		}
	}

	// De-exposed surfaces must stay gone: devflows left the public edge with
	// the build endpoint, and the per-artifact save ops would cut tags that
	// bypass the whole-spec build gate.
	wantAbsent := []string{
		"start-devflow", "list-devflows", "get-devflow", "decide-devflow-gate",
		"save-requirements", "save-design",
	}
	for _, op := range wantAbsent {
		if strings.Contains(s, op) {
			t.Errorf("spec still serves de-exposed operation %q", op)
		}
	}
	for _, scheme := range []string{"userJWT", "bearerFormat"} {
		if !strings.Contains(s, scheme) {
			t.Errorf("spec missing security scheme marker %q", scheme)
		}
	}
}
