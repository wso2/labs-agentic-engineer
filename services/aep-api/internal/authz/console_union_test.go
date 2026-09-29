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

// consolePermissionsPath is the console module that carries its copy of the
// permission vocabulary. Relative to this package.
const consolePermissionsPath = "../../../../apps/console/src/auth/permissions.ts"

// TestConsoleUnionMatchesCatalog is the third seam between this catalog and
// something that cannot import it — the chart's AuthzRoles and the chart's
// console scopes are the other two, and all three work the same way: read the
// other side's file, compare, and say which file to edit.
//
// The console's copy exists because TypeScript cannot import Go and the
// console image is built by a Node-only stage, so there is no moment at which
// generating it would be cheaper than keeping it.
//
// What the console loses when this drifts is bounded but real. Its
// permissionsFromScope filters the token by the `ae:` PREFIX rather than by
// membership of this list, so an unknown key is inert rather than discarded —
// the surface is not silently withheld. What is missing is the TYPE: nobody
// can write a gate on a key the union does not carry, because useHasPermission
// refuses it at compile time. So the failure is loud, but it lands on whoever
// tries to use the permission rather than on whoever added it, which is the
// wrong person and usually weeks later. This test moves it back.
//
// Exact in both directions: a key here and not there is an ungateable
// permission, and a key there and not here is a gate on something no role can
// ever hold, which reads to a user as a permanently locked surface.
func TestConsoleUnionMatchesCatalog(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(consolePermissionsPath))
	if err != nil {
		t.Fatalf("read the console's permission vocabulary: %v\n"+
			"If src/auth/permissions.ts moved, update consolePermissionsPath — do not delete this test.", err)
	}

	console := parseConsolePermissions(t, string(raw))

	var want []string
	for _, p := range AllPermissions {
		want = append(want, string(p))
	}
	slices.Sort(want)
	slices.Sort(console)

	if slices.Equal(console, want) {
		return
	}

	for _, p := range want {
		if !slices.Contains(console, p) {
			t.Errorf("the console's ALL_PERMISSIONS omits %q — no console surface can be gated on it, "+
				"because useHasPermission will not accept a key outside the union.\n"+
				"Add it to %s.", p, consolePermissionsPath)
		}
	}
	for _, p := range console {
		if !slices.Contains(want, p) {
			t.Errorf("the console's ALL_PERMISSIONS carries %q, which no AE permission defines — "+
				"any surface gated on it is locked for everyone, since no role can hold it.\n"+
				"Remove it from %s.", p, consolePermissionsPath)
		}
	}
}

// parseConsolePermissions pulls the keys out of the ALL_PERMISSIONS array.
//
// Scoped to that array rather than scanning the whole file, because the file's
// prose names permissions too (ae:github-config and ae:model-config in the
// hasAny doc comment) and a whole-file scan would read those as vocabulary.
// The union is not parsed separately: it is declared as
// `(typeof ALL_PERMISSIONS)[number]`, so the array IS the union and the two
// cannot disagree.
//
// A regexp rather than a TS parse, for the same reason chart_roles_test.go
// uses a line scanner: the alternative is a Node toolchain to run `go test`.
// It fails loudly rather than silently reading nothing if the shape changes.
func parseConsolePermissions(t *testing.T, src string) []string {
	t.Helper()

	const marker = "export const ALL_PERMISSIONS = ["
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("no `%s` found in %s — the vocabulary moved or was renamed, "+
			"and this test is reading nothing", marker, consolePermissionsPath)
	}
	rest := src[start+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		t.Fatal("the ALL_PERMISSIONS array is not closed — refusing to guess where it ends")
	}

	key := regexp.MustCompile(`"(ae:[a-z-]+)"`)
	var out []string
	for _, m := range key.FindAllStringSubmatch(rest[:end], -1) {
		out = append(out, m[1])
	}

	if len(out) == 0 {
		t.Fatalf("ALL_PERMISSIONS in %s parsed to zero keys — its shape changed",
			consolePermissionsPath)
	}
	return out
}
