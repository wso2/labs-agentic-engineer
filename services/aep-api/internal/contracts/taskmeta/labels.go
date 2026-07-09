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

package taskmeta

import "strings"

// The label vocabulary (§2). Labels carry flat, routable/filterable facts; the
// machine block (block.go) carries the structured ones. Labels split into five
// roles:
//
//   - marker    : LabelMarker — the router ignores unlabeled issues.
//   - class     : exactly one of LabelCoding/LabelOps — the sole routing
//     dimension (coding produces a PR; ops performs a platform operation).
//   - origin    : LabelOriginPrefix + Origin — filterable metadata, never routing.
//   - command   : LabelExecute (edge-triggered, consumed on receipt) and
//     LabelHold (level-triggered, stands while present). Human/external-written.
//   - projection: LabelStatusPrefix + DerivedStatus and LabelAttention —
//     platform-written mirrors of derived state, never read back as truth.
const (
	// LabelMarker makes an issue a Task; the funnel ignores issues without it.
	LabelMarker = "aep:task"

	// LabelCoding / LabelOps / LabelProvision are the executor-class labels
	// (exactly one). LabelProvision marks a provisioning gate issue
	// (dependency-management §3.6): config-collection / resource-provisioning /
	// org-publish work, driven by the drawer + readiness watcher, not the coding
	// agent.
	LabelCoding    = "aep:coding"
	LabelOps       = "aep:ops"
	LabelProvision = "aep:provision"

	// LabelOriginPrefix is prepended to an Origin to form its label.
	LabelOriginPrefix = "aep:origin/"

	// LabelExecute is the edge-triggered dispatch command, consumed by the
	// platform when acted on. LabelHold is the level-triggered pause, honored
	// while present.
	LabelExecute = "aep:execute"
	LabelHold    = "aep:hold"

	// LabelStatusPrefix is prepended to a DerivedStatus to form its projection
	// label. LabelAttention flags that something needs a human (mangled block,
	// stale-vs-design, refused op, no executor).
	LabelStatusPrefix = "aep:status/"
	LabelAttention    = "aep:attention"

	// LabelProvisionNoted is a platform-written marker stamped on an aep:provision
	// gate issue the first (and only) time a stray aep:execute is consumed on it,
	// so the funnel's one-time "use the design page's dependency panel" comment is
	// not re-posted on every repeated Execute stamp. It is deliberately NOT in the
	// Classify vocabulary (it classifies as KindOther) so the status-projection
	// reconciler never strips it.
	LabelProvisionNoted = "aep:provision-noted"
)

// ExecutorClass is the single routing dimension (§3): coding tasks are
// fulfilled by a coding agent and produce a pull request; ops tasks are
// fulfilled by a platform-operations executor.
type ExecutorClass string

const (
	ClassCoding ExecutorClass = "coding"
	ClassOps    ExecutorClass = "ops"
	// ClassProvision fulfils a dependency-provisioning gate (external value
	// collection, platform-resource provisioning, org-publish). It produces no
	// PR; it is driven by the drawer action + the resource-readiness watcher
	// (dependency-management §3.6), never dispatched from the aep:execute funnel.
	ClassProvision ExecutorClass = "provision"
)

// Valid reports whether c is a known executor class.
func (c ExecutorClass) Valid() bool {
	return c == ClassCoding || c == ClassOps || c == ClassProvision
}

// ClassLabel returns the routing label for an executor class ("aep:coding" /
// "aep:ops").
func ClassLabel(c ExecutorClass) string { return "aep:" + string(c) }

// OriginLabel returns the origin label for an Origin ("aep:origin/spec-plan").
func OriginLabel(o Origin) string { return LabelOriginPrefix + string(o) }

// StatusLabel returns the projection label for a derived status
// ("aep:status/ready_for_review"). Platform-written only.
func StatusLabel(s DerivedStatus) string { return LabelStatusPrefix + string(s) }

// NewTaskLabels is the label set stamped on a freshly created Task: marker,
// class, origin. Command and projection labels are added later by humans and
// the platform respectively.
func NewTaskLabels(c ExecutorClass, o Origin) []string {
	return []string{LabelMarker, ClassLabel(c), OriginLabel(o)}
}

// LabelKind categorizes a single label string by its role in the vocabulary.
type LabelKind int

const (
	// KindOther is any label not in the aep: vocabulary.
	KindOther LabelKind = iota
	KindMarker
	KindClass
	KindOrigin
	KindExecute
	KindHold
	KindStatus
	KindAttention
)

// Classify reports the role of a single label string.
func Classify(label string) LabelKind {
	switch {
	case label == LabelMarker:
		return KindMarker
	case label == LabelCoding || label == LabelOps || label == LabelProvision:
		return KindClass
	case label == LabelExecute:
		return KindExecute
	case label == LabelHold:
		return KindHold
	case label == LabelAttention:
		return KindAttention
	case strings.HasPrefix(label, LabelOriginPrefix):
		return KindOrigin
	case strings.HasPrefix(label, LabelStatusPrefix):
		return KindStatus
	default:
		return KindOther
	}
}

// ParsedLabels is the structured reading of an issue's whole label list — the
// class/origin/command/flag facts the funnel and read path need in one pass.
type ParsedLabels struct {
	IsTask         bool          // LabelMarker present
	Class          ExecutorClass // "" when unset
	ClassSet       bool
	ClassAmbiguous bool // both coding and ops present (invalid — flag attention)
	Origin         Origin
	OriginSet      bool
	Execute        bool // LabelExecute present (pending dispatch intent)
	Hold           bool // LabelHold present
	Attention      bool // LabelAttention present
	Status         DerivedStatus
	StatusSet      bool
}

// setClass records a class label, flagging ClassAmbiguous when a different
// class was already seen; the last one seen wins Class.
func (p *ParsedLabels) setClass(c ExecutorClass) {
	if p.ClassSet && p.Class != c {
		p.ClassAmbiguous = true
	}
	p.Class, p.ClassSet = c, true
}

// ParseLabels reads an issue's label list into ParsedLabels. Two class labels
// (mangled state) set ClassAmbiguous; the last one seen wins Class.
func ParseLabels(labels []string) ParsedLabels {
	var p ParsedLabels
	for _, l := range labels {
		switch {
		case l == LabelMarker:
			p.IsTask = true
		case l == LabelCoding:
			p.setClass(ClassCoding)
		case l == LabelOps:
			p.setClass(ClassOps)
		case l == LabelProvision:
			p.setClass(ClassProvision)
		case l == LabelExecute:
			p.Execute = true
		case l == LabelHold:
			p.Hold = true
		case l == LabelAttention:
			p.Attention = true
		case strings.HasPrefix(l, LabelOriginPrefix):
			p.Origin, p.OriginSet = Origin(strings.TrimPrefix(l, LabelOriginPrefix)), true
		case strings.HasPrefix(l, LabelStatusPrefix):
			p.Status, p.StatusSet = DerivedStatus(strings.TrimPrefix(l, LabelStatusPrefix)), true
		}
	}
	return p
}
