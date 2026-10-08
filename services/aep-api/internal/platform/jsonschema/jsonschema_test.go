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
	"strings"
	"testing"
)

// The bounds below come from schemas no Zod gate stands in front of (the AI
// gateway's policy catalog), so this interpreter is the only check before a
// value reaches a system that refuses it.
func TestValidate_EnforcesNumericBoundsPatternAndMaxItems(t *testing.T) {
	s := MustParse([]byte(`{"type":"object","properties":{
		"max":   {"type":"integer","minimum":1,"maximum":1000},
		"ratio": {"type":"number","exclusiveMinimum":0,"exclusiveMaximum":1},
		"name":  {"type":"string","pattern":"^[A-Z_]+$"},
		"tags":  {"type":"array","maxItems":2,"items":{"type":"string"}}
	}}`))
	tests := []struct {
		name    string
		value   map[string]any
		wantErr string // "" means valid
	}{
		{"within every bound", map[string]any{"max": 300.0, "ratio": 0.5, "name": "EMAIL", "tags": []any{"a"}}, ""},
		{"below minimum", map[string]any{"max": 0.0}, "max: must be at least 1"},
		{"above maximum", map[string]any{"max": 1001.0}, "max: must be at most 1000"},
		{"at the exclusive minimum", map[string]any{"ratio": 0.0}, "ratio: must be greater than 0"},
		{"at the exclusive maximum", map[string]any{"ratio": 1.0}, "ratio: must be less than 1"},
		{"not matching the pattern", map[string]any{"name": "email"}, "name: must match"},
		{"too many items", map[string]any{"tags": []any{"a", "b", "c"}}, "tags: must have at most 2 items"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgs := Validate(tc.value, s)
			if tc.wantErr == "" {
				if len(msgs) != 0 {
					t.Fatalf("want valid, got %v", msgs)
				}
				return
			}
			if len(msgs) == 0 || !strings.Contains(msgs[0], tc.wantErr) {
				t.Fatalf("messages %v do not contain %q", msgs, tc.wantErr)
			}
		})
	}
}

// A pattern that does not compile cannot be checked, and a check that cannot
// run is reported rather than passed.
func TestValidate_AnUncompilablePatternIsReported(t *testing.T) {
	s := MustParse([]byte(`{"type":"string","pattern":"("}`))
	if msgs := Validate("x", s); len(msgs) == 0 {
		t.Fatal("want a message for a pattern that does not compile")
	}
}
