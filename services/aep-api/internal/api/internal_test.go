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

// TestGenerateInternalOpenAPIYAML asserts the internal S2S spec registers
// cleanly (no duplicate-op panic) and describes exactly the runner-callback
// operations with the S2S security schemes — and only those.
func TestGenerateInternalOpenAPIYAML(t *testing.T) {
	out, err := GenerateInternalOpenAPIYAML()
	if err != nil {
		t.Fatalf("GenerateInternalOpenAPIYAML: %v", err)
	}
	yaml := string(out)

	for _, want := range []string{
		"runner-refresh-credentials",
		// Runner S2S re-keyed from task to execution (tasks-github-native §9.2).
		"/internal/v1/executions/{executionId}/credentials/refresh",
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
