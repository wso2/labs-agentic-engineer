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

package openchoreo

import (
	"strings"
	"testing"
)

func podObject(phase, reason string, containerState map[string]interface{}) map[string]interface{} {
	status := map[string]interface{}{"phase": phase}
	if reason != "" {
		status["reason"] = reason
		status["message"] = "Pod was active on the node longer than the specified deadline"
	}
	if containerState != nil {
		status["containerStatuses"] = []interface{}{
			map[string]interface{}{"name": "agent", "state": containerState},
		}
	}
	return map[string]interface{}{"status": status}
}

func TestPodFromNodeObject_Running(t *testing.T) {
	got := PodFromNodeObject(podObject("Running", "", map[string]interface{}{
		"running": map[string]interface{}{"startedAt": "2026-08-06T10:00:00Z"},
	}), "ca-abc-2608061000-xyz")
	if !got.Found || got.Name != "ca-abc-2608061000-xyz" || got.Phase != "Running" {
		t.Fatalf("unexpected pod: %+v", got)
	}
	if got.WaitingReason != "" || got.TerminatedReason != "" {
		t.Errorf("a running pod carries no waiting/terminated reason: %+v", got)
	}
}

func TestPodFromNodeObject_WaitingReasonIsSurfaced(t *testing.T) {
	got := PodFromNodeObject(podObject("Pending", "", map[string]interface{}{
		"waiting": map[string]interface{}{
			"reason":  "CreateContainerConfigError",
			"message": `secret "ca-abc-anthropic" not found`,
		},
	}), "pod-1")
	if got.WaitingReason != "CreateContainerConfigError" {
		t.Fatalf("WaitingReason = %q, want CreateContainerConfigError", got.WaitingReason)
	}
	if got.Message != `secret "ca-abc-anthropic" not found` {
		t.Fatalf("Message = %q, want the container's waiting message", got.Message)
	}
}

// The pod-level status.reason is what Kubernetes stamps when
// activeDeadlineSeconds trips, and it must win over the container's generic
// "Error" — the two are the difference between "timed out" and "the agent
// exited non-zero", which the console reports differently.
func TestPodFromNodeObject_PodLevelReasonWinsOverContainerReason(t *testing.T) {
	got := PodFromNodeObject(podObject("Failed", "DeadlineExceeded", map[string]interface{}{
		"terminated": map[string]interface{}{"reason": "Error", "exitCode": float64(1)},
	}), "pod-2")
	if got.Phase != "Failed" || got.TerminatedReason != "DeadlineExceeded" {
		t.Fatalf("unexpected pod: %+v", got)
	}
}

func TestPodFromNodeObject_ContainerTerminatedReasonWhenPodHasNone(t *testing.T) {
	got := PodFromNodeObject(podObject("Failed", "", map[string]interface{}{
		"terminated": map[string]interface{}{"reason": "OOMKilled", "exitCode": float64(137)},
	}), "pod-3")
	if got.TerminatedReason != "OOMKilled" {
		t.Fatalf("TerminatedReason = %q, want OOMKilled", got.TerminatedReason)
	}
}

func TestPodFromNodeObject_EmptyObjectIsNotFound(t *testing.T) {
	if got := PodFromNodeObject(nil, ""); got.Found {
		t.Fatalf("a nil object must decode to Found=false, got %+v", got)
	}
}

// A Pending pod that never got a node has no containerStatuses — its failure
// lives on PodScheduled=False. Without this, the console narrates the generic
// "waiting for a runner" forever while the cluster is at pod capacity.
func TestPodFromNodeObject_UnschedulableConditionIsSurfaced(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Pending",
			"conditions": []interface{}{
				map[string]interface{}{
					"type":    "PodScheduled",
					"status":  "False",
					"reason":  "Unschedulable",
					"message": "0/5 nodes are available: 5 Too many pods.",
				},
			},
		},
	}
	got := PodFromNodeObject(obj, "pod-pending")
	if got.WaitingReason != "Unschedulable" {
		t.Fatalf("WaitingReason = %q, want Unschedulable", got.WaitingReason)
	}
	if !strings.Contains(got.Message, "Too many pods") {
		t.Fatalf("Message = %q, want the scheduler's capacity sentence", got.Message)
	}
}

func TestPodFromNodeObject_ContainerWaitingWinsOverScheduledCondition(t *testing.T) {
	obj := map[string]interface{}{
		"status": map[string]interface{}{
			"phase": "Pending",
			"conditions": []interface{}{
				map[string]interface{}{
					"type":   "PodScheduled",
					"status": "True",
				},
			},
			"containerStatuses": []interface{}{
				map[string]interface{}{
					"name": "agent",
					"state": map[string]interface{}{
						"waiting": map[string]interface{}{
							"reason":  "ContainerCreating",
							"message": "",
						},
					},
				},
			},
		},
	}
	got := PodFromNodeObject(obj, "pod-creating")
	if got.WaitingReason != "ContainerCreating" {
		t.Fatalf("WaitingReason = %q, want ContainerCreating", got.WaitingReason)
	}
}
