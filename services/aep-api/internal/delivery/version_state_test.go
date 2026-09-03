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

package delivery

import (
	"encoding/json"
	"strings"
	"testing"
)

// RunStatus is the QueryRunStatus payload, so its wire shape is a contract with
// the console. Version carries `omitzero` — the module's one use of it — and
// that option is the difference between a status that stays exactly as it was
// for every run that never deploys and one that grows an empty `version` object
// on all of them. A silently-ignored tag would look identical in the source, so
// it is asserted rather than assumed.
func TestRunStatusVersion_OmittedWhenZeroAndCarriedWhenRead(t *testing.T) {
	raw, err := json.Marshal(RunStatus{RunID: "run-1", State: RunStateWaiting})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "version") {
		t.Errorf("a run that has read no version state still serves a `version` key: %s", raw)
	}

	raw, err = json.Marshal(RunStatus{
		RunID: "run-1",
		State: RunStateRunning,
		Version: VersionState{Components: []ComponentState{{
			Component: "onboarding-api", State: ComponentStateBehind, DesiredSHA: "aaa1",
		}, {
			Component: "onboarding-webapp", State: ComponentStateHeld,
			WaitingOn: []string{"onboarding-api"},
		}}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back RunStatus
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Version.Components) != 2 {
		t.Fatalf("the version state did not survive the round trip: %s", raw)
	}
	if back.Version.Components[0].DesiredSHA != "aaa1" ||
		back.Version.Components[1].State != ComponentStateHeld ||
		len(back.Version.Components[1].WaitingOn) != 1 {
		t.Errorf("a component's state lost a field on the wire: %+v", back.Version.Components)
	}
	// The whole point of serving it: a reader can say what is not serving and
	// what each held component waits on, without asking the cluster.
	if got := back.Version.Describe(); got != "onboarding-api=behind, onboarding-webapp=held (waiting on onboarding-api)" {
		t.Errorf("Describe() = %q", got)
	}
}
