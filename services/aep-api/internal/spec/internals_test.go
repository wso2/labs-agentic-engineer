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

import "testing"

// TestValidateReadPath_WorkloadEscapeHatch pins the second read escape hatch and,
// more importantly, its fence.
//
// A component's workload.yaml sits at its App Path, which is per-component design
// data rather than a literal this package can enumerate — so unlike the exact
// allow-list it has to be a SHAPE. A shape is not traversal-safe on its own,
// which is why the canonical/traversal checks run before it is consulted.
//
// The check that reads this file is wiring conformance: does what a component
// SHIPPED consume the resources its design declares? Fenced to the specs/ API it
// could never read the file at all, so the check was silently a no-op — and its
// own issue body says the failure "does not look broken": the app builds, deploys
// and serves, and the only symptom is a binding stuck Ready=False.
func TestValidateReadPath_WorkloadEscapeHatch(t *testing.T) {
	for _, c := range []struct {
		path string
		ok   bool
	}{
		{"todo-webapp/workload.yaml", true},
		{"workload.yaml", true}, // a component building from the repo root
		{"tests/validation/report.json", true},
		{"specs/design/domain-model.md", true},

		// The fence. A nested path would let the hatch walk the tree.
		{"a/b/workload.yaml", false},
		{"../workload.yaml", false},
		{"svc/../../workload.yaml", false},
		{"/etc/workload.yaml", false},
		{"./workload.yaml", false}, // non-canonical
		{"svc/workload.yaml.bak", false},
		{"svc/secrets.yaml", false},
		{"", false},
	} {
		err := validateReadPath(c.path)
		if c.ok && err != nil {
			t.Errorf("validateReadPath(%q) = %v, want allowed", c.path, err)
		}
		if !c.ok && err == nil {
			t.Errorf("validateReadPath(%q) = allowed, want refused", c.path)
		}
	}
}
