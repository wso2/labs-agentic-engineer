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

package projects

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// A binding whose Ready reason is OpenChoreo's ConnectionsPending is waiting on
// a sibling's binding, not on anything of its own. It stays PENDING — the
// deadline, not this read, decides when waiting has gone on too long — and it
// carries the fact the deadline needs to leave it out of what it files.
func TestComponentDeployFrom_ConnectionsPendingIsWaitingOnAProvider(t *testing.T) {
	t.Parallel()
	got := componentDeployFrom("skill-agent", "development", &openchoreo.ReleaseBindingSummary{
		ReadyStatus: "False", ReadyReason: "ConnectionsPending", ReleaseName: "rel-1",
	})
	if got.Ready || got.Failed {
		t.Fatalf("Ready=%v Failed=%v, want neither: a pending connection is a wait", got.Ready, got.Failed)
	}
	if !got.WaitingOnProvider {
		t.Error("WaitingOnProvider = false, want true for ConnectionsPending")
	}
	if got.Reason != "ConnectionsPending" {
		t.Errorf("Reason = %q, want the condition reason carried verbatim", got.Reason)
	}
}

// Every other Ready=False is the component's own: a rollout in progress, a
// terminal render failure, a platform-side hold.
func TestComponentDeployFrom_OtherReasonsAreNotWaitingOnAProvider(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"", "Progressing", "ResourceDependenciesPending", "RenderingFailed"} {
		got := componentDeployFrom("api", "development", &openchoreo.ReleaseBindingSummary{
			ReadyStatus: "False", ReadyReason: reason,
		})
		if got.WaitingOnProvider {
			t.Errorf("reason %q: WaitingOnProvider = true, want false", reason)
		}
	}
	if got := componentDeployFrom("api", "development", nil); got.WaitingOnProvider {
		t.Error("no binding yet: WaitingOnProvider = true, want false")
	}
}
