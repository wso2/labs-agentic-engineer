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

package jsonschema

import (
	"encoding/json"
	"reflect"
	"testing"
)

// tree is a recursive discriminated union in the shape Zod renders one:
// `$defs` + local `$ref`s, `oneOf` branches pinned by a `kind` const, a
// string map, and integer bounds.
const tree = `{
  "type": "object",
  "properties": { "nodes": { "$ref": "#/$defs/List" } },
  "required": ["nodes"],
  "additionalProperties": false,
  "$defs": {
    "List": { "type": "array", "items": { "$ref": "#/$defs/Node" } },
    "Node": {
      "oneOf": [
        {
          "type": "object",
          "properties": {
            "kind": { "type": "string", "const": "box" },
            "children": { "$ref": "#/$defs/List" },
            "span": { "type": "integer", "minimum": 1, "maximum": 4 }
          },
          "required": ["kind", "children"],
          "additionalProperties": false
        },
        {
          "type": "object",
          "properties": {
            "kind": { "type": "string", "const": "row" },
            "values": {
              "type": "object",
              "propertyNames": { "type": "string" },
              "additionalProperties": { "type": "string" }
            }
          },
          "required": ["kind", "values"],
          "additionalProperties": false
        }
      ]
    }
  }
}`

func check(t *testing.T, doc string) []Issue {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("bad test document: %v", err)
	}
	return Check(v, MustParse([]byte(tree)))
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want []Issue
	}{
		{"a valid recursive document", `{"nodes":[{"kind":"box","span":2,"children":[{"kind":"row","values":{"a":"1"}}]}]}`, []Issue{}},
		{"the intended branch's complaint, deep in the tree",
			`{"nodes":[{"kind":"box","children":[{"kind":"box","children":[],"extra":1}]}]}`,
			[]Issue{{Path: "nodes[0].children[0]", Message: "unknown property extra"}}},
		{"a kind no branch declares is named at the discriminator",
			`{"nodes":[{"kind":"carousel"}]}`,
			[]Issue{{Path: "nodes[0].kind", Message: "is not one of the allowed kinds"}}},
		{"a string map's values are checked",
			`{"nodes":[{"kind":"row","values":{"a":1}}]}`,
			[]Issue{{Path: "nodes[0].values.a", Message: "must be a string"}}},
		{"an integer below its minimum",
			`{"nodes":[{"kind":"box","span":0,"children":[]}]}`,
			[]Issue{{Path: "nodes[0].span", Message: "must be at least 1"}}},
		{"an integer above its maximum",
			`{"nodes":[{"kind":"box","span":5,"children":[]}]}`,
			[]Issue{{Path: "nodes[0].span", Message: "must be at most 4"}}},
		{"a union member that is not an object",
			`{"nodes":["box"]}`,
			[]Issue{{Path: "nodes[0]", Message: "does not match any allowed shape"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := check(t, c.doc)
			if got == nil {
				got = []Issue{}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// Validate keeps its `path: message` spelling for the gates that report one
// string per file.
func TestValidateSpellsThePathIntoTheMessage(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`{"nodes":[{"kind":"row","values":{"a":1}}]}`), &v)
	got := Validate(v, MustParse([]byte(tree)))
	if len(got) != 1 || got[0] != "nodes[0].values.a: must be a string" {
		t.Fatalf("got %q", got)
	}
}

func TestMustParseRefusesADanglingRef(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a $ref to a missing definition must panic at parse time")
		}
	}()
	MustParse([]byte(`{"type":"array","items":{"$ref":"#/$defs/Missing"}}`))
}

func TestUnsupportedKeywordsSeesThroughDefs(t *testing.T) {
	got := UnsupportedKeywords([]byte(`{"$defs":{"A":{"type":"string","pattern":"^a$"}},"$ref":"#/$defs/A"}`))
	if !reflect.DeepEqual(got, []string{"pattern"}) {
		t.Fatalf("got %v, want [pattern]", got)
	}
}
