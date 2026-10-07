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

import (
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/prototypespec"
)

// The design save gate over a web-application's prototype files. The rules
// themselves are pinned in prototypespec against the tables the kit asserts;
// these rows prove the gate applies them to the right files, reports them as
// per-file rows, and judges nothing it should leave alone.

const (
	prototypeManifestKey = gateComponentDir + "prototype.json"
	prototypeSourceKey   = gateComponentDir + "prototype.tsx"

	validPrototypeManifest = `{
  "schemaVersion": 3,
  "name": "Expenses",
  "entryScreen": "home",
  "roles": [{ "id": "user", "name": "Employee" }],
  "states": [{ "id": "default", "name": "Default" }],
  "screens": [{ "id": "home", "name": "Home", "roleIds": ["user"] }],
  "flows": []
}`
	validPrototypeSource = `import { defineApp } from "@wso2/prototype-kit";
export default defineApp({ screens: {}, data: {} });
`
)

func prototypeBundle(t *testing.T) map[string]string {
	t.Helper()
	files := securityDesignBundle(t)
	files[prototypeManifestKey] = validPrototypeManifest
	files[prototypeSourceKey] = validPrototypeSource
	return files
}

func fileRows(t *testing.T, err error) []FileValidationError {
	t.Helper()
	if err == nil {
		return nil
	}
	verr, ok := err.(*DesignValidationError)
	if !ok {
		t.Fatalf("want *DesignValidationError, got %T: %v", err, err)
	}
	return verr.Files
}

func TestSaveGate_AcceptsASoundPrototype(t *testing.T) {
	if err := validateDesignBundle(prototypeBundle(t)); err != nil {
		t.Fatalf("a sound prototype must pass the save gate: %v", err)
	}
}

func TestSaveGate_PrototypeIsOptionalAndEitherHalfMayBeAbsent(t *testing.T) {
	for _, drop := range [][]string{{prototypeManifestKey, prototypeSourceKey}, {prototypeSourceKey}, {prototypeManifestKey}} {
		files := prototypeBundle(t)
		for _, k := range drop {
			delete(files, k)
		}
		if err := validateDesignBundle(files); err != nil {
			t.Errorf("dropping %v must not refuse the save: %v", drop, err)
		}
	}
}

func TestSaveGate_RefusesAnInvalidPrototype(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		content string
		code    string
		message string
	}{
		{"manifest is not JSON", prototypeManifestKey, "{nope", prototypespec.CodeSchemaViolation, "not valid JSON"},
		{"manifest has the legacy component field", prototypeManifestKey,
			strings.Replace(validPrototypeManifest, `"name": "Expenses",`, `"name": "Expenses", "component": "web",`, 1),
			prototypespec.CodeSchemaViolation, "component"},
		{"manifest names an unknown entry screen", prototypeManifestKey,
			strings.Replace(validPrototypeManifest, `"entryScreen": "home"`, `"entryScreen": "nowhere"`, 1),
			prototypespec.CodeUnknownReference, `entryScreen: entryScreen "nowhere" is not one of the screens`},
		{"manifest repeats an id", prototypeManifestKey,
			strings.Replace(validPrototypeManifest, `"id": "default"`, `"id": "user"`, 1),
			prototypespec.CodeDuplicateID, "states[0].id"},
		{"manifest is another version", prototypeManifestKey,
			strings.Replace(validPrototypeManifest, `"schemaVersion": 3`, `"schemaVersion": 2`, 1),
			prototypespec.CodeUnsupportedVersion, "schemaVersion 2 is not supported"},
		{"source does not parse", prototypeSourceKey, "export default defineApp({\n", prototypespec.CodeSyntaxError, "line"},
		{"source imports another package", prototypeSourceKey, `import _ from "lodash";` + "\n",
			prototypespec.CodeForbiddenImport, `line 1: import of "lodash"`},
		{"source is over the size cap", prototypeSourceKey, validPrototypeSource + "//" + strings.Repeat("x", prototypespec.MaxSourceLength),
			prototypespec.CodeSourceTooLarge, "(file)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := prototypeBundle(t)
			files[tc.key] = tc.content
			rows := fileRows(t, validateDesignBundle(files))
			if len(rows) != 1 {
				t.Fatalf("want exactly one row, got %+v", rows)
			}
			if rows[0].Path != tc.key || rows[0].Code != tc.code {
				t.Errorf("row = %s %s, want %s %s", rows[0].Path, rows[0].Code, tc.key, tc.code)
			}
			assertContains(t, rows[0].Message, tc.message)
		})
	}
}

// Each component's files are judged on their own: a bad prototype names its
// component's file, and a sound one beside it adds nothing.
func TestSaveGate_JudgesEachComponentsPrototype(t *testing.T) {
	files := prototypeBundle(t)
	files["components/other/prototype.json"] = "{nope"
	rows := fileRows(t, validateDesignBundle(files))
	if len(rows) != 1 || rows[0].Path != "components/other/prototype.json" {
		t.Fatalf("want one row naming the other component's manifest, got %+v", rows)
	}
}

// A prototype is a component's own file. The same name anywhere else in the
// bundle is not one, and is left alone.
func TestSaveGate_LeavesStrayPrototypeNamesAlone(t *testing.T) {
	files := prototypeBundle(t)
	files["prototype.json"] = "{nope"
	files[gateComponentDir+"nested/prototype.tsx"] = "export default defineApp({\n"
	files["dependencies/billing/prototype.json"] = "{nope"
	if err := validateDesignBundle(files); err != nil {
		t.Fatalf("a prototype name outside components/<c>/ must not be judged: %v", err)
	}
}

// The extension allow-list is what lets prototype.tsx ride the bundle the gate
// reads in the first place.
func TestDesignBundleFilter_AdmitsPrototypeFiles(t *testing.T) {
	for rel, want := range map[string]bool{
		"components/web/prototype.json": true,
		"components/web/prototype.tsx":  true,
		"components/web/prototype.ts":   false,
		"components/web/prototype.jsx":  false,
	} {
		if got := designBundleFilter(rel); got != want {
			t.Errorf("designBundleFilter(%q) = %v, want %v", rel, got, want)
		}
	}
}
