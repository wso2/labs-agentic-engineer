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

package codingagent

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

func TestClassifyPod(t *testing.T) {
	cases := []struct {
		name string
		pod  openchoreo.RuntimePod
		want CycleOutcome
	}{
		{"no pod yet", openchoreo.RuntimePod{}, OutcomePending},
		{"pending", openchoreo.RuntimePod{Found: true, Phase: "Pending"}, OutcomePending},
		{"running", openchoreo.RuntimePod{Found: true, Phase: "Running"}, OutcomeRunning},
		{"succeeded", openchoreo.RuntimePod{Found: true, Phase: "Succeeded"}, OutcomeSucceeded},
		{"failed", openchoreo.RuntimePod{Found: true, Phase: "Failed"}, OutcomeFailed},
		// An Unknown phase is a node that stopped reporting, not a verdict. It
		// must never end a cycle: the run would be marked dead over a kubelet
		// hiccup the platform routinely recovers from.
		{"unknown", openchoreo.RuntimePod{Found: true, Phase: "Unknown"}, OutcomePending},
		{"empty phase", openchoreo.RuntimePod{Found: true}, OutcomePending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyPod(tc.pod); got != tc.want {
				t.Fatalf("ClassifyPod(%+v) = %q, want %q", tc.pod, got, tc.want)
			}
		})
	}
}

func TestFailureReason_DeadlineExceededIsTimedOut(t *testing.T) {
	got := FailureReason(openchoreo.RuntimePod{
		Found: true, Phase: "Failed", TerminatedReason: "DeadlineExceeded",
		Message: "Pod was active on the node longer than the specified deadline",
	})
	if got != ReasonTimedOut {
		t.Fatalf("FailureReason = %q, want %q", got, ReasonTimedOut)
	}
}

func TestFailureReason_NamesTheTerminationReason(t *testing.T) {
	got := FailureReason(openchoreo.RuntimePod{Found: true, Phase: "Failed", TerminatedReason: "OOMKilled"})
	if got != "agent_failed:OOMKilled" {
		t.Fatalf("FailureReason = %q, want agent_failed:OOMKilled", got)
	}
}

func TestFailureReason_FallsBackWhenNothingSaysWhy(t *testing.T) {
	if got := FailureReason(openchoreo.RuntimePod{Found: true, Phase: "Failed"}); got != "agent_failed" {
		t.Fatalf("FailureReason = %q, want agent_failed", got)
	}
}

// The startup-grace reason exists so ImagePullBackOff, Unschedulable and an
// unsynced secret are three different answers instead of one "timed out".
func TestStartupFailureReason_NamesTheWaitingReasonAndItsMessage(t *testing.T) {
	got := StartupFailureReason(
		openchoreo.RuntimePod{
			Found: true, Phase: "Pending",
			WaitingReason: "CreateContainerConfigError",
			Message:       `secret "ca-abc-anthropic" not found`,
		},
		nil,
	)
	want := `startup_failed:CreateContainerConfigError: secret "ca-abc-anthropic" not found`
	if got != want {
		t.Fatalf("StartupFailureReason = %q, want %q", got, want)
	}
}

func TestStartupFailureReason_UsesTheWarningEventWhenThePodIsSilent(t *testing.T) {
	got := StartupFailureReason(
		openchoreo.RuntimePod{},
		[]openchoreo.RuntimeEvent{
			{Type: "Normal", Reason: "Scheduled", Message: "assigned"},
			{Type: "Warning", Reason: "FailedScheduling", Message: "0/3 nodes are available"},
		},
	)
	want := "startup_failed:FailedScheduling: 0/3 nodes are available"
	if got != want {
		t.Fatalf("StartupFailureReason = %q, want %q", got, want)
	}
}

func TestStartupFailureReason_FallsBackWhenNothingExplainsIt(t *testing.T) {
	if got := StartupFailureReason(openchoreo.RuntimePod{}, nil); got != "startup_failed:no_pod_scheduled" {
		t.Fatalf("StartupFailureReason = %q, want startup_failed:no_pod_scheduled", got)
	}
}
