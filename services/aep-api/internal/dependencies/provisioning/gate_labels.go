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

package provisioning

import (
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// A gate issue is PROSE plus LABELS. Its body is written for a human to read
// and nothing platform-side parses it, so the one structured fact the platform
// still needs from a gate — WHICH DEPENDENCY it holds — rides a label:
//
//	provision          the gate KIND (delivery.KindProvision)
//	aep:dep/<slug>     the dependency this gate is for
//
// That pair is the whole index. Both the mint-time dedupe ("does this dep
// already have an open gate?") and the drawer's resolve ("which issue do I
// close for this dep?") are label queries — never a body read, and never a
// title match, because a human may rewrite a title.
const gateDepLabelPrefix = "aep:dep/"

// aep:wired is the same trick applied to the OTHER side of a dependency: stamped
// on a WORKING-SET issue, it records that the platform has already posted a
// COMPLETE endpoint-wiring comment there (wiring.go). It is the comment's
// idempotency key — GitHub has no dedupe on comments the way CreateIssue has
// DedupeKey, and the poster runs on every cycle dispatch.
//
// "Complete" is load-bearing: the label is stamped only when nothing was omitted
// from the block. A comment that had to leave out a sibling endpoint that had not
// resolved yet goes up UNLABELLED, so the next dispatch posts the fuller version
// rather than treating a partial answer as final. That is the bug this whole
// design removes, in miniature.
//
// It carries no dependency slug: the block a comment carries is now the
// component's WHOLE endpoint set, resolved as a unit at dispatch, not one
// dependency's row posted when its gate happened to close.
const wiredLabel = "aep:wired"

// labelUnsafeRE collapses every run of characters GitHub label names handle
// poorly into a single hyphen. Dependency names come from the design and are
// already tame, but a label key must be total.
var labelUnsafeRE = regexp.MustCompile(`[^a-z0-9._-]+`)

// depSlug is the label-safe form of a dependency name, or "" when the name
// slugifies to nothing (the caller then emits no label at all).
func depSlug(depName string) string {
	slug := labelUnsafeRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(depName)), "-")
	return strings.Trim(slug, "-")
}

// gateDepLabel is the dependency label for a gate issue. An empty or
// unslugifiable name yields "" so callers can append it unconditionally.
func gateDepLabel(depName string) string {
	slug := depSlug(depName)
	if slug == "" {
		return ""
	}
	return gateDepLabelPrefix + slug
}

// gateVersionLabelPrefix records WHICH VERSION a gate belongs to.
//
// It is the second structured fact a gate carries, added for the same reason as
// the first: the platform needs it, and a body is not a place to keep something
// the platform reads. What needs it is the mint's own dedupe.
//
// A gate is deduped so a re-run does not file it twice, and the identity has to
// be (version, dependency) rather than just (dependency) — every version
// re-derives its own gates, so `orders-db` legitimately has one gate per version
// across a project's life. The DedupeKey has always said so
// (`gate:<project>:<tag>:<dep>`), but the STATE narrowing did not: the Go-side
// lookup and the host's key resolution both consider only OPEN issues. That is
// invisible until something closes a gate and then runs the mint again — which
// is exactly what a retried ProvisionGates does, because it settles the gates of
// already-ready dependencies itself. The result was a milestone filling with
// duplicate gates, three per attempt.
//
// Widening the lookup to every state is only safe with this label, and that is
// why it exists. Without it, "a gate for `orders-db` in any state" matches the
// CLOSED gate a previous version left behind, and v2's build would decline to
// mint its own gate at all.
//
// It is only for the gates that ARE per-version — a dependency's provision gate
// and the roles gate. The visibility and publish gates are keyed per project
// (see their DedupeKeys) and carry no version label, because for them one gate
// across every version is the correct identity.
const gateVersionLabelPrefix = "aep:gate-version/"

// gateVersionLabel is the version label for a gate, or "" when the tag
// slugifies to nothing — so a caller can append it unconditionally, exactly as
// gateDepLabel allows.
func gateVersionLabel(tag string) string {
	slug := depSlug(tag)
	if slug == "" {
		return ""
	}
	return gateVersionLabelPrefix + slug
}

// withGateVersion stamps a per-version gate's label set with the version it
// belongs to. A tag that slugifies to nothing leaves the set untouched, which
// degrades to the pre-label behaviour: the mint then dedupes on OPEN gates
// alone, as it always did.
func withGateVersion(labels []string, tag string) []string {
	if l := gateVersionLabel(tag); l != "" {
		return append(labels, l)
	}
	return labels
}

// gateIsForVersion reports whether a gate issue's labels say it belongs to this
// version. False for a gate minted before the label existed, which is the safe
// direction: such a gate suppresses a re-mint only while it is OPEN, exactly as
// it did before.
func gateIsForVersion(labels []string, tag string) bool {
	want := gateVersionLabel(tag)
	if want == "" {
		return false
	}
	for _, l := range labels {
		if strings.EqualFold(l, want) {
			return true
		}
	}
	return false
}

// PlatformGateLabelPrefix marks a gate the PLATFORM resolves itself, as opposed
// to one keyed to a design dependency. It is a DIFFERENT prefix from
// gateDepLabelPrefix on purpose: a platform gate must be unreachable from
// gateDepFromLabels, so no dependency name — however unlikely — can ever slug
// into the same label and make a dependency's gate and a platform gate
// indistinguishable.
const PlatformGateLabelPrefix = "aep:gate/"

// platformGateLabels is the label set a platform-resolved gate is minted with:
// the `provision` kind, so it holds the next dispatch exactly like a dependency
// gate, plus its own identity. No `aep` arming label — nothing may work it.
func platformGateLabels(gate string) []string {
	return []string{delivery.KindProvision, PlatformGateLabelPrefix + depSlug(gate)}
}

// gateLabels is the full label set a gate issue is minted with. A gate
// deliberately does NOT carry the `aep` ARMING label: it is never agent work,
// only a hold on the next dispatch. That absence is also what keeps the gate
// count independent of every working set — the counts query counts gates on
// their own alias precisely because they are not a subset of the armed
// population, so an open gate holds the next dispatch without ever subtracting
// from the work behind it.
func gateLabels(depName string) []string {
	labels := []string{delivery.KindProvision}
	if l := gateDepLabel(depName); l != "" {
		labels = append(labels, l)
	}
	return labels
}

// gateDepFromLabels reads a gate issue's dependency slug back out of its
// labels, or "" when it carries none (a hand-filed gate).
func gateDepFromLabels(labels []string) string {
	for _, l := range labels {
		if rest, ok := strings.CutPrefix(strings.ToLower(l), gateDepLabelPrefix); ok {
			return rest
		}
	}
	return ""
}
