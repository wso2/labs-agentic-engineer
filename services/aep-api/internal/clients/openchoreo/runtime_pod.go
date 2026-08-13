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
	"time"
)

// RuntimePod is the BFF's view of the coding-agent Job's child Pod, decoded
// from the resource tree. It is deliberately NOT a k8s type: the tree hands us
// an untyped object map, and importing the Kubernetes API just to read four
// fields would put a k8s dependency back into a service that no longer has one.
//
// Found=false means the tree carried no Pod node at all — the Job exists but
// nothing has been scheduled yet, which is a normal early state and not an
// error.
type RuntimePod struct {
	Found bool
	Name  string
	// Phase is the Kubernetes pod phase verbatim: Pending, Running, Succeeded,
	// Failed or Unknown.
	Phase string
	// WaitingReason is the first waiting container's reason ("" once the
	// container runs): ImagePullBackOff, CreateContainerConfigError, …
	WaitingReason string
	// TerminatedReason is why the pod stopped. The POD-level status.reason wins
	// over the container's, because that is where Kubernetes stamps
	// DeadlineExceeded when activeDeadlineSeconds trips — and "timed out" and
	// "exited non-zero" are different answers for the user.
	TerminatedReason string
	// Message is the human sentence attached to whichever reason was taken.
	Message string
}

// PodLogLine is one line of pod stdout with the timestamp the platform recorded.
type PodLogLine struct {
	Timestamp time.Time
	Log       string
}

// RuntimeEvent is one Kubernetes event on the pod — the only place a pod that
// never started says why (image pull, scheduling, an unsynced secret).
type RuntimeEvent struct {
	Type          string // Normal | Warning
	Reason        string
	Message       string
	LastTimestamp time.Time
}

// PodFromNodeObject decodes a resource-tree Pod node's `object` map. Pure, so
// the classification rules above are testable without an OpenChoreo server.
func PodFromNodeObject(obj map[string]interface{}, name string) RuntimePod {
	if obj == nil {
		return RuntimePod{}
	}
	pod := RuntimePod{Found: true, Name: name}
	status, _ := obj["status"].(map[string]interface{})
	if status == nil {
		return pod
	}
	pod.Phase, _ = status["phase"].(string)
	if reason, _ := status["reason"].(string); reason != "" {
		pod.TerminatedReason = reason
		pod.Message, _ = status["message"].(string)
	}
	statuses, _ := status["containerStatuses"].([]interface{})
	for _, raw := range statuses {
		cs, _ := raw.(map[string]interface{})
		if cs == nil {
			continue
		}
		state, _ := cs["state"].(map[string]interface{})
		if state == nil {
			continue
		}
		if waiting, _ := state["waiting"].(map[string]interface{}); waiting != nil && pod.WaitingReason == "" {
			pod.WaitingReason, _ = waiting["reason"].(string)
			if msg, _ := waiting["message"].(string); msg != "" && pod.Message == "" {
				pod.Message = msg
			}
		}
		if term, _ := state["terminated"].(map[string]interface{}); term != nil && pod.TerminatedReason == "" {
			pod.TerminatedReason, _ = term["reason"].(string)
			if msg, _ := term["message"].(string); msg != "" && pod.Message == "" {
				pod.Message = msg
			}
		}
	}
	// A Pending pod with no containerStatuses yet (never scheduled) carries its
	// failure on PodScheduled=False — container waiting reasons never appear.
	// Surface Unschedulable / scheduling failures there so the console can
	// narrate capacity pressure instead of a generic "waiting for a runner".
	if pod.WaitingReason == "" {
		if reason, msg := podScheduledFailure(status); reason != "" {
			pod.WaitingReason = reason
			if msg != "" && pod.Message == "" {
				pod.Message = msg
			}
		}
	}
	return pod
}

// podScheduledFailure returns the reason/message from a PodScheduled=False
// condition (Unschedulable, SchedulerError, …). Empty when the pod is scheduled
// or the condition is absent.
func podScheduledFailure(status map[string]interface{}) (reason, message string) {
	conditions, _ := status["conditions"].([]interface{})
	for _, raw := range conditions {
		c, _ := raw.(map[string]interface{})
		if c == nil {
			continue
		}
		typ, _ := c["type"].(string)
		if typ != "PodScheduled" {
			continue
		}
		condStatus, _ := c["status"].(string)
		if !strings.EqualFold(condStatus, "False") {
			return "", ""
		}
		reason, _ = c["reason"].(string)
		message, _ = c["message"].(string)
		return reason, message
	}
	return "", ""
}
