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

// "Have the requirements moved since the design was derived from them?" (#575)
//
// Answered by COMPARING, never by remembering. Every commit is a permanent,
// addressable snapshot, and every agent turn records the commit it read the
// project at — so the requirements as the last design run saw them are still
// there to be read. Nothing is stamped, so nothing can be stamped at the wrong
// moment or missed when a write fails, and the question is answerable for
// projects that predate the field entirely.
//
// The alternative — writing a fingerprint down when a design lands — does not
// fit this architecture: an agent turn never commits. Its file changes stream
// into the live collab document and the collab server commits them later on a
// debounce, and the request that finally lands carries no turn id and no
// author. There is no moment the platform controls, and no way to tell that
// flush from someone editing a document by hand.

package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// requirementsFingerprintPrefix is what counts as "the requirements" for
// staleness: EVERYTHING under the requirements directory, main document and
// nested feature files alike.
//
// Deliberately broader than requirementsBundleFilter, which admits only
// top-level files and answers a different question ("does a spec exist"). The
// depth is where the detail lives — the main document names its feature files
// and keeps their content out of line — so changes there are the ones most
// likely to invalidate a design. A rule with carve-outs is one a user cannot
// predict; this one states in a sentence: if anything in your requirements
// changed after the design was written, the design might be behind.
const requirementsFingerprintPrefix = requirementsPrefix

// referencesSubdir is the one exclusion. Reference documents are transient turn
// inputs overlaid into a turn's workspace and never committed (console
// ADR-0017) — they are the user's source material, not the requirements
// derived from it. They cannot appear here at all on a project created since
// that reversal; the exclusion covers the older projects whose documents really
// were committed, so a PDF that has sat unchanged for months is never mistaken
// for a requirement that moved.
const referencesSubdir = "references/"

// RequirementsFingerprint reduces a tree listing to one comparable value: the
// requirements files present, and the content of each.
//
// Built from PATH + BLOB SHA rather than from file contents. Git already
// content-addresses every blob, so two listings agree exactly when the files
// agree — no content is read, and the status poll's existing listing is enough
// to compute today's value for free.
//
// Order-independent by construction (sorted before hashing), because a tree
// listing's order is not part of what it means.
func RequirementsFingerprint(entries []sourcecontrol.Entry) string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.Path, requirementsFingerprintPrefix)
		if !ok || rel == "" || strings.HasPrefix(rel, referencesSubdir) {
			continue
		}
		lines = append(lines, rel+"\x00"+e.SHA)
	}
	// No requirements at all is its own value, distinct from "one empty file":
	// an empty hash input would make a project with no requirements compare
	// equal to one whose requirements were all deleted, which is a real
	// difference and exactly the kind a staleness check must not miss.
	if len(lines) == 0 {
		return ""
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}
