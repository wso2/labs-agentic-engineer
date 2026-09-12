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

// group_catalog_test.go — the `list_groups` tool and the `list_roles` alias it
// replaces. Both names are one handler for one phase, so every case here runs
// over both: the day they drift, the table is what catches it.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakeGroupCatalog is a stub RoleCatalogLister. It records the org handle the
// handler passed down — proving the catalog is chosen by the verified context
// claim and never by a tool argument — and answers with canned rows or an error.
type fakeGroupCatalog struct {
	rows []RoleCatalogEntry
	err  error
	orgs []string
}

func (f *fakeGroupCatalog) ListRoleCatalog(_ context.Context, orgHandle string) ([]RoleCatalogEntry, error) {
	f.orgs = append(f.orgs, orgHandle)
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// groupCatalogHandler builds the MCP surface with only the catalog port that
// these cases exercise; the external-resource reader is required for the
// surface to answer at all.
func groupCatalogHandler(gc RoleCatalogLister) http.Handler {
	return NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, gc, nil,
		spec.ValidateOpenAPI, spec.NormalizeOpenAPIYAML, spec.FetchSpecFromURL, spec.SliceOpenAPI)
}

// catalogToolNames is the pair under test: the tool and, for one phase, the
// deprecated name that dispatches to it. The result KEY differs, and only the
// key — a turn running an older skill revision must read exactly what it read
// before.
var catalogToolNames = []struct {
	tool string
	key  string
}{
	{tool: "list_groups", key: "groups"},
	{tool: "list_roles", key: "roles"},
}

func TestMCP_ListGroups_Rows(t *testing.T) {
	rows := []RoleCatalogEntry{
		{Name: "Administrators", Description: "made by hand", PlatformCreated: false, MemberCount: 1},
		{Name: "Approver", Description: "ours, nobody in it yet", PlatformCreated: true},
		{Name: "Finance", PlatformCreated: false, MemberCount: 3},
	}
	want := []struct {
		name            string
		platformCreated bool
		memberCount     float64
		projects        float64
	}{
		{name: "Administrators", platformCreated: false, memberCount: 1, projects: 0},
		{name: "Approver", platformCreated: true, memberCount: 0, projects: 0},
		{name: "Finance", platformCreated: false, memberCount: 3, projects: 0},
	}

	for _, tn := range catalogToolNames {
		t.Run(tn.tool, func(t *testing.T) {
			gc := &fakeGroupCatalog{rows: rows}
			resp := decodeRPC(t, postRPC(t, groupCatalogHandler(gc), "org-1", callBody(tn.tool, `{}`)))
			text := toolText(t, resp, false)

			var payload map[string][]map[string]any
			if err := json.Unmarshal([]byte(text), &payload); err != nil {
				t.Fatalf("unmarshal %s payload: %v (%s)", tn.tool, err, text)
			}
			got, ok := payload[tn.key]
			if !ok {
				t.Fatalf("%s payload has no %q key: %s", tn.tool, tn.key, text)
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
				// 0 until scopes phase 2 reads idp_role_bindings — and the field
				// must be PRESENT, so a design agent is never left guessing
				// whether a missing key means "none" or "unknown".
				if got[i]["projects"] != w.projects {
					t.Errorf("%s projects = %v, want %v", w.name, got[i]["projects"], w.projects)
				}
			}
			if len(gc.orgs) != 1 || gc.orgs[0] != "org-1" {
				t.Errorf("orgs seen = %v, want exactly the context org [org-1]", gc.orgs)
			}
		})
	}
}

// A catalog that is not wired degrades to an empty list rather than an error:
// the surface stays usable for every other tool, which is the same rule the
// other optional ports follow.
func TestMCP_ListGroups_NotWiredIsEmpty(t *testing.T) {
	for _, tn := range catalogToolNames {
		t.Run(tn.tool, func(t *testing.T) {
			resp := decodeRPC(t, postRPC(t, groupCatalogHandler(nil), "org-1", callBody(tn.tool, `{}`)))
			text := toolText(t, resp, false)
			var payload map[string][]map[string]any
			if err := json.Unmarshal([]byte(text), &payload); err != nil {
				t.Fatalf("unmarshal: %v (%s)", err, text)
			}
			if rows, ok := payload[tn.key]; !ok || len(rows) != 0 {
				t.Fatalf("want an empty %q array, got %s", tn.key, text)
			}
		})
	}
}

// A directory that cannot be read is a TOOL ERROR, never an empty list: an
// empty catalog reads as "no groups exist" and sends the design agent off to
// declare a duplicate of every group the org has.
func TestMCP_ListGroups_ReadFailureIsAToolError(t *testing.T) {
	for _, tn := range catalogToolNames {
		t.Run(tn.tool, func(t *testing.T) {
			gc := &fakeGroupCatalog{err: errors.New("thunder is down")}
			resp := decodeRPC(t, postRPC(t, groupCatalogHandler(gc), "org-1", callBody(tn.tool, `{}`)))
			text := toolText(t, resp, true)
			if !strings.Contains(text, "thunder is down") {
				t.Errorf("tool error = %q, want the underlying cause", text)
			}
			if !strings.Contains(text, strings.ReplaceAll(tn.tool, "_", " ")) {
				t.Errorf("tool error = %q, want it to name the tool that failed", text)
			}
		})
	}
}

// The alias exists so an older skill revision keeps working, and it must say so
// in the only place a model reads: its description. Both names otherwise carry
// the SAME text, from one constant.
func TestMCP_ListRolesIsADeprecatedAliasOfListGroups(t *testing.T) {
	byName := map[string]mcpTool{}
	for _, tool := range mcpTools() {
		byName[tool.Name] = tool
	}
	groups, ok := byName["list_groups"]
	if !ok {
		t.Fatal("list_groups is not advertised")
	}
	roles, ok := byName["list_roles"]
	if !ok {
		t.Fatal("list_roles must stay advertised for one phase")
	}
	if groups.Description != listGroupsDescription {
		t.Errorf("list_groups description drifted from the shared text: %q", groups.Description)
	}
	if roles.Description != "Deprecated: use list_groups. "+listGroupsDescription {
		t.Errorf("list_roles description = %q, want the deprecation prefix + the shared text", roles.Description)
	}
	for _, field := range []string{"assignTo", "groups[]", "memberCount", "projects", "platformCreated"} {
		if !strings.Contains(listGroupsDescription, field) {
			t.Errorf("the description never mentions %q — the model cannot use a field it is not told about", field)
		}
	}
}
