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

package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The domain/slice boundary rules of the domain architecture.
//
// Each rule polices the domains on disk and is strict: the legacy flat layout it
// once had to coexist with is gone.
//
// A rule nobody has seen fail is a rule nobody knows works. Every rule below is
// therefore written as a pure function over a ROOT PATH, and each has a
// fires-proof test that plants the violation in a temp tree and asserts the rule
// reports it. Without that, a scanner with a typo'd path would "pass" forever.

// targetDomains are the business domains of §3. This is a DESIGN decision,
// not a disk discovery: a new top-level package under internal/ must be
// classified — as a domain here, or as infrastructure in nonDomainPkgs — so that
// growing an eighth domain is a deliberate act with a review, not a side effect.
var targetDomains = map[string]bool{
	"organization":  true,
	"spec":          true,
	"delivery":      true,
	"dependencies":  true,
	"projects":      true,
	"sourcecontrol": true,
	"ops":           true,
	// The SHARED identity-provider Roles and Test users a build provisions from
	// specs/design/security.json. A domain rather than kernel infrastructure: it
	// owns entities, a repository and a lifecycle of its own, and three other
	// domains reach it only through ports.
	"identity": true,
}

// nonDomainPkgs are the internal/ packages that are NOT business domains: the
// kernel + the edge machinery. The legacy rows (api, feature) were deleted in P9
// once internal/api collapsed into edge/ and every feature moved to its domain.
var nonDomainPkgs = map[string]bool{
	"platform":  true, // the kernel
	"edge":      true, // the surface composer (was internal/api)
	"gen":       true, // generated wire types — public surface
	"igen":      true, // generated wire types — S2S surface
	"migrate":   true, // the ordered migration list
	"app":       true, // the composition root
	"arch":      true, // these tests
	"clients":   true, // outbound adapters (folds into platform/clients)
	"config":    true,
	"contracts": true,
	"seed":      true,
}

// plannedPkgs are classified names that do not exist YET. They are listed
// separately so the honesty check below can demand that every OTHER row
// correspond to something real — the distinction between "planned" and "stale"
// is exactly what a classification map loses if nobody checks it. Empty now:
// edge/ exists, so nothing is merely planned.
var plannedPkgs = map[string]bool{}

// domainsOnDisk returns the domains that exist on disk. All seven have landed, so
// TestAllDomainsLanded pins that this equals the full targetDomains set — a missing
// domain is a regression. The rules below still range over the discovered set so
// they stay disk-driven, but the set is no longer allowed to be a subset.
func domainsOnDisk(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, d := range listDir(t, root) {
		if targetDomains[d] {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// TestAllDomainsLanded is the strict-phase gate: every one of the seven target
// domains must exist on disk. During the migration domainsOnDisk was
// deliberately a subset (rules applied only to landed domains); now that P9 has
// landed, a missing domain means a package was deleted or renamed away from its
// classification — a regression this catches immediately.
func TestAllDomainsLanded(t *testing.T) {
	got := map[string]bool{}
	for _, d := range domainsOnDisk(t, "..") {
		got[d] = true
	}
	for d := range targetDomains {
		if !got[d] {
			t.Errorf("target domain %q is not on disk — every domain must exist post-migration (classification regression?)", d)
		}
	}
	if len(got) != len(targetDomains) {
		t.Errorf("domains on disk = %d, want %d (the full targetDomains set)", len(got), len(targetDomains))
	}
}

// fileImports parses one Go file and returns its import paths. Parsing (rather
// than grepping) means a path named in a comment or a string literal cannot
// produce a false violation.
func fileImports(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// goFilesIn returns the .go files directly in dir (not its sub-packages).
func goFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out
}

// subPackagesOf returns a domain's sub-package directory names — its slices plus
// the httpapi aggregator.
func subPackagesOf(t *testing.T, root, domain string) []string {
	t.Helper()
	dir := filepath.Join(root, domain)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	return listDir(t, dir)
}

// ── Rule: gorm lives only in <domain>/repository.go ─────────────────────────

// gormFenceViolations returns every file under a domain that imports gorm from
// somewhere other than the domain-root's repository.go.
//
// This is the per-domain successor to the flat gormImporters ratchet, which is
// keyed to internal/feature/* + repositories/ and would go RED the moment a
// domain adds its own repository.go. Both run side by side during the migration:
// the old list shrinks as features die, this fence governs what replaces them.
func gormFenceViolations(t *testing.T, root string, domains []string) []string {
	t.Helper()
	var bad []string
	for _, d := range domains {
		_ = filepath.WalkDir(filepath.Join(root, d), func(path string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil // tests may touch gorm (dbtest fixtures)
			}
			for _, imp := range fileImports(t, path) {
				if !strings.HasPrefix(imp, "gorm.io/") {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				// The sanctioned home: the domain ROOT's repository seam —
				// repository.go, or repository_<name>.go when a domain owns
				// several tables and one file per table reads better than one
				// 600-line merge (AGENTS.md: separate by responsibility). Gorm in
				// a SLICE (a sub-dir) is still banned, however it is named.
				base := filepath.Base(rel)
				atRoot := filepath.Dir(rel) == d
				if !(atRoot && (base == "repository.go" || strings.HasPrefix(base, "repository_"))) {
					bad = append(bad, rel)
				}
			}
			return nil
		})
	}
	sort.Strings(bad)
	return bad
}

// TestGormFencedToDomainRepository asserts the ORM stays behind each domain's
// repository. A slice that reaches for gorm directly has escaped the seam that
// makes the domain testable.
func TestGormFencedToDomainRepository(t *testing.T) {
	if bad := gormFenceViolations(t, "..", domainsOnDisk(t, "..")); len(bad) > 0 {
		t.Errorf("gorm.io imported outside <domain>/repository.go: %v\n"+
			"Persistence belongs behind the domain's repository interface — a slice uses the "+
			"interface, never the ORM.", bad)
	}
}

// ── Rule: the domain-root never imports its own sub-packages ────────────────

// rootImportsSliceViolations returns domain-root files that import one of the
// domain's own sub-packages. The dependency runs slices -> root, never back:
// the root holds the shared core (model, repository, ports, funnel), so a root
// that imported a slice would be a cycle in the design even where Go permits it.
func rootImportsSliceViolations(t *testing.T, root, modPath string, domains []string) []string {
	t.Helper()
	var bad []string
	for _, d := range domains {
		subs := subPackagesOf(t, root, d)
		for _, f := range goFilesIn(t, filepath.Join(root, d)) {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			for _, imp := range fileImports(t, f) {
				for _, sub := range subs {
					want := modPath + "/internal/" + d + "/" + sub
					if imp == want || strings.HasPrefix(imp, want+"/") {
						rel, _ := filepath.Rel(root, f)
						bad = append(bad, rel+" -> "+d+"/"+sub)
					}
				}
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// TestDomainRootNeverImportsItsSlices asserts root ⊥ slice.
func TestDomainRootNeverImportsItsSlices(t *testing.T) {
	if bad := rootImportsSliceViolations(t, "..", mod, domainsOnDisk(t, "..")); len(bad) > 0 {
		t.Errorf("domain-root imports its own sub-package: %v\n"+
			"Slices import the root, never the reverse. Shared behaviour belongs IN the root.", bad)
	}
}

// ── Rule: a slice never imports a sibling slice ─────────────────────────────

// siblingSliceViolations returns slice-to-sibling-slice imports. This is the
// rule that makes a slice a real blast-radius boundary rather than a naming
// convention: without it, "change one use-case" silently means "change three".
//
// httpapi is exempt as the IMPORTER: it is the domain's aggregator, and
// embedding every slice's handler is its entire job. It is never an importee.
func siblingSliceViolations(t *testing.T, root, modPath string, domains []string) []string {
	t.Helper()
	var bad []string
	for _, d := range domains {
		subs := subPackagesOf(t, root, d)
		for _, from := range subs {
			if from == "httpapi" {
				continue // the aggregator imports slices by design
			}
			_ = filepath.WalkDir(filepath.Join(root, d, from), func(path string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				for _, imp := range fileImports(t, path) {
					for _, to := range subs {
						if to == from {
							continue
						}
						want := modPath + "/internal/" + d + "/" + to
						if imp == want || strings.HasPrefix(imp, want+"/") {
							rel, _ := filepath.Rel(root, path)
							bad = append(bad, rel+" -> "+d+"/"+to)
						}
					}
				}
				return nil
			})
		}
	}
	sort.Strings(bad)
	return bad
}

// TestSliceNeverImportsSibling asserts slice ⊥ sibling slice.
func TestSliceNeverImportsSibling(t *testing.T) {
	if bad := siblingSliceViolations(t, "..", mod, domainsOnDisk(t, "..")); len(bad) > 0 {
		t.Errorf("slice imports a sibling slice: %v\n"+
			"Share through the domain-root instead — and only once the duplication is real.", bad)
	}
}

// ── Rule: every internal/ package is classified ─────────────────────────────

// TestEveryInternalPackageIsClassified asserts no top-level internal/ package
// escapes classification. A new directory here is either an eighth domain (a
// design decision) or infrastructure — both deserve a reviewer, and neither
// should be able to appear by accident.
func TestEveryInternalPackageIsClassified(t *testing.T) {
	for _, d := range listDir(t, "..") {
		if !targetDomains[d] && !nonDomainPkgs[d] && !plannedPkgs[d] {
			t.Errorf("internal/%s is neither a target domain (§3) nor classified infrastructure — "+
				"add it to targetDomains or nonDomainPkgs and say which it is in the PR", d)
		}
	}
}

// TestClassificationIsHonest is the reverse direction: every classified
// infrastructure package must actually EXIST. Without it the map only checks
// disk->map, so a row survives the deletion of the thing it names and the map
// slowly becomes a description of the past — precisely what happened to the
// "credentials" row, which outlived its package by one commit (P0.6 moved it to
// platform/secrets).
//
// Domains are exempt: they are the migration's TARGET and appear one per phase.
// plannedPkgs is the explicit, reviewed list of not-yet-existing names.
func TestClassificationIsHonest(t *testing.T) {
	present := map[string]bool{}
	for _, d := range listDir(t, "..") {
		present[d] = true
	}
	for p := range nonDomainPkgs {
		if !present[p] {
			t.Errorf("nonDomainPkgs names internal/%s, which does not exist — remove the row, or "+
				"move it to plannedPkgs if it is genuinely still coming", p)
		}
	}
	for p := range plannedPkgs {
		if present[p] {
			t.Errorf("plannedPkgs names internal/%s, which now EXISTS — move it to nonDomainPkgs "+
				"(or targetDomains) so the map stops calling it hypothetical", p)
		}
	}
}

// ── Proof that the rules fire ───────────────────────────────────────────────
//
// Each rule is planted with its violation in a temp tree. Until a domain exists
// on disk these are the ONLY evidence the scanners work at all — and even after,
// they are what proves a green run means "no violations" rather than "scanned
// nothing".

// plantDomain writes files into root/<domain>/<relpath> to fake a domain layout.
func plantDomain(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func TestGormFenceFires(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		// Sanctioned: the domain-root's repository seam — the canonical file and
		// a per-table repository_<name>.go both count.
		"ops/repository.go":         "package ops\n\nimport _ \"gorm.io/gorm\"\n",
		"ops/repository_reports.go": "package ops\n\nimport _ \"gorm.io/gorm\"\n",
		// The violations: a slice reaching past its repository straight to the ORM,
		// and a non-repository file at the domain root.
		"ops/listreports/handler.go": "package listreports\n\nimport _ \"gorm.io/gorm\"\n",
		"ops/model.go":               "package ops\n\nimport _ \"gorm.io/gorm\"\n",
		// A repository.go inside a SLICE is NOT the root seam — still a violation.
		"ops/listreports/repository.go": "package listreports\n\nimport _ \"gorm.io/gorm\"\n",
	})
	bad := gormFenceViolations(t, root, []string{"ops"})
	want := map[string]bool{
		filepath.Join("ops", "listreports", "handler.go"):    true,
		filepath.Join("ops", "model.go"):                     true,
		filepath.Join("ops", "listreports", "repository.go"): true,
	}
	if len(bad) != len(want) {
		t.Fatalf("gorm fence: got %v, want exactly the 3 non-root-seam files", bad)
	}
	for _, b := range bad {
		if !want[b] {
			t.Fatalf("gorm fence flagged an unexpected file %q (want %v)", b, want)
		}
		if b == filepath.Join("ops", "repository.go") || b == filepath.Join("ops", "repository_reports.go") {
			t.Fatalf("gorm fence wrongly flagged the domain's own repository seam: %v", bad)
		}
	}
}

func TestRootImportsSliceRuleFires(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/model.go":               "package ops\n\nimport _ \"" + mod + "/internal/ops/listreports\"\n",
		"ops/listreports/handler.go": "package listreports\n",
	})
	if bad := rootImportsSliceViolations(t, root, mod, []string{"ops"}); len(bad) != 1 {
		t.Fatalf("root->slice rule did not fire: got %v", bad)
	}
}

func TestSiblingSliceRuleFires(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/listreports/handler.go": "package listreports\n\nimport _ \"" + mod + "/internal/ops/getreport\"\n",
		"ops/getreport/handler.go":   "package getreport\n",
		// The aggregator imports both — and must NOT be reported.
		"ops/httpapi/aggregate.go": "package httpapi\n\nimport (\n\t_ \"" + mod + "/internal/ops/getreport\"\n\t_ \"" + mod + "/internal/ops/listreports\"\n)\n",
	})
	bad := siblingSliceViolations(t, root, mod, []string{"ops"})
	if len(bad) != 1 {
		t.Fatalf("sibling-slice rule fired %d times, want exactly 1 (the httpapi aggregator is "+
			"exempt as importer; only listreports->getreport is a violation): %v", len(bad), bad)
	}
	if !strings.Contains(bad[0], "listreports") {
		t.Fatalf("sibling-slice rule reported the wrong file: %v", bad)
	}
}

// TestSliceRulesDoNotFireOnACleanDomain is the mirror: a correctly-shaped domain
// must produce ZERO violations. Without it, a scanner that always reported a
// violation would pass every fires-proof above.
func TestSliceRulesDoNotFireOnACleanDomain(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/model.go":               "package ops\n",
		"ops/repository.go":          "package ops\n\nimport _ \"gorm.io/gorm\"\n",
		"ops/listreports/handler.go": "package listreports\n\nimport _ \"" + mod + "/internal/ops\"\n",
		"ops/getreport/handler.go":   "package getreport\n\nimport _ \"" + mod + "/internal/platform/tenant\"\n",
		"ops/httpapi/aggregate.go":   "package httpapi\n\nimport _ \"" + mod + "/internal/ops/listreports\"\n",
	})
	domains := []string{"ops"}
	if bad := gormFenceViolations(t, root, domains); len(bad) != 0 {
		t.Errorf("gorm fence fired on a clean domain: %v", bad)
	}
	if bad := rootImportsSliceViolations(t, root, mod, domains); len(bad) != 0 {
		t.Errorf("root->slice rule fired on a clean domain: %v", bad)
	}
	if bad := siblingSliceViolations(t, root, mod, domains); len(bad) != 0 {
		t.Errorf("sibling-slice rule fired on a clean domain: %v", bad)
	}
}

// harnessExempt are the platform packages allowed to see domains: they assemble
// the REAL graph, which is their entire job — the same deliberate exception
// TestPlatformAndContractsAreFeatureFree already makes for componenttest.
var harnessExempt = map[string]bool{"componenttest": true, "dbtest": true}

// platformDomainViolations returns platform files that DIRECTLY import a domain.
//
// Source-based, so the very same function can be aimed at a planted temp tree —
// the fires-proof below calls THIS function, not a hand-rolled copy of it. (A
// fires-proof that re-implements its rule proves only that the copy works.)
func platformDomainViolations(t *testing.T, root, modPath string, domains []string) []string {
	t.Helper()
	var bad []string
	platformDir := filepath.Join(root, "platform")
	if _, err := os.Stat(platformDir); err != nil {
		return nil
	}
	for _, p := range listDir(t, platformDir) {
		if harnessExempt[p] {
			continue
		}
		_ = filepath.WalkDir(filepath.Join(platformDir, p), func(path string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			for _, imp := range fileImports(t, path) {
				for _, dom := range domains {
					want := modPath + "/internal/" + dom
					if imp == want || strings.HasPrefix(imp, want+"/") {
						rel, _ := filepath.Rel(root, path)
						bad = append(bad, rel+" -> "+dom)
					}
				}
			}
			return nil
		})
	}
	sort.Strings(bad)
	return bad
}

// TestPlatformImportsNoDomain asserts the kernel stays domain-free as domains
// appear. The existing TestPlatformAndContractsAreFeatureFree covers the legacy
// feature/ layout; this is its successor for internal/<domain>.
//
// Two checks: the source-based one (shared with the fires-proof) catches a direct
// import, and the `go list -deps` one additionally catches a domain reached
// TRANSITIVELY — platform/x -> clients/y -> domain — which no source scan of
// platform/ alone could see. The transitive half only runs once a domain exists.
func TestPlatformImportsNoDomain(t *testing.T) {
	domains := domainsOnDisk(t, "..")

	if bad := platformDomainViolations(t, "..", mod, domains); len(bad) > 0 {
		t.Errorf("platform imports a domain: %v\n"+
			"The kernel stays domain-free — invert the dependency with a port owned by the domain.", bad)
	}

	if len(domains) == 0 {
		return // nothing for the transitive half to find yet
	}
	for _, p := range listDir(t, "../platform") {
		if harnessExempt[p] {
			continue
		}
		pkg := mod + "/internal/platform/" + p
		for d := range deps(t, pkg) {
			for _, dom := range domains {
				if d == mod+"/internal/"+dom || strings.HasPrefix(d, mod+"/internal/"+dom+"/") {
					t.Errorf("platform/%s reaches domain %s (transitively, via %s) — the kernel "+
						"stays domain-free", p, dom, d)
				}
			}
		}
	}
}

// TestPlatformImportsNoDomainFires proves the rule by calling the rule — the
// same function the real test uses — against a planted kernel->domain import.
func TestPlatformImportsNoDomainFires(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"platform/obs/obs.go": "package obs\n\nimport _ \"" + mod + "/internal/ops\"\n",
		"ops/model.go":        "package ops\n",
	})
	if bad := platformDomainViolations(t, root, mod, []string{"ops"}); len(bad) != 1 {
		t.Fatalf("the platform->domain rule did not fire on a planted import: got %v", bad)
	}
}

// TestPlatformImportsNoDomainDoesNotOverfire is the mirror — including the
// harness carve-out, which is the one way this rule could wrongly fail a phase.
func TestPlatformImportsNoDomainDoesNotOverfire(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"platform/obs/obs.go":               "package obs\n\nimport _ \"" + mod + "/internal/platform/tenant\"\n",
		"platform/componenttest/harness.go": "package componenttest\n\nimport _ \"" + mod + "/internal/ops\"\n",
		"ops/model.go":                      "package ops\n",
	})
	if bad := platformDomainViolations(t, root, mod, []string{"ops"}); len(bad) != 0 {
		t.Fatalf("rule fired on a clean kernel / exempt harness: %v", bad)
	}
}

// inTargetDomain reports whether a module-relative package path (e.g.
// "internal/ops/listreports") lives inside one of the seven target domains.
//
// It is the hand-off point between the two gorm rules: the legacy shrink-only
// gormImporters list governs everything OUTSIDE the domains, and
// TestGormFencedToDomainRepository governs everything inside. Without this split
// the rules contradict — the sanctioned <domain>/repository.go would read as a
// "NEW direct gorm importer" and force the shrink-only list to grow once per
// domain phase (§19.6).
func inTargetDomain(short string) bool {
	parts := strings.Split(short, "/")
	return len(parts) >= 2 && parts[0] == "internal" && targetDomains[parts[1]]
}

// TestGormRulesHandOffCleanly proves the two gorm rules partition the tree
// rather than overlap or leave a gap: the sanctioned domain repository is
// exempt from the legacy list, and everything outside a domain is still subject
// to it.
func TestGormRulesHandOffCleanly(t *testing.T) {
	cases := []struct {
		pkg    string
		domain bool
	}{
		{"internal/ops", true},                   // a domain root -> the fence
		{"internal/ops/listreports", true},       // a slice -> the fence
		{"internal/spec/httpapi", true},          // an aggregator -> the fence
		{"internal/platform/database", false},    // kernel -> the legacy list
		{"internal/feature/organization", false}, // legacy feature -> the legacy list
		{"repositories", false},                  // the flat kernel -> the legacy list
		{"internal/opsomething", false},          // NOT a domain despite the prefix
	}
	for _, c := range cases {
		if got := inTargetDomain(c.pkg); got != c.domain {
			t.Errorf("inTargetDomain(%q) = %v, want %v", c.pkg, got, c.domain)
		}
	}
}

// ── Rule: a domain aggregator declares no methods ───────────────────────────

// methodsDeclaredIn returns "Type.Method" for every method declared in the Go
// files directly under dir (tests excluded).
func methodsDeclaredIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for _, f := range goFilesIn(t, dir) {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			expr := fn.Recv.List[0].Type
			if star, isPtr := expr.(*ast.StarExpr); isPtr {
				expr = star.X
			}
			name := "?"
			if id, ok := expr.(*ast.Ident); ok {
				name = id.Name
			}
			out = append(out, name+"."+fn.Name.Name)
		}
	}
	sort.Strings(out)
	return out
}

// aggregatorMethodViolations returns methods declared in any domain's httpapi
// package. An aggregator must ONLY embed.
func aggregatorMethodViolations(t *testing.T, root string, domains []string) []string {
	t.Helper()
	var bad []string
	for _, d := range domains {
		dir := filepath.Join(root, d, "httpapi")
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		for _, m := range methodsDeclaredIn(t, dir) {
			bad = append(bad, d+"/httpapi: "+m)
		}
	}
	sort.Strings(bad)
	return bad
}

// TestAggregatorsDeclareNoMethods pins the third precondition of the edge nets
// (§19.1) — the one NEITHER other net can catch.
//
// A domain's httpapi aggregator embeds slice handlers, so a slice's op reaches
// the edge at depth-2. A method declared ON the aggregator sits at depth-1 and
// silently shadows its own slice: green build, slice dead.
//
//   - The legacyShim cannot catch it: the shim only makes a same-depth TIE an
//     `ambiguous selector`, and depth-1 vs depth-2 is not a tie.
//   - The method-origin reflection gate cannot catch it either, once the op has
//     been cut from legacy — which is exactly what a completed migration looks
//     like. embedsProviding reports the domain embed (true — it does provide the
//     method, just the aggregator's own), the ledger agrees, and everything
//     passes. After P9 deletes legacy, it could never catch it at all.
//
// Only the source says who DECLARED what, which is why this mirrors
// TestApiServerDeclaresNoMethods one level down.
func TestAggregatorsDeclareNoMethods(t *testing.T) {
	if bad := aggregatorMethodViolations(t, "..", domainsOnDisk(t, "..")); len(bad) > 0 {
		t.Errorf("a domain aggregator declares methods: %v\n"+
			"An aggregator only EMBEDS. A method here sits at depth-1 and silently shadows the "+
			"slice's own at depth-2 — the build stays green and the slice becomes dead code. "+
			"Move the body into the slice.", bad)
	}
}

// TestAggregatorRuleFires proves it, since a rule with nothing to find is a rule
// nobody knows works.
func TestAggregatorRuleFires(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/httpapi/aggregate.go": "package httpapi\n\n" +
			"type Handlers struct{ *sliceHandler }\n\n" +
			"// the shadowing method: depth-1, beats the slice's own at depth-2\n" +
			"func (h *Handlers) ListRcaAgentReports() string { return \"shadowed\" }\n",
	})
	bad := aggregatorMethodViolations(t, root, []string{"ops"})
	if len(bad) != 1 || !strings.Contains(bad[0], "Handlers.ListRcaAgentReports") {
		t.Fatalf("the aggregator rule did not fire on a planted shadowing method: got %v", bad)
	}
}

// TestAggregatorRuleDoesNotOverfire is the mirror: a pure aggregator is clean.
func TestAggregatorRuleDoesNotOverfire(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/httpapi/aggregate.go": "package httpapi\n\n" +
			"type Handlers struct{ *sliceHandler }\n\n" +
			"func New() *Handlers { return nil }\n", // a plain func is not a method
	})
	if bad := aggregatorMethodViolations(t, root, []string{"ops"}); len(bad) != 0 {
		t.Fatalf("the aggregator rule fired on a pure aggregator: %v", bad)
	}
}

// ── Rule: a landed domain never imports a legacy feature ────────────────────

// domainImportsFeatureViolations returns domain files that import
// internal/feature/*.
//
// This rule is what replaces the featureEdgeAllowlist for a migrated package.
// When a feature becomes a domain, its allowlist row is deleted (the honesty
// check demands it) — which silently stops policing everything about it. The
// edge that actually matters is the REVERSE of the one the allowlist tracked:
// features importing a domain are transitional and die with the feature, but a
// DOMAIN importing a feature is legacy leaking into the target, and would
// quietly make the domain undeletable-from-legacy at P9.
func domainImportsFeatureViolations(t *testing.T, root, modPath string, domains []string) []string {
	t.Helper()
	var bad []string
	for _, d := range domains {
		_ = filepath.WalkDir(filepath.Join(root, d), func(path string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			for _, imp := range fileImports(t, path) {
				if strings.HasPrefix(imp, modPath+"/internal/feature/") {
					rel, _ := filepath.Rel(root, path)
					bad = append(bad, rel+" -> "+strings.TrimPrefix(imp, modPath+"/internal/"))
				}
			}
			return nil
		})
	}
	sort.Strings(bad)
	return bad
}

// TestDomainsAreFeatureFree asserts the target never depends on the legacy it is
// replacing. A domain is DONE; a feature is dying. The arrow may only point the
// other way.
func TestDomainsAreFeatureFree(t *testing.T) {
	if bad := domainImportsFeatureViolations(t, "..", mod, domainsOnDisk(t, "..")); len(bad) > 0 {
		t.Errorf("a domain imports a legacy feature: %v\n"+
			"Domains do not depend on features — features are being deleted. Declare a "+
			"consumer-side port in the domain's ports.go and bridge it at the composition root "+
			"(labelled with the phase that retires the bridge).", bad)
	}
}

func TestDomainsAreFeatureFreeFires(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/model.go": "package ops\n\nimport _ \"" + mod + "/internal/feature/gitrepo\"\n",
	})
	if bad := domainImportsFeatureViolations(t, root, mod, []string{"ops"}); len(bad) != 1 {
		t.Fatalf("the domain->feature rule did not fire: got %v", bad)
	}
}

func TestDomainsAreFeatureFreeDoesNotOverfire(t *testing.T) {
	root := t.TempDir()
	plantDomain(t, root, map[string]string{
		"ops/model.go": "package ops\n\nimport (\n\t_ \"" + mod + "/internal/platform/tenant\"\n\t_ \"" + mod + "/internal/gen\"\n)\n",
	})
	if bad := domainImportsFeatureViolations(t, root, mod, []string{"ops"}); len(bad) != 0 {
		t.Fatalf("the domain->feature rule fired on kernel/gen imports: %v", bad)
	}
}
