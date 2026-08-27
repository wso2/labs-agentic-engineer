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

package edge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInternalContract asserts the committed internal contract describes
// exactly the runner-callback operations with the S2S security schemes — and
// only those. (The Huma export is gone; the contract is the source of truth.)
func TestInternalContract(t *testing.T) {
	out, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "contracts", "api", "internal", "v1", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read internal contract: %v", err)
	}
	yaml := string(out)

	for _, want := range []string{
		"runner-refresh-credentials",
		// Token refresh is every runner's, not validation's, so it keeps its path.
		// The id it names is the dispatched CYCLE — the parameter's spelling is the
		// same wire-compat debt AEP_TASK_ID carries. The version root lives in
		// `servers`, so the path is server-relative.
		"/executions/{executionId}/credentials/refresh",
		// The validation runner callback, grouped under the feature that owns it
		// and keyed by the cycle id the runner actually carries. It used to sit
		// under /executions/{executionId}, resolved against a table the milestone
		// supervisor never writes — so every validation runner was told its own
		// dispatch did not exist.
		"runner-validation-context",
		"/validation/{cycleId}/context",
		"taskJWT",
		"publisherCC",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("internal spec missing %q", want)
		}
	}

	// The runner skills-pull S2S endpoint is retired — the runner clones
	// `org-skills` and resolves applied skills locally. Its route/op must not
	// reappear in the internal surface.
	for _, gone := range []string{
		"runner-skills",
		"/internal/v1/executions/{executionId}/skills",
	} {
		if strings.Contains(yaml, gone) {
			t.Errorf("internal spec must not describe retired skills endpoint %q", gone)
		}
	}

	// The internal surface must NOT leak the public user-JWT scheme — each
	// surface declares only its own auth.
	if strings.Contains(yaml, "userJWT") {
		t.Error("internal spec must not declare userJWT")
	}
}
