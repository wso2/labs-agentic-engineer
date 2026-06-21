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

// Package arch holds the architecture-boundary invariant test
// (docs/design/asdlc-service-modularization.md §4/§6.3). It is the CI lock-in
// that keeps the feature import boundaries from regressing: there are no flat
// services/controllers packages, no feature imports another feature's concrete
// except along the allowed (acyclic) edges, and the cross-feature cycles
// (task↔codingagent, design↔task) stay broken through internal/contracts. Runs
// under plain `go test` (no extra tooling, so it works even where
// golangci-lint/depguard isn't installed).
package arch

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
)

const mod = "github.com/wso2/asdlc/asdlc-service"

// depCache memoizes each package's transitive import set so the boundary tests
// shell out to `go list -deps` once per distinct package (not once per
// assertion) — the same package is queried across multiple checks/tests.
var (
	depCacheMu sync.Mutex
	depCache   = map[string]map[string]bool{}
)

// deps returns the transitive import set of pkg (as a set) via `go list -deps`,
// memoized across all callers/tests.
func deps(t *testing.T, pkg string) map[string]bool {
	t.Helper()
	depCacheMu.Lock()
	defer depCacheMu.Unlock()
	if set, ok := depCache[pkg]; ok {
		return set
	}
	out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s failed: %v\n%s", pkg, err, out)
	}
	set := map[string]bool{}
	for _, d := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		set[d] = true
	}
	depCache[pkg] = set
	return set
}

func imports(t *testing.T, pkg, dep string) bool {
	return deps(t, pkg)[dep]
}

// TestNoFlatServicesOrControllers asserts there are no flat layers: nothing
// imports the flat services package, and nothing — not even the composition
// root or api routes — imports the controllers package. Features own their
// controllers behind task-local consumer ports (no cross-feature concrete
// import).
func TestNoFlatServicesOrControllers(t *testing.T) {
	features := []string{
		"artifacts", "gitrepo", "component", "orgcreds", "task", "codingagent",
		"requirements", "design", "project", "organization", "idp",
		"runtimeconfig", "skills", "webhook",
	}
	for _, f := range features {
		pkg := mod + "/internal/feature/" + f
		if imports(t, pkg, mod+"/services") {
			t.Errorf("%s imports the flat services package (should be gone)", f)
		}
		if imports(t, pkg, mod+"/controllers") {
			t.Errorf("%s imports the controllers package (forbidden — features own their controllers or use ports)", f)
		}
	}
	// The platform leaves must not import any feature or the flat layers.
	for _, p := range []string{"contracts", "platform/tenant", "platform/auth"} {
		pkg := mod + "/internal/" + p
		for d := range deps(t, pkg) {
			if strings.Contains(d, "/internal/feature/") {
				t.Errorf("%s imports a feature (%s) — must stay a leaf", p, d)
			}
			if d == mod+"/services" || d == mod+"/controllers" {
				t.Errorf("%s imports %s — must stay a leaf", p, d)
			}
		}
	}
	// The wiring layer holds no flat edges: the composition root and api
	// routes own no controllers/services import.
	for _, p := range []string{"/cmd/asdlc-api", "/api"} {
		pkg := mod + p
		if imports(t, pkg, mod+"/controllers") {
			t.Errorf("%s imports the controllers package — it is deleted; wire features directly", p)
		}
		if imports(t, pkg, mod+"/services") {
			t.Errorf("%s imports the flat services package — it is deleted", p)
		}
	}
}

// TestFlatPackagesDeleted asserts the flat services/ and controllers/ packages
// no longer exist on disk — the strongest form of the boundary (a re-created
// flat layer fails here even before anything imports it).
func TestFlatPackagesDeleted(t *testing.T) {
	for _, p := range []string{"/services", "/controllers"} {
		if err := exec.Command("go", "list", mod+p).Run(); err == nil {
			t.Errorf("package %s%s still resolves — the flat layer must stay deleted", mod, p)
		}
	}
}

// TestTaskCodingagentCycleBroken asserts the §4 bidirectional cycle stays cut:
// both directions route through internal/contracts, so neither concrete package
// imports the other.
func TestTaskCodingagentCycleBroken(t *testing.T) {
	if imports(t, mod+"/internal/feature/task", mod+"/internal/feature/codingagent") {
		t.Error("task imports codingagent — the build-dispatch/status cycle must route through contracts")
	}
	if imports(t, mod+"/internal/feature/codingagent", mod+"/internal/feature/task") {
		t.Error("codingagent imports task — the build-dispatch/status cycle must route through contracts")
	}
	// design must not import task or component concretely (its edges are ports
	// + contracts).
	if imports(t, mod+"/internal/feature/design", mod+"/internal/feature/task") {
		t.Error("design imports task — must use the contracts/port hook")
	}
	if imports(t, mod+"/internal/feature/design", mod+"/internal/feature/component") {
		t.Error("design imports component — must use the traitSync consumer port")
	}
}

// TestContractsIsLeaf asserts internal/contracts depends on nothing inside the
// module except models value types (the §4.0 / §6.3 admission rule).
func TestContractsIsLeaf(t *testing.T) {
	for d := range deps(t, mod+"/internal/contracts") {
		if !strings.HasPrefix(d, mod) {
			continue // stdlib / third-party
		}
		if d != mod+"/internal/contracts" && d != mod+"/models" {
			t.Errorf("contracts imports %s — only %s/models is allowed", d, mod)
		}
	}
}
