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

package authz

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// chartRolesPath is the Helm template that installs the org's OpenChoreo
// AuthzRoles. Relative to this package.
const chartRolesPath = "../../../../deployments/helm-charts/platform/templates/authz/ae-roles.yaml"

// TestChartAuthzRolesMatchCatalog is the seam between two things that cannot
// import each other: the OC action catalog here, and the YAML that installs
// those actions into OpenChoreo.
//
// The roles used to be created at runtime by GET /authz/ensure, which resolved
// them FROM this catalog, so the two could not disagree. Installing them with
// the chart buys a role that exists before anyone signs in, and costs exactly
// that guarantee back — YAML cannot call ResolveOcPermissions.
//
// The drift would be silent and would not look like drift. A role missing an
// action is a 403 from OpenChoreo on a call the AE permission gate has already
// allowed, several systems from the list that caused it; an extra action is a
// grant nobody decided to make. So the comparison is EXACT in both directions.
func TestChartAuthzRolesMatchCatalog(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(chartRolesPath))
	if err != nil {
		t.Fatalf("read the chart's AuthzRole template: %v\n"+
			"If templates/authz/ae-roles.yaml moved, update chartRolesPath — do not delete this test.", err)
	}
	chart := parseChartRoleActions(t, string(raw))

	bridge := NewAuthZBridge(OcActionCatalog)
	for _, role := range Roles() {
		want := bridge.ResolveOcPermissions(RolePermissions(role))
		slices.Sort(want)

		got, ok := chart[role]
		if !ok {
			t.Errorf("the chart installs no AuthzRole %q — a role the catalog defines has nothing on the OpenChoreo side to honour it", role)
			continue
		}
		slices.Sort(got)

		if !slices.Equal(got, want) {
			t.Errorf("AuthzRole %q actions disagree with OcActionCatalog.\n  chart:   %v\n  catalog: %v\n"+
				"Update templates/authz/ae-roles.yaml to the catalog's list.", role, got, want)
		}
	}

	for role := range chart {
		if !slices.Contains(Roles(), role) {
			t.Errorf("the chart installs an AuthzRole %q that no AE role defines — it grants OC actions to a group nothing assigns", role)
		}
	}
}

// parseChartRoleActions pulls each role's action list out of the template.
//
// A line-scanner rather than a YAML parse: the file is a Helm template, so it
// is not valid YAML until rendered, and rendering it here would mean depending
// on the helm binary being installed to run `go test`. The template is written
// to stay scannable — one `{{- if eq $role "<name>" }}` branch per role, each
// holding a flat list of `- action` lines — and this fails loudly rather than
// silently reading zero actions if that stops being true.
func parseChartRoleActions(t *testing.T, tmpl string) map[string][]string {
	t.Helper()

	branch := regexp.MustCompile(`\{\{-?\s*if eq \$role "([a-z-]+)"`)
	action := regexp.MustCompile(`^\s*-\s+([a-z]+:[a-z-]+)\s*$`)

	out := map[string][]string{}
	current := ""
	// The else branch belongs to whichever role the if did not name; with two
	// roles that is unambiguous, and a third would need the template to say
	// which, so this refuses rather than guessing.
	roles := Roles()
	for _, line := range strings.Split(tmpl, "\n") {
		if m := branch.FindStringSubmatch(line); m != nil {
			current = m[1]
			out[current] = nil
			continue
		}
		if strings.Contains(line, "{{- else }}") && current != "" {
			if len(roles) != 2 {
				t.Fatalf("the template's else branch is only unambiguous with two roles, and there are %d — "+
					"give each role its own `if eq $role` branch", len(roles))
			}
			for _, r := range roles {
				if r != current {
					current = r
					break
				}
			}
			out[current] = nil
			continue
		}
		if strings.Contains(line, "{{- end }}") {
			current = ""
			continue
		}
		if m := action.FindStringSubmatch(line); m != nil && current != "" {
			out[current] = append(out[current], m[1])
		}
	}

	if len(out) == 0 {
		t.Fatal("no role action lists found in the chart template — its shape changed and this test is reading nothing")
	}
	return out
}
