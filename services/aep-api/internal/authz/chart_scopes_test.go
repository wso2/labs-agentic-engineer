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
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// chartValuesPath is the platform chart's values file, which carries the OAuth
// scope string the console is deployed with. Relative to this package.
const chartValuesPath = "../../../../deployments/helm-charts/platform/values.yaml"

// TestChartConsoleScopesCoverCatalog guards the copy of the permission
// vocabulary that is deployment configuration rather than code: the scopes
// the console REQUESTS at sign-in, a space-delimited string in values.yaml
// handed to the console Deployment as VITE_THUNDER_SCOPES.
//
// Sibling of TestConsoleUnionMatchesCatalog, which guards the console's own
// list. Both copies exist for the same reason and are reconciled the same way;
// this one carries the worse failure of the two.
//
// Omitting a key there is the worst failure in the whole chain and the hardest
// to read. Thunder narrows a requested scope to what the caller's role holds
// but never grants what was not requested, so a permission missing from this
// string is unholdable by anyone — including ae-admin, whose role grants it,
// on a cluster where aectl declared the action correctly. Everything is
// configured right and the caller gets a 403 with nothing in any log naming
// the cause. Same hazard, same remedy, as TestChartAuthzRolesMatchCatalog
// beside it.
//
// Coverage rather than equality: the string legitimately carries the OIDC
// scopes (openid profile email) alongside the ae:* keys. Every ae:* entry is
// still checked in both directions — a stale one left behind after a rename
// is a scope Thunder has no action for, which is the same silent 403 arriving
// from the other side.
func TestChartConsoleScopesCoverCatalog(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(chartValuesPath))
	if err != nil {
		t.Fatalf("read the platform chart's values: %v\n"+
			"If values.yaml moved, update chartValuesPath — do not delete this test.", err)
	}

	var values struct {
		Console struct {
			Thunder struct {
				Scopes string `yaml:"scopes"`
			} `yaml:"thunder"`
		} `yaml:"console"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatalf("parse the platform chart's values: %v", err)
	}

	scopes := strings.Fields(values.Console.Thunder.Scopes)
	if len(scopes) == 0 {
		t.Fatal("console.thunder.scopes is empty or absent — either the chart stopped requesting " +
			"any scope (no caller could hold a permission) or this test is reading the wrong key")
	}

	var granted []string
	for _, s := range scopes {
		if strings.HasPrefix(s, "ae:") {
			granted = append(granted, s)
		}
	}

	for _, p := range AllPermissions {
		if !slices.Contains(granted, string(p)) {
			t.Errorf("console.thunder.scopes omits %q — the console never requests it, so Thunder "+
				"can never grant it and NO role, however privileged, can hold it.\n"+
				"Add it to console.thunder.scopes in %s.", p, chartValuesPath)
		}
	}

	for _, s := range granted {
		if !slices.Contains(AllPermissions, Permission(s)) {
			t.Errorf("console.thunder.scopes requests %q, which no AE permission defines — "+
				"Thunder has no action behind it, and the request either drops it or fails.\n"+
				"Remove it from console.thunder.scopes in %s.", s, chartValuesPath)
		}
	}
}
