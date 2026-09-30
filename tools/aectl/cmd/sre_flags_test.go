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

package cmd

import (
	"strings"
	"testing"
)

// TestSreInstallFlags pins `aectl sre install`'s flag surface for the stock
// ghcr.io/openchoreo/sre-agent image: the patched-image/Anthropic-key flags
// are gone, --org is required, and the image/plane defaults track OC 1.3.0.
func TestSreInstallFlags(t *testing.T) {
	f := sreInstallCmd.Flags()
	for _, gone := range []string{"sre-llm-provider", "sre-llm-model", "sre-llm-api-key", "ae-auto-dispatch", "ae-publish-reports", "rca-model"} {
		if f.Lookup(gone) != nil {
			t.Errorf("flag --%s must be removed", gone)
		}
	}
	if f.Lookup("org") == nil {
		t.Fatal("--org is required")
	}
	if got := f.Lookup("rca-image-repo").DefValue; got != "ghcr.io/openchoreo/sre-agent" {
		t.Errorf("rca-image-repo default = %q", got)
	}
	if got := f.Lookup("rca-image-tag").DefValue; !strings.HasPrefix(got, "v1.3.0@sha256:") {
		t.Errorf("rca-image-tag default = %q, want digest-pinned v1.3.0", got)
	}
	if got := f.Lookup("obs-plane-version").DefValue; got != "1.3.0" {
		t.Errorf("obs-plane-version default = %q", got)
	}
}
