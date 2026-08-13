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
	"fmt"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
)

// unixMilliDigits is the width of the timestamp NewBuildRunName appends. Thirteen
// digits covers every instant from 2001 to 2286.
const unixMilliDigits = 13

// buildRunNameWidths cap the readable head. See delivery's equivalents: both
// halves are recoverable in full from the run's own labels, so the cap costs
// readability and nothing else.
const (
	runNameProjectWidth   = 18
	runNameComponentWidth = 18
)

// NewBuildRunName produces the WorkflowRun metadata.name for a new build of
// (projectID, componentName). Stable shape so the BFF can pre-compute the
// name and stage the per-WorkflowRun build Secret (named
// `<runName>-git-secret`) before POSTing the WorkflowRun — see
// docs/design/build-credential-injection.md. The millisecond timestamp
// keeps successive triggers unique.
//
// Composed through k8sname.Bounded against MaxLabelValueLen, for the reason
// given there: the binding budget on a run name is the 63-char label-value
// limit, not the 253-char name limit, and a name over it is accepted by
// OpenChoreo and then never builds. Formatting the parts directly used to leave
// this path with ZERO headroom (a 32-char project and a 16-char component land
// on exactly 63), so any slightly longer name would have broken the console's
// Build button the same silent way.
func NewBuildRunName(projectName, componentName string) string {
	head := k8sname.Bounded(k8sname.MaxLabelValueLen-1-unixMilliDigits,
		k8sname.Capped(projectName, runNameProjectWidth),
		k8sname.Capped(componentName, runNameComponentWidth),
	)
	return fmt.Sprintf("%s-%d", head, time.Now().UnixMilli())
}

// ocJobNameHashLen is the width of the hash OpenChoreo appends when it names a
// dataplane Job from a ComponentReleaseBinding:
// `{component.metadata.name}-{environment}-{hash8}`. That string is copied into
// the Job's pod-template labels, so it is bound by MaxLabelValueLen — not by
// the 253-char resource-name limit.
const ocJobNameHashLen = 8

// CodingAgentComponentNameBudget is how long a coding-agent Component's
// metadata.name (the SCOPED form CreateComponent writes) may be. OpenChoreo
// then appends `-{DevEnvironmentName}-{hash8}` for the Job / pod-selector
// label; overflowing that composed string is accepted at Component create and
// then fails ResourceApplyFailed with no runner pod — the console's
// "Waiting for a runner to be scheduled…" dark zone.
//
// Coding-agent cycles always bind into DevEnvironmentName, so the decoration
// width is fixed here rather than parameterised.
const CodingAgentComponentNameBudget = k8sname.MaxLabelValueLen - (1 + len(DevEnvironmentName) + 1 + ocJobNameHashLen) // 42

// minCodingAgentRunNameLen is "ca-" + an 8-char digest — the shortest Bounded
// output that still carries the ca- watcher discriminator.
const minCodingAgentRunNameLen = 3 + ocJobNameHashLen // "ca-" + digest

// NewCodingAgentRunName produces the FRIENDLY coding-agent Component name
// (`ca-…`) for one cycle. CreateComponent scopes it with the project; the
// SCOPED name is composed to fit CodingAgentComponentNameBudget so OC's Job
// decoration still clears MaxLabelValueLen.
//
// Stable across retries: Bounded digests the untruncated (project, cycle)
// identity, so a Temporal retry after a crash recreates the same name and
// CreateComponent's 409 path re-reads instead of minting a second billed
// Component. Callers that need the ca- prefix for watcher discrimination get
// it by construction whenever the project leaves room; an overlong project
// is refused at CreateComponent (see the coding-agent name guard there).
func NewCodingAgentRunName(projectName, cycleID string) string {
	// Length must match ScopedComponentName(projectName, runName) byte-for-byte —
	// CreateComponent scopes with the raw project id, not a re-sanitized form.
	room := CodingAgentComponentNameBudget - len(projectName) - 1
	if room < minCodingAgentRunNameLen {
		// Still emit a ca-… JobRef so watchers recognise it; CreateComponent
		// refuses before OC can accept a Component whose Job label cannot
		// render.
		room = minCodingAgentRunNameLen
	}
	clean := strings.ReplaceAll(cycleID, "-", "")
	return k8sname.Bounded(room,
		k8sname.Whole("ca"),
		k8sname.Capped(clean, 20),
	)
}

// ScopedComponentName is the k8s metadata name OC uses for a component.
// Delegates to ocname.ScopedComponentName — the shared source of truth, because
// spec stamps this same name into design.json at design save (see
// spec/derive_wiring.go) and a byte of drift there costs a consumer's
// ReleaseBinding its Ready condition.
//
// Callers must always pass the friendly component name (never a previously
// scoped name) — scope exactly once.
func ScopedComponentName(projectName, componentName string) string {
	return ocname.ScopedComponentName(projectName, componentName)
}

// FriendlyComponentName reverses ScopedComponentName using the owner project
// recorded on the OC component. Safe on legacy rows that were never prefixed.
func FriendlyComponentName(k8sName, projectName string) string {
	if projectName == "" {
		return k8sName
	}
	return strings.TrimPrefix(k8sName, projectName+"-")
}
