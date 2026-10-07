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

package prototypespec

// The Go half of the parity proof. It reads the tables the kit's own tests
// assert (packages/prototype-kit/test/fixtures) over the module boundary rather
// than copying them, because the promise is that a prototype one gate accepts
// the other accepts, and a copied table is a promise that decays silently.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

// The kit's shared fixtures: prototypespec → platform → internal → aep-api →
// services → repo root.
const fixtures = "../../../../../packages/prototype-kit/test/fixtures/"

type expectedFinding struct {
	Code     string `json:"code"`
	Location string `json:"location"`
	Message  string `json:"message"`
}

type patchOp struct {
	Op    string `json:"op"`
	Path  []any  `json:"path"`
	Value any    `json:"value"`
}

type manifestCase struct {
	Name       string            `json:"name"`
	Patch      []patchOp         `json:"patch"`
	Document   json.RawMessage   `json:"document"`
	Text       *string           `json:"text"`
	SchemaOnly bool              `json:"schemaOnly"`
	Findings   []expectedFinding `json:"findings"`
}

func readFixture(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatalf("read the shared table %s — layout drift?: %v", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

func TestManifestCasesSharedWithKit(t *testing.T) {
	var table struct {
		Base  json.RawMessage `json:"base"`
		Cases []manifestCase  `json:"cases"`
	}
	readFixture(t, "manifest-cases.json", &table)
	if len(table.Cases) == 0 {
		t.Fatal("the shared manifest table is empty")
	}
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var raw []byte
			switch {
			case c.Text != nil:
				raw = []byte(*c.Text)
			case c.Document != nil:
				raw = c.Document
			default:
				raw = patched(t, table.Base, c.Patch)
			}
			manifest, got := ParseManifest(raw)
			if (manifest == nil) != (len(got) > 0) {
				t.Fatalf("ParseManifest returned manifest=%v with %d findings", manifest != nil, len(got))
			}
			if c.SchemaOnly {
				// Only the code is shared: each side words and places its own
				// schema violations, and this side reports the first.
				if len(got) == 0 || got[0].Code != CodeSchemaViolation {
					t.Fatalf("findings = %+v, want a %s", got, CodeSchemaViolation)
				}
				return
			}
			if len(got) != len(c.Findings) {
				t.Fatalf("findings = %+v, want %+v", got, c.Findings)
			}
			for i, want := range c.Findings {
				if got[i].Code != want.Code || got[i].Location != want.Location || got[i].Message != want.Message {
					t.Errorf("finding %d = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

// patched applies a case's patch to a fresh copy of the base document, exactly
// as test/patch.ts does on the TypeScript side: a set one past an array's end
// appends, a remove from an array splices.
func patched(t *testing.T, base json.RawMessage, patch []patchOp) []byte {
	t.Helper()
	var doc any
	if err := json.Unmarshal(base, &doc); err != nil {
		t.Fatal(err)
	}
	for _, op := range patch {
		doc = apply(t, doc, op)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func apply(t *testing.T, doc any, op patchOp) any {
	t.Helper()
	return applyAt(t, doc, op.Path, op)
}

func applyAt(t *testing.T, node any, path []any, op patchOp) any {
	t.Helper()
	seg, last := path[0], len(path) == 1
	switch n := node.(type) {
	case map[string]any:
		key := seg.(string)
		switch {
		case !last:
			n[key] = applyAt(t, n[key], path[1:], op)
		case op.Op == "set":
			n[key] = op.Value
		default:
			delete(n, key)
		}
		return n
	case []any:
		i := int(seg.(float64))
		switch {
		case !last:
			n[i] = applyAt(t, n[i], path[1:], op)
		case op.Op != "set":
			return append(n[:i:i], n[i+1:]...)
		case i == len(n):
			return append(n, op.Value)
		default:
			n[i] = op.Value
		}
		return n
	}
	t.Fatalf("patch path %v does not lead through the document", path)
	return nil
}

type sourceCase struct {
	Name     string            `json:"name"`
	Source   string            `json:"source"`
	PadTo    int               `json:"padTo"`
	Findings []expectedFinding `json:"findings"`
}

func TestSourceFloorCasesSharedWithKit(t *testing.T) {
	var table struct {
		MaxSourceLength int          `json:"maxSourceLength"`
		Cases           []sourceCase `json:"cases"`
	}
	readFixture(t, "source-floor-cases.json", &table)
	if table.MaxSourceLength != MaxSourceLength {
		t.Fatalf("MaxSourceLength = %d, the kit's table says %d", MaxSourceLength, table.MaxSourceLength)
	}
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			source := c.Source
			if c.PadTo > 0 {
				// The same padding test/source-floor-parity.test.ts adds: a
				// trailing comment, counted in UTF-16 code units.
				source += "//" + strings.Repeat("x", c.PadTo-len(utf16.Encode([]rune(source)))-2)
			}
			got := CheckSource(source)
			if len(got) != len(c.Findings) {
				t.Fatalf("findings = %+v, want %+v", got, c.Findings)
			}
			for i, want := range c.Findings {
				if got[i].Code != want.Code || got[i].Location != want.Location {
					t.Errorf("finding %d = %s at %s (%s), want %s at %s", i, got[i].Code, got[i].Location, got[i].Message, want.Code, want.Location)
				}
			}
		})
	}
}
