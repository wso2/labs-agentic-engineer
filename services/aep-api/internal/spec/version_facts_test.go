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

func blob(path, sha string) sourcecontrol.Entry {
	return sourcecontrol.Entry{Path: path, SHA: sha}
}

func rows(t *testing.T, got []VersionChange) map[string]VersionChange {
	t.Helper()
	out := make(map[string]VersionChange, len(got))
	for _, r := range got {
		out[r.Name] = r
	}
	return out
}

func TestVersionChangesNamesOwnersNotFiles(t *testing.T) {
	before := []sourcecontrol.Entry{
		blob("specs/requirements/prd.md", "r1"),
		blob("specs/design/components/orders-api/design.json", "c1"),
		blob("specs/design/components/orders-api/openapi.yaml", "c2"),
		blob("specs/design/dependencies/legacy-mailer/dependency.json", "d1"),
	}
	after := []sourcecontrol.Entry{
		blob("specs/requirements/prd.md", "r1"),
		// One of the component's two files moved: the COMPONENT changed once,
		// not one row per file.
		blob("specs/design/components/orders-api/design.json", "c1"),
		blob("specs/design/components/orders-api/openapi.yaml", "c2-new"),
		blob("specs/design/components/reports-web/design.json", "c3"),
	}

	got := rows(t, versionChanges(before, after, nil, nil))

	if len(got) != 3 {
		t.Fatalf("changes = %+v, want three rows (orders-api, reports-web, legacy-mailer)", got)
	}
	if r := got["orders-api"]; r.State != VersionChangeChanged || r.Kind != VersionChangeKindComponent {
		t.Errorf("orders-api = %+v, want a changed component", r)
	}
	if r := got["reports-web"]; r.State != VersionChangeNew {
		t.Errorf("reports-web = %+v, want new", r)
	}
	if r := got["legacy-mailer"]; r.State != VersionChangeRemoved || r.Kind != VersionChangeKindExternal {
		t.Errorf("legacy-mailer = %+v, want a removed external dependency", r)
	}
}

// Every row names something that EXISTS once the version is built. The
// requirements are the input to that, and they move on nearly every version,
// so they are not a row at all.
func TestVersionChangesNeverNameTheRequirements(t *testing.T) {
	before := []sourcecontrol.Entry{blob("specs/requirements/prd.md", "r1")}
	after := []sourcecontrol.Entry{
		blob("specs/requirements/prd.md", "r2"),
		blob("specs/requirements/user-stories.md", "s1"),
	}

	if got := versionChanges(before, after, nil, nil); len(got) != 0 {
		t.Fatalf("changes = %+v, want none — the requirements are not a row", got)
	}
}

func TestVersionChangesOnAFirstBuildAreAllNew(t *testing.T) {
	after := []sourcecontrol.Entry{
		blob("specs/requirements/prd.md", "r1"),
		blob("specs/design/components/orders-api/design.json", "c1"),
	}

	got := versionChanges(nil, after, nil, map[string]bool{"postgres-cnpg": true})

	if len(got) != 2 {
		t.Fatalf("changes = %+v, want the component and the resource", got)
	}
	for _, r := range got {
		if r.State != VersionChangeNew {
			t.Errorf("%s = %q, want every row new on a first build", r.Name, r.State)
		}
	}
	// Grouped by kind, so the dialog can render its headings straight off the
	// order it is given.
	if got[0].Kind != VersionChangeKindComponent || got[1].Kind != VersionChangeKindResource {
		t.Errorf("order = %+v, want the component before the resource", got)
	}
}

// A platform resource owns no directory, so the tree cannot see it arrive —
// only the design can.
func TestVersionChangesReadPlatformResourcesFromTheDesign(t *testing.T) {
	tree := []sourcecontrol.Entry{blob("specs/design/components/orders-api/design.json", "c1")}

	got := rows(t, versionChanges(tree, tree,
		map[string]bool{"redis": true},
		map[string]bool{"postgres-cnpg": true}))

	if r := got["postgres-cnpg"]; r.State != VersionChangeNew || r.Kind != VersionChangeKindResource {
		t.Errorf("postgres-cnpg = %+v, want a new platform resource", r)
	}
	if r := got["redis"]; r.State != VersionChangeRemoved {
		t.Errorf("redis = %+v, want removed", r)
	}
}

func TestVersionChangesIgnoreEverythingOutsideSpecs(t *testing.T) {
	before := []sourcecontrol.Entry{blob("README.md", "a")}
	after := []sourcecontrol.Entry{blob("README.md", "b"), blob("src/main.go", "c")}

	if got := versionChanges(before, after, nil, nil); len(got) != 0 {
		t.Fatalf("changes = %+v, want none — a version is the specs/ tree", got)
	}
}
