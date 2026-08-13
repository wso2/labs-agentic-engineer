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

// cycle_outcome.go — how a run cycle's Job is classified.
//
// The classification is a PURE function of the rendered Pod, and it is pure on
// purpose: this is the rule that decides whether a milestone run is told its
// agent died, so it has to be exercisable over every pod shape without a
// cluster. The one thing it never reads is the ReleaseBinding's Ready
// condition — OpenChoreo registers no health check for `batch/v1 Job`, so a
// binding reports success over a Job that is still running or has failed.

import (
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// CycleOutcome is what one tick concluded about a cycle's pod.
type CycleOutcome string

const (
	// OutcomePending covers every state on the way to Running, including no pod
	// at all and the Unknown phase — a node that stopped reporting is not a
	// verdict, and treating it as one would kill runs over kubelet hiccups.
	OutcomePending   CycleOutcome = "pending"
	OutcomeRunning   CycleOutcome = "running"
	OutcomeSucceeded CycleOutcome = "succeeded"
	OutcomeFailed    CycleOutcome = "failed"
)

// Terminal reasons written onto the cycle row.
const (
	// ReasonTimedOut is activeDeadlineSeconds tripping — a distinct answer from
	// a non-zero exit, because the user's next move differs.
	ReasonTimedOut = "timed_out"
	// ReasonJobNotFound is the sustained-404 verdict: the Component backing a
	// live cycle is gone and stayed gone across consecutive ticks.
	ReasonJobNotFound = "job_not_found"
)

// deadlineExceededReason is Kubernetes' own spelling on the pod's status.reason
// when activeDeadlineSeconds trips.
const deadlineExceededReason = "DeadlineExceeded"

// ClassifyPod maps a pod snapshot onto a cycle outcome.
func ClassifyPod(pod openchoreo.RuntimePod) CycleOutcome {
	if !pod.Found {
		return OutcomePending
	}
	switch pod.Phase {
	case "Succeeded":
		return OutcomeSucceeded
	case "Failed":
		return OutcomeFailed
	case "Running":
		return OutcomeRunning
	default:
		return OutcomePending
	}
}

// FailureReason names why a terminal-failed pod failed, in the vocabulary the
// cycle row stores and the console renders.
func FailureReason(pod openchoreo.RuntimePod) string {
	switch {
	case pod.TerminatedReason == deadlineExceededReason:
		return ReasonTimedOut
	case pod.TerminatedReason != "":
		return "agent_failed:" + pod.TerminatedReason
	default:
		return "agent_failed"
	}
}

// StartupFailureReason explains a pod that never reached Running before the
// watcher's startup grace expired. It prefers the pod's own waiting reason and
// falls back to the first Warning event, because a pod that was never created
// has no status to read and its events are the only account of why.
func StartupFailureReason(pod openchoreo.RuntimePod, events []openchoreo.RuntimeEvent) string {
	if pod.Found && pod.WaitingReason != "" {
		return "startup_failed:" + withMessage(pod.WaitingReason, pod.Message)
	}
	for _, e := range events {
		if e.Type == "Warning" && e.Reason != "" {
			return "startup_failed:" + withMessage(e.Reason, e.Message)
		}
	}
	if pod.Found {
		return "startup_failed:pod_not_running"
	}
	return "startup_failed:no_pod_scheduled"
}

// withMessage joins a reason with its human sentence, trimmed so a multi-line
// kubelet message cannot smear across the reason column.
func withMessage(reason, message string) string {
	message = strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
	if message == "" {
		return reason
	}
	return reason + ": " + message
}
