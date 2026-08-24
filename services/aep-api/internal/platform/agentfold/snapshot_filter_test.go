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

package agentfold

import "testing"

// A user-uploaded reference is not an agent-authored spec artifact, so the
// extension allow-list is the wrong test for it: it admits a .md reference and
// drops a .txt or .csv one, which then sits in the snapshot with nothing
// putting it in front of the model. The folder decides.
//
// This mirrors the identical test in services/agents — the two filters are one
// rule implemented twice, and a change to either that isn't made to the other
// silently changes what a turn can read.
func TestKeepInTurnSnapshot_TextReferences(t *testing.T) {
	for _, ext := range []string{"md", "txt", "csv", "tsv", "json", "yaml", "yml", "xml", "html", "rst"} {
		p := "specs/requirements/references/brief." + ext
		if !KeepInTurnSnapshot(p) {
			t.Errorf("KeepInTurnSnapshot(%q) = false, want true — a .%s reference must be readable as text", p, ext)
		}
	}
	// Outside the references folder the old rules still hold — admitting these
	// globally would put arbitrary yaml and json into every turn.
	for _, p := range []string{"specs/design/workload.yaml", "specs/requirements/rows.csv"} {
		if KeepInTurnSnapshot(p) {
			t.Errorf("KeepInTurnSnapshot(%q) = true, want false", p)
		}
	}
}

// Natively-read binaries ride as file PARTS on the turn. Admitting one here
// would pour a PDF's bytes into the text map — the failure that channel exists
// to avoid.
func TestKeepInTurnSnapshot_BinaryReferencesStayOut(t *testing.T) {
	for _, ext := range []string{"pdf", "png", "jpg", "jpeg", "gif", "webp"} {
		p := "specs/requirements/references/doc." + ext
		if KeepInTurnSnapshot(p) {
			t.Errorf("KeepInTurnSnapshot(%q) = true, want false — .%s must ride as a file part, not as text", p, ext)
		}
	}
}
