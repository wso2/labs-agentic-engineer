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

package spec

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func entry(path, sha string) sourcecontrol.Entry {
	return sourcecontrol.Entry{Path: path, SHA: sha}
}

// The whole staleness check is "did this value change", so what it must catch
// and what it must ignore are the entire specification.
func TestRequirementsFingerprint(t *testing.T) {
	t.Parallel()

	base := []sourcecontrol.Entry{
		entry("specs/requirements/prd.md", "aaa"),
		entry("specs/requirements/features/expenses.md", "bbb"),
	}

	t.Run("identical requirements agree", func(t *testing.T) {
		t.Parallel()
		if RequirementsFingerprint(base) != RequirementsFingerprint(base) {
			t.Fatal("the same requirements produced two different values")
		}
	})

	// The case the flag exists for.
	t.Run("an edited requirement changes it", func(t *testing.T) {
		t.Parallel()
		edited := []sourcecontrol.Entry{
			entry("specs/requirements/prd.md", "zzz"),
			entry("specs/requirements/features/expenses.md", "bbb"),
		}
		if RequirementsFingerprint(base) == RequirementsFingerprint(edited) {
			t.Fatal("a rewritten story left the fingerprint unchanged")
		}
	})

	// Depth is where the detail lives — the main document names its feature
	// files and keeps their content out of line, so a change there is among
	// the likeliest to invalidate a design. The "does a spec exist" predicate
	// looks only at top-level files; this one must not.
	t.Run("a nested feature file counts", func(t *testing.T) {
		t.Parallel()
		edited := []sourcecontrol.Entry{
			entry("specs/requirements/prd.md", "aaa"),
			entry("specs/requirements/features/expenses.md", "zzz"),
		}
		if RequirementsFingerprint(base) == RequirementsFingerprint(edited) {
			t.Fatal("an edited feature file left the fingerprint unchanged")
		}
	})

	t.Run("adding and removing a requirement both change it", func(t *testing.T) {
		t.Parallel()
		added := append(append([]sourcecontrol.Entry{}, base...),
			entry("specs/requirements/features/payroll.md", "ccc"))
		if RequirementsFingerprint(base) == RequirementsFingerprint(added) {
			t.Fatal("a new feature file left the fingerprint unchanged")
		}
		if RequirementsFingerprint(base) == RequirementsFingerprint(base[:1]) {
			t.Fatal("a deleted feature file left the fingerprint unchanged")
		}
	})

	// Everything outside requirements is somebody else's question. The design
	// changing must not read as the requirements moving, or the flag would
	// raise itself the moment it was cleared.
	t.Run("changes outside requirements are ignored", func(t *testing.T) {
		t.Parallel()
		withDesign := append(append([]sourcecontrol.Entry{}, base...),
			entry("specs/design/domain-model.md", "ddd"),
			entry("README.md", "eee"),
		)
		if RequirementsFingerprint(base) != RequirementsFingerprint(withDesign) {
			t.Fatal("a design change moved the requirements fingerprint")
		}
	})

	// Reference documents are the user's source material, overlaid into a
	// turn's workspace and never committed on any project created since that
	// reversal. Older projects really do carry them, and a PDF sitting
	// unchanged for months is not a requirement that moved.
	t.Run("committed reference documents are excluded", func(t *testing.T) {
		t.Parallel()
		withRefs := append(append([]sourcecontrol.Entry{}, base...),
			entry("specs/requirements/references/brief.pdf", "fff"))
		if RequirementsFingerprint(base) != RequirementsFingerprint(withRefs) {
			t.Fatal("a committed reference document moved the fingerprint")
		}
	})

	// A tree listing's order is not part of what it means.
	t.Run("listing order does not matter", func(t *testing.T) {
		t.Parallel()
		reversed := []sourcecontrol.Entry{base[1], base[0]}
		if RequirementsFingerprint(base) != RequirementsFingerprint(reversed) {
			t.Fatal("the same files in a different order disagreed")
		}
	})

	// A project with no requirements and one whose requirements were all
	// deleted are the same state, and both are distinct from having some.
	t.Run("no requirements is its own value", func(t *testing.T) {
		t.Parallel()
		if got := RequirementsFingerprint(nil); got != "" {
			t.Fatalf("empty requirements = %q, want the empty value", got)
		}
		if RequirementsFingerprint(base) == "" {
			t.Fatal("real requirements collided with the empty value")
		}
	})
}
