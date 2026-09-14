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
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// RunFailure is the platform's own record of WHY a run is failing — the facts
// that used to survive only as one log line the moment the supervisor settled
// the row (`workflow_dev.go` logged `gerr` and wrote `plan-failed`).
//
// It is the run's single failure record, not a history: one nullable jsonb
// column on the run row, written by the activity that hit the fault and
// overwritten on each attempt of that fault, so a reader sees "attempt 2 of
// 3" while the run is still `planning` and the terminal fault once it has
// settled. An activity that succeeds after a recorded fault clears it — a
// blip that healed is not a failure to report.
//
// TerminalReason is unchanged and stays the coarse key every predicate reads
// (`plan-failed`); this explains it. Code is a closed set (RunFailureCode*) —
// the console owns the sentence for each, the way RunEvent.notice codes work —
// and Detail is the platform's recorded words for a reader who wants them,
// never model output or a request body, scrubbed and capped at the producer.
type RunFailure struct {
	// Code names the failure class. One of the RunFailureCode* constants.
	Code string `json:"code"`
	// Phase is the run phase the fault was met in (RunPhase* values).
	Phase string `json:"phase"`
	// Component and Dependency name the subject when the fault has one — a
	// dependency the platform could not provision, and the component that
	// declared it. Empty for a fault about the run itself (a planning turn).
	Component  string `json:"component,omitempty"`
	Dependency string `json:"dependency,omitempty"`
	// Permanent says repeating cannot change the answer. It is the producer's
	// classification (ErrProvisionPermanent, sourcecontrol.IsPermanent), the same
	// one that decides the retry policy — so the sentence "retrying cannot fix
	// this" and the one attempt it took are two readings of one fact.
	Permanent bool `json:"permanent"`
	// Attempts is how many attempts have hit this fault so far; MaxAttempts is
	// the activity's bound, 0 for Temporal's unbounded default.
	Attempts    int `json:"attempts"`
	MaxAttempts int `json:"maxAttempts"`
	// FirstAt / LastAt bracket the attempts.
	FirstAt time.Time `json:"firstAt"`
	LastAt  time.Time `json:"lastAt"`
	// Detail is the platform's own error text, scrubbed (ScrubFailureDetail).
	Detail string `json:"detail,omitempty"`
}

// Failure codes. Each names exactly one class a reader acts on differently.
const (
	// RunFailureCodeDependencyUnprovisionable — a dependency the platform
	// cannot author however often it tries: a schema the ResourceType builder
	// refuses, a ClusterResourceType nobody installed, a Resource that never
	// cuts a release. The design has to change.
	RunFailureCodeDependencyUnprovisionable = "dependency-unprovisionable"
	// RunFailureCodeDependencyProvisionFailed — provisioning failed for a
	// reason the platform could not classify as permanent, and the bounded
	// retry (gateActivityAttempts) is being, or has been, spent on it.
	RunFailureCodeDependencyProvisionFailed = "dependency-provision-failed"
	// RunFailureCodePlanTurnFailed — the planning turn errored for a reason
	// repeating might change (an LLM or transport error); retried unbounded.
	RunFailureCodePlanTurnFailed = "plan-turn-failed"
	// RunFailureCodeRepositoryUnavailable — the run's repository, issue or
	// credential is gone (sourcecontrol.IsPermanent). Permanent.
	RunFailureCodeRepositoryUnavailable = "repository-unavailable"
)

// Value / Scan make RunFailure encode itself as jsonb, for the same reason
// DependencyNames does: the run repository writes through map updates, which
// hand the value straight to the driver.
func (f *RunFailure) Value() (driver.Value, error) {
	if f == nil {
		return nil, nil
	}
	return json.Marshal(f)
}

func (f *RunFailure) Scan(src any) error {
	if src == nil {
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("delivery: cannot scan %T into RunFailure", src)
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, f)
}

// failureDetailMax caps Detail. A provisioner's error is a sentence or three;
// anything longer is a stack of wrapped causes a reader does not need and a
// row should not carry.
const failureDetailMax = 2048

// The credential shapes that could ride an error message, each with the KEY
// kept so the line stays legible. URL userinfo goes first: a `token:` inside
// `https://x-access-token:<secret>@host` is a URL password, and the key/value
// pattern would otherwise swallow the host and path after it.
var (
	redactURLUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^/\s:@]+:)[^@/\s]+(@)`)
	redactAuthHeader  = regexp.MustCompile(`(?i)((?:authorization|x-api-key)\s*[:=]\s*(?:bearer\s+)?)[A-Za-z0-9._~+/=\-]{8,}`)
	redactKeyValue    = regexp.MustCompile(`(?i)\b((?:token|password|secret|api[_-]?key)\s*[:=]\s*)[^\s,;"'@/\[\]]{4,}`)
	redactGitHubToken = regexp.MustCompile(`gh[psour]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`)
)

// ScrubFailureDetail prepares an error's text for the run row: redacts the
// credential shapes above and caps the length. Producers call it; nothing
// downstream re-checks, so this is the one gate.
func ScrubFailureDetail(s string) string {
	out := redactURLUserinfo.ReplaceAllString(s, "${1}[REDACTED]${2}")
	out = redactAuthHeader.ReplaceAllString(out, "${1}[REDACTED]")
	out = redactKeyValue.ReplaceAllString(out, "${1}[REDACTED]")
	out = redactGitHubToken.ReplaceAllString(out, "[REDACTED]")
	if len(out) > failureDetailMax {
		out = out[:failureDetailMax] + "…"
	}
	return out
}
