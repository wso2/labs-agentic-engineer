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

package mcpdiscovery

// group_catalog_test.go — the `list_groups` tool: the design-time read of the
// directory groups an org already has.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakeGroupCatalog is a stub GroupCatalogLister. It records the org handle the
// handler passed down — proving the catalog is chosen by the verified context
// claim and never by a tool argument — and answers with canned rows or an error.
type fakeGroupCatalog struct {
	rows []GroupCatalogEntry
	err  error
	orgs []string
}

func (f *fakeGroupCatalog) ListGroupCatalog(_ context.Context, orgHandle string) ([]GroupCatalogEntry, error) {
	f.orgs = append(f.orgs, orgHandle)
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// groupCatalogHandler builds the MCP surface with only the catalog port that
// these cases exercise; the external-resource reader is required for the
// surface to answer at all.
func groupCatalogHandler(gc GroupCatalogLister) http.Handler {
	return NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, gc, nil,
		spec.ValidateOpenAPI, spec.NormalizeOpenAPIYAML, spec.FetchSpecFromURL, spec.SliceOpenAPI)
}

func TestMCP_ListGroups_Rows(t *testing.T) {
	rows := []GroupCatalogEntry{
		{Name: "Administrators", Description: "made by hand", PlatformCreated: false, MemberCount: 1},
		{Name: "Approver", Description: "ours, nobody in it yet", PlatformCreated: true},
		{Name: "Finance", PlatformCreated: false, MemberCount: 3, Projects: 2},
	}
	want := []struct {
		name            string
		platformCreated bool
		memberCount     float64
		projects        float64
	}{
		{name: "Administrators", platformCreated: false, memberCount: 1, projects: 0},
		{name: "Approver", platformCreated: true, memberCount: 0, projects: 0},
		{name: "Finance", platformCreated: false, memberCount: 3, projects: 2},
	}

	gc := &fakeGroupCatalog{rows: rows}
	resp := decodeRPC(t, postRPC(t, groupCatalogHandler(gc), "org-1", callBody("list_groups", `{}`)))
	text := toolText(t, resp, false)

	var payload map[string][]map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal list_groups payload: %v (%s)", err, text)
	}
	got, ok := payload["groups"]
	if !ok {
		t.Fatalf("list_groups payload has no %q key: %s", "groups", text)
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d: %s", len(got), len(want), text)
	}
	for i, w := range want {
		if got[i]["name"] != w.name {
			t.Errorf("rows[%d].name = %v, want %q", i, got[i]["name"], w.name)
		}
		if got[i]["platformCreated"] != w.platformCreated {
			t.Errorf("%s platformCreated = %v, want %v", w.name, got[i]["platformCreated"], w.platformCreated)
		}
		if got[i]["memberCount"] != w.memberCount {
			t.Errorf("%s memberCount = %v, want %v", w.name, got[i]["memberCount"], w.memberCount)
		}
		// The field must always be PRESENT, zero included, so a design agent is
		// never left guessing whether a missing key means "none" or "unknown".
		if got[i]["projects"] != w.projects {
			t.Errorf("%s projects = %v, want %v", w.name, got[i]["projects"], w.projects)
		}
	}
	if len(gc.orgs) != 1 || gc.orgs[0] != "org-1" {
		t.Errorf("orgs seen = %v, want exactly the context org [org-1]", gc.orgs)
	}
}

// A catalog that is not wired degrades to an empty list rather than an error:
// the surface stays usable for every other tool, which is the same rule the
// other optional ports follow.
func TestMCP_ListGroups_NotWiredIsEmpty(t *testing.T) {
	resp := decodeRPC(t, postRPC(t, groupCatalogHandler(nil), "org-1", callBody("list_groups", `{}`)))
	text := toolText(t, resp, false)
	var payload map[string][]map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, text)
	}
	if rows, ok := payload["groups"]; !ok || len(rows) != 0 {
		t.Fatalf("want an empty %q array, got %s", "groups", text)
	}
}

// A directory that cannot be read is a TOOL ERROR, never an empty list: an
// empty catalog reads as "no groups exist" and sends the design agent off to
// declare a duplicate of every group the org has.
func TestMCP_ListGroups_ReadFailureIsAToolError(t *testing.T) {
	gc := &fakeGroupCatalog{err: errors.New("thunder is down")}
	resp := decodeRPC(t, postRPC(t, groupCatalogHandler(gc), "org-1", callBody("list_groups", `{}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "thunder is down") {
		t.Errorf("tool error = %q, want the underlying cause", text)
	}
	if !strings.Contains(text, "list groups") {
		t.Errorf("tool error = %q, want it to name the tool that failed", text)
	}
}

// The description is the ONLY place a model learns what the catalog's fields
// mean, so a field the rows carry and the text never names is a field the model
// cannot use. `list_roles`, the deprecated alias this tool replaced, is gone:
// the old name must no longer be advertised.
func TestMCP_ListGroupsDescriptionNamesEveryField(t *testing.T) {
	byName := map[string]mcpTool{}
	for _, tool := range mcpTools() {
		byName[tool.Name] = tool
	}
	groups, ok := byName["list_groups"]
	if !ok {
		t.Fatal("list_groups is not advertised")
	}
	if groups.Description != listGroupsDescription {
		t.Errorf("list_groups description drifted from the shared text: %q", groups.Description)
	}
	if _, stillThere := byName["list_roles"]; stillThere {
		t.Error("list_roles is still advertised — the deprecated alias was removed in scopes phase 2")
	}
	for _, field := range []string{"assignTo", "groups[]", "memberCount", "projects", "platformCreated"} {
		if !strings.Contains(listGroupsDescription, field) {
			t.Errorf("the description never mentions %q — the model cannot use a field it is not told about", field)
		}
	}
}
