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

// Package arch holds the architecture-boundary invariant tests — the CI lock
// that keeps the vertical-slice layout from regressing. The invariants:
//
//   - no flat services/ or controllers/ layer exists or is imported;
//   - every internal/platform/* package and internal/contracts is
//     domain-free (componenttest is the one deliberate exception — it
//     assembles the real app);
//   - contracts imports nothing module-internal at all;
//   - all Go code lives under internal/ except cmd/ (mains). The flat models/ +
//     repositories/ kernels are DISSOLVED, and internal/feature/ is GONE — every
//     entity/repository/feature now lives in its owning domain (the
//     domain-boundary rules are in domain_arch_test.go).
//
// The platform package list is discovered from disk (os.ReadDir), so a new
// package is policed the moment it exists. Runs under plain `go test` — no
// extra tooling.
package arch

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

const mod = "github.com/wso2/aep/aep-api"

// depCache memoizes each package's transitive import set so the boundary
// tests shell out to `go list -deps` once per distinct package.
var (
	depCacheMu sync.Mutex
	depCache   = map[string]map[string]bool{}
)

// deps returns the transitive import set of pkg via `go list -deps`,
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

// directImports returns pkg's DIRECT imports (not transitive) — the right
// granularity for the edge allowlist.
func directImports(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -f imports %s failed: %v\n%s", pkg, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func imports(t *testing.T, pkg, dep string) bool {
	return deps(t, pkg)[dep]
}

// listDir returns the package directory names under the given path relative
// to this test file — the on-disk discovery that keeps every check current
// without a hardcoded slice.
func listDir(t *testing.T, rel string) []string {
	t.Helper()
	entries, err := os.ReadDir(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestNoFlatServicesOrControllers asserts there are no flat layers: no
// feature, platform leaf, or wiring package imports the deleted services/ or
// controllers/ packages.
func TestNoFlatServicesOrControllers(t *testing.T) {
	// internal/feature/ is GONE (every feature migrated into a domain), so the
	// only remaining risk is a composition-root regression re-importing the
	// deleted flat services/controllers packages.
	for _, p := range []string{"/internal/app", "/cmd/aep-api", "/internal/edge"} {
		pkg := mod + p
		if imports(t, pkg, mod+"/controllers") {
			t.Errorf("%s imports the controllers package — it is deleted; wire features directly", p)
		}
		if imports(t, pkg, mod+"/services") {
			t.Errorf("%s imports the flat services package — it is deleted", p)
		}
	}
}

// TestFlatPackagesDeleted asserts the flat services/ and controllers/
// packages no longer exist on disk — the strongest form of the boundary (a
// re-created flat layer fails here even before anything imports it).
func TestFlatPackagesDeleted(t *testing.T) {
	for _, p := range []string{"/services", "/controllers"} {
		if err := exec.Command("go", "list", mod+p).Run(); err == nil {
			t.Errorf("package %s%s still resolves — the flat layer must stay deleted", mod, p)
		}
	}
}

// TestTaskExecutionSplit asserts the §1 Task/Execution split is a package
// boundary (docs/design/tasks-github-native.md §10): delivery/task (the
// GitHub-facing half) and delivery/execution (the platform-owned half) never
// import each other — they communicate only through the pure taskmeta encoding
// and the executions rows (the shared kernel). task reaches the funnel through
// the task.Dispatcher consumer port; execution never needs task at all. Now that
// both are delivery-domain sub-packages this is also enforced by slice⊥sibling,
// but it is stated explicitly because the split is the design's load-bearing
// invariant (§10.3.1).
func TestTaskExecutionSplit(t *testing.T) {
	const task = mod + "/internal/delivery/task"
	const execution = mod + "/internal/delivery/execution"
	if imports(t, task, execution) {
		t.Error("delivery/task imports delivery/execution — the Task/Execution split is a package boundary; reach the funnel through the task.Dispatcher port")
	}
	if imports(t, execution, task) {
		t.Error("delivery/execution imports delivery/task — the Task/Execution split is a package boundary")
	}
}

// TestPlatformAndContractsAreFeatureFree asserts every internal/platform/*
// package and internal/contracts imports no feature (and none of the flat
// layers). componenttest is the one deliberate exception: it assembles the
// REAL app graph for the component tier, so it imports everything by design.
func TestPlatformAndContractsAreFeatureFree(t *testing.T) {
	pkgs := []string{mod + "/internal/contracts"}
	for _, p := range listDir(t, "../platform") {
		if p == "componenttest" {
			continue // assembles the real app — feature imports are its job
		}
		pkgs = append(pkgs, mod+"/internal/platform/"+p)
	}
	for _, pkg := range pkgs {
		for d := range deps(t, pkg) {
			if strings.Contains(d, "/internal/feature/") {
				t.Errorf("%s imports a feature (%s) — must stay feature-free", pkg, d)
			}
			if d == mod+"/services" || d == mod+"/controllers" {
				t.Errorf("%s imports %s — must stay feature-free", pkg, d)
			}
		}
	}
}

// TestContractsIsLeaf asserts internal/contracts depends on NOTHING inside
// the module: it is the cycle-breaking leaf, and the dependency direction is
// models → contracts (models re-exports contracts types), never the reverse.
func TestContractsIsLeaf(t *testing.T) {
	for d := range deps(t, mod+"/internal/contracts") {
		if !strings.HasPrefix(d, mod) {
			continue // stdlib / third-party
		}
		if d != mod+"/internal/contracts" {
			t.Errorf("contracts imports %s — contracts must import nothing module-internal", d)
		}
	}
}

// TestTaskmetaIsPure asserts internal/contracts/taskmeta is a pure domain leaf
// (docs/design/tasks-github-native.md §10): the machine-block codec, label
// vocabulary, and derived-status algebra that both halves of the Task/Execution
// split import. Modeled on TestContractsIsLeaf but for the subpackage:
//
//   - it imports NOTHING module-internal (features import taskmeta, never the
//     reverse — the encoding is shared truth, re-implemented nowhere);
//   - no ORM or network stack anywhere in its transitive closure (gorm / net/http);
//   - no direct filesystem/process import (os).
//
// The os / gorm / net-http bans are meaningful here because taskmeta performs
// no IO. os is checked at the DIRECT-import granularity, not transitively: fmt
// and crypto/sha256 both pull os into any package's closure, so a transitive os
// ban is impossible — the intent is "taskmeta never touches the filesystem
// itself", which a direct-import check captures exactly.
func TestTaskmetaIsPure(t *testing.T) {
	const pkg = mod + "/internal/contracts/taskmeta"
	for d := range deps(t, pkg) {
		if strings.HasPrefix(d, mod) && d != pkg {
			t.Errorf("taskmeta imports module-internal %s — it must stay a pure domain leaf (features import it, never the reverse)", d)
		}
		if strings.Contains(d, "gorm.io/") {
			t.Errorf("taskmeta pulls in %s — the machine-block/label/derive domain must not depend on gorm", d)
		}
		if d == "net/http" {
			t.Errorf("taskmeta pulls in net/http — the domain layer performs no IO")
		}
	}
	for _, imp := range directImports(t, pkg) {
		if imp == "os" {
			t.Errorf("taskmeta directly imports os — the domain layer touches no filesystem")
		}
	}
}

// gormImporters is the frozen allowlist of module-internal LEGACY packages
// permitted to import gorm.io/gorm directly. The DB seam is being migrated
// feature-by-feature into repositories/ (the aep-api testability plan, step 11),
// so this list may only SHRINK: a NEW direct gorm importer fails (route
// persistence through a repository — the real repository over dbtest is the DB
// test seam), and a STALE entry — a package that no longer imports gorm — also
// fails, so every migration trims exactly one row and the list stays honest.
//
// The seven target domains are DELIBERATELY out of scope here: they are governed
// by TestGormFencedToDomainRepository, which allows gorm in <domain>/repository.go
// and nowhere else. Without that carve-out the two rules contradict — a domain
// adding the sanctioned repository.go would be a "NEW direct gorm importer" and
// force this shrink-only list to GROW once per domain, which would rot it
// into a rubber stamp.
var gormImporters = map[string]bool{
	// Composition + kernel (structurally hold gorm; not feature slices).
	"internal/edge": true,
	"internal/app":  true,
	// Public composition seam: Options.ImpersonateOrgResolverBuilder late-binds
	// on *gorm.DB after Resolve opens infra (no persistence of its own).
	"app": true,
	// The secret kernel module (§10.4): its Postgres-backed store is one of the
	// four backends it exists to own. Was internal/credentials.
	"internal/platform/secrets": true,
	// The migration MECHANISM (conn + Runner/Step) — domain-free by design.
	"internal/platform/database": true,
	// The ordered migration LIST — names domain-owned steps, so it sits beside
	// edge rather than in the kernel (§7).
	"internal/migrate":                true,
	"internal/platform/dbtest":        true,
	"internal/platform/componenttest": true,
	// No feature packages remain: the migration is complete. Every domain's raw
	// gorm now lives behind its <domain>/repository*.go and is governed by
	// TestGormFencedToDomainRepository, not this list. This set is the PERMANENT
	// kernel/edge gorm allowlist — it should stay exactly this size.
}

// TestGormImportAllowlist asserts the set of packages that DIRECTLY import
// gorm.io/gorm is exactly gormImporters. It is the ratchet behind the raw-gorm
// → repositories/ migration: coupling to the ORM cannot spread to a new package
// without a failing test, and the list can only shrink as features move their
// persistence behind repositories.
func TestGormImportAllowlist(t *testing.T) {
	out, err := exec.Command("go", "list", "-f",
		`{{.ImportPath}}{{range .Imports}} {{.}}{{end}}`, mod+"/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list %s/...: %v\n%s", mod, err, out)
	}
	remaining := map[string]bool{}
	for k := range gormImporters {
		remaining[k] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		short := strings.TrimPrefix(fields[0], mod+"/")
		if inTargetDomain(short) {
			continue // governed by TestGormFencedToDomainRepository instead
		}
		direct := false
		for _, imp := range fields[1:] {
			if imp == "gorm.io/gorm" {
				direct = true
				break
			}
		}
		if !direct {
			continue
		}
		if !gormImporters[short] {
			t.Errorf("NEW direct gorm.io/gorm importer %q — route persistence through repositories/ (a repository over dbtest is the DB test seam); if this genuinely belongs in the kernel, add it to gormImporters with rationale in the PR", short)
		}
		delete(remaining, short)
	}
	for stale := range remaining {
		t.Errorf("allowlist package %q no longer imports gorm.io/gorm — remove it from gormImporters (the list may only shrink)", stale)
	}
}

// TestInternalOnlyLayout asserts no Go source lives outside the sanctioned
// top-level roots: internal/ (everything), cmd/ (mains), app/ (public
// composition seam — Run(Options)), ocauth/ (public OC auth contracts for
// overlay modules), and secretsprovider/ (public secrets-delivery port for
// overlay modules). The flat models/ and repositories/ shared kernels are both
// DISSOLVED — every entity lives in its owning <domain>/entity_*.go and each
// repository in <domain>/repository_*.go.
func TestInternalOnlyLayout(t *testing.T) {
	allowedRoots := map[string]bool{
		"internal": true, "cmd": true, "app": true, "ocauth": true, "secretsprovider": true,
	}
	root := ".." + string(filepath.Separator) + ".." // module root from internal/arch
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			// Skip VCS/tooling dirs and testdata wholesale.
			if d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if !allowedRoots[top] {
			t.Errorf("Go file outside the sanctioned roots: %s (allowed: internal/, cmd/, app/, ocauth/, secretsprovider/)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module root: %v", err)
	}
}
