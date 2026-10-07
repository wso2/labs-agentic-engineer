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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
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
		// The validation runner callback, under the runner's runs/ group and
		// keyed by the cycle id the runner actually carries. It used to sit
		// under /executions/{executionId}, resolved against a table the milestone
		// supervisor never writes — so every validation runner was told its own
		// dispatch did not exist.
		"runner-validation-context",
		"/runs/{cycleId}/validation-context",
		"publisherCC",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("internal spec missing %q", want)
		}
	}

	// The runner skills-pull S2S endpoint is retired — the runner clones
	// `org-skills` and resolves applied skills locally. Its route/op must not
	// reappear in the internal route group.
	for _, gone := range []string{
		"runner-skills",
		"runner-refresh-credentials",
		"credentials/refresh",
		"/internal/v1/executions/{executionId}/skills",
	} {
		if strings.Contains(yaml, gone) {
			t.Errorf("internal spec must not describe retired skills endpoint %q", gone)
		}
	}

	// The internal route group must NOT leak the public user-JWT scheme — each
	// route group declares only its own auth.
	if strings.Contains(yaml, "userJWT") {
		t.Error("internal spec must not declare userJWT")
	}
}

// sourcecontrol owns the attentionReason closed set the IssueInfo projection
// filters through; it must be exactly the contract's enum.
func TestAttentionReasonSetMatchesContracts(t *testing.T) {
	for _, v := range []string{"unverified_fix", "no_change_verdict", "escalated", "bogus", ""} {
		inSet := sourcecontrol.IsContractAttentionReason(v)
		if pub := gen.IssueInfoAttentionReason(v).Valid(); pub != inSet {
			t.Errorf("%q: sourcecontrol set %v, public enum %v", v, inSet, pub)
		}
	}
}
