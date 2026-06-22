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

package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestGateInvariant_NoRawGatedRouteRegistration is the CI allowlist lock for the
// gate's STRUCTURAL invariant: every route under a gated prefix
// (/api/v1/organizations, /internal/credentials, /repos) MUST register through a
// typed Router/ServiceRouter method (rt.OrgScoped / rt.RepoScoped /
// rt.HandleRepoScoped / rt.Public) — never a raw *http.ServeMux receiver
// (mux.Handle / mux.HandleFunc). Routing every gated route through the typed
// methods is what makes "this route's tenant scope was decided on purpose" an
// allowlist-by-construction property rather than a per-file habit.
//
// The test is a deliberately simple string scan (not full AST): it reads every
// api/*_routes.go file and flags any line that BOTH (a) calls a raw ServeMux
// register method on a `*http.ServeMux`-shaped receiver AND (b) embeds a
// pattern string containing a gated prefix — unless the (file:line) is on the
// explicit allowlist below.
func TestGateInvariant_NoRawGatedRouteRegistration(t *testing.T) {
	// gatedPrefixes — substrings that mark a tenant-gated route surface. A raw
	// ServeMux registration of any of these is the regression we forbid.
	gatedPrefixes := []string{
		"/api/v1/organizations",
		"/internal/credentials",
		"/repos",
	}

	// rawRegisterRE matches a raw ServeMux register call of the form
	// `<recv>.Handle(` or `<recv>.HandleFunc(`. The router types deliberately
	// expose Handle*-free method names (OrgScoped/RepoScoped/HandleRepoScoped/
	// Public), so any `.Handle(`/`.HandleFunc(` call is a raw *http.ServeMux
	// registration. Router-method calls (rt.OrgScoped(...), etc.) do not match
	// this regex and are therefore always allowed.
	rawRegisterRE := regexp.MustCompile(`\.(Handle|HandleFunc)\(`)

	// allowlist — (file:line)-keyed carve-outs that intentionally register a
	// gated-prefix route on a raw mux. Each MUST carry a justification.
	// The Anthropic /effective-key resolver and the JWKS endpoint are now
	// code-first Huma operations (api/huma_infra.go), routed into apiMux from
	// app.go (not a *_routes.go file), so there are no raw gated-prefix
	// registrations in *_routes.go to carve out here.
	allowlist := map[string]string{}

	files, err := filepath.Glob("*_routes.go")
	if err != nil {
		t.Fatalf("glob *_routes.go: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no *_routes.go files found; scan would be vacuously green — check cwd")
	}

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		var currentFunc string
		funcRE := regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)`)

		for i, line := range strings.Split(string(src), "\n") {
			lineNo := i + 1
			trimmed := strings.TrimSpace(line)

			// Track the enclosing function name so allowlist entries can be
			// keyed by (file:funcName), which is stable across edits/reorders.
			if m := funcRE.FindStringSubmatch(trimmed); m != nil {
				currentFunc = m[1]
			}

			// Skip comments — gated prefixes appear constantly in doc comments.
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}

			if !rawRegisterRE.MatchString(line) {
				continue // not a raw register call (router methods land here)
			}

			// Does this raw register line carry a gated-prefix pattern string?
			var hit string
			for _, p := range gatedPrefixes {
				if strings.Contains(line, `"`) && containsPatternPrefix(line, p) {
					hit = p
					break
				}
			}
			if hit == "" {
				continue // raw register, but not of a gated-prefix route
			}

			key := file + ":" + currentFunc
			if reason, ok := allowlist[key]; ok {
				t.Logf("allowlisted raw gated route at %s:%d (%s): %s", file, lineNo, key, reason)
				continue
			}

			t.Errorf("gate invariant violated: %s:%d registers gated-prefix route %q via a raw *http.ServeMux call (%s).\n"+
				"  Route it through the typed Router/ServiceRouter (rt.OrgScoped/RepoScoped/HandleRepoScoped/Public) instead,\n"+
				"  or add %q to the allowlist with a justification if it is an intentional carve-out.",
				file, lineNo, hit, strings.TrimSpace(line), key)
		}
	}
}

// containsPatternPrefix reports whether line embeds an HTTP pattern string whose
// path contains prefix. It looks inside the double-quoted literal so a gated
// prefix mentioned only in a trailing comment on the same line is not counted.
func containsPatternPrefix(line, prefix string) bool {
	// Extract the first double-quoted literal (the pattern arg).
	start := strings.Index(line, `"`)
	if start < 0 {
		return false
	}
	rest := line[start+1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return false
	}
	return strings.Contains(rest[:end], prefix)
}
