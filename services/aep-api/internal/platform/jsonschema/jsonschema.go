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

// Package jsonschema is a compact draft-2020-12 interpreter for the JSON Schema
// artifacts published from the shared Zod definitions
// (packages/contracts/schemas/*.schema.json).
//
// Driving validation from the published file — rather than hand-coding a Go
// mirror of each schema — is what keeps the "one definition" invariant
// (docs/design/agents-generation-migration.md §8): the agent's author-time write
// gate and the BFF's save gate read the SAME bytes. This package exists so a
// second gated artifact does not mean a second copy of the interpreter; each
// consuming package (designspec, rolesspec) vendors its own schema and calls in
// here.
//
// It supports exactly the keywords those artifacts use, and no more — an
// unsupported keyword is IGNORED, not an error, so adding one to a Zod schema
// silently weakens the Go side. `TestSupportsEveryKeyword` in each consuming
// package walks its vendored schema and fails on a keyword this file does not
// implement, which is what makes that weakening loud.
package jsonschema

import (
	"encoding/json"
	"fmt"
)

// Schema is one node of a parsed schema document.
type Schema struct {
	Type       string             `json:"type"`
	Properties map[string]*Schema `json:"properties"`
	Required   []string           `json:"required"`
	// AdditionalProperties is either a boolean or a subschema object (draft
	// 2020-12), so it is decoded raw: only the literal `false` triggers the
	// strict "no unknown properties" check; `true` or any subschema (used by a
	// string-map property) is permissive.
	AdditionalProperties json.RawMessage `json:"additionalProperties"`
	Enum                 []any           `json:"enum"`
	// Const pins a single allowed value (z.literal). Compared structurally
	// against the decoded JSON value.
	Const     json.RawMessage `json:"const"`
	MinLength *int            `json:"minLength"`
	MinItems  *int            `json:"minItems"`
	Items     *Schema         `json:"items"`
	// AnyOf is how a nullable or union-typed field renders (z.string().nullable()
	// becomes anyOf[{type:string},{type:null}]). The value must satisfy at least
	// one branch.
	AnyOf []*Schema `json:"anyOf"`
}

// SupportedKeywords is every keyword this interpreter acts on. A consuming
// package's vendored-schema test walks its artifact against this set so a Zod
// change that emits something new fails loudly instead of silently validating
// less. `$schema` is a document annotation, not a constraint, and is listed so
// the walk does not trip on it.
var SupportedKeywords = map[string]bool{
	"$schema":              true,
	"type":                 true,
	"properties":           true,
	"required":             true,
	"additionalProperties": true,
	"enum":                 true,
	"const":                true,
	"minLength":            true,
	"minItems":             true,
	"items":                true,
	"anyOf":                true,
	// Emitted by Zod for `z.number().int().positive()` alongside `type:integer`.
	// The integer type check already rejects the shapes that matter here (a
	// string, an object) and the referential checks own the rest, so these are
	// deliberately accepted-and-ignored rather than left to trip the walk.
	"exclusiveMinimum": true,
	"maximum":          true,
	"minimum":          true,
}

// MustParse decodes a schema document, panicking on malformed input. Callers
// embed a checked-in artifact, so a parse failure is a build-time defect.
func MustParse(raw []byte) *Schema {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		panic("jsonschema: cannot parse embedded schema: " + err.Error())
	}
	return &s
}

// Validate returns the schema-violation messages for value (empty on success).
// It stops at the first, most-specific failure per node so a caller can report
// one actionable message.
func Validate(value any, s *Schema) []string { return validate(value, s, "") }

func validate(value any, s *Schema, path string) []string {
	if s == nil {
		return nil
	}
	if len(s.Const) > 0 {
		if msgs := validateConst(value, s, path); len(msgs) > 0 {
			return msgs
		}
	}
	if len(s.AnyOf) > 0 {
		if msgs := validateAnyOf(value, s, path); len(msgs) > 0 {
			return msgs
		}
	}
	switch s.Type {
	case "object":
		return validateObject(value, s, path)
	case "array":
		return validateArray(value, s, path)
	case "string":
		return validateString(value, s, path)
	case "boolean":
		if _, ok := value.(bool); !ok {
			return []string{at(path) + "must be a boolean"}
		}
	case "integer":
		f, ok := value.(float64)
		if !ok {
			return []string{at(path) + "must be an integer"}
		}
		if f != float64(int64(f)) {
			return []string{at(path) + "must be an integer"}
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return []string{at(path) + "must be a number"}
		}
	case "null":
		if value != nil {
			return []string{at(path) + "must be null"}
		}
	}
	return nil
}

// validateConst compares the value against the pinned literal. The comparison is
// on re-encoded JSON so it works for any literal kind without a type switch.
func validateConst(value any, s *Schema, path string) []string {
	want := string(s.Const)
	got, err := json.Marshal(value)
	if err != nil || string(got) != want {
		return []string{at(path) + fmt.Sprintf("must be %s", want)}
	}
	return nil
}

// validateAnyOf accepts the value when at least one branch accepts it. Branch
// messages are discarded: reporting "failed all of N branches" with every
// branch's complaint is noise, and the branches of the schemas we publish are
// alternatives of shape (a string, or null), not of meaning.
func validateAnyOf(value any, s *Schema, path string) []string {
	for _, branch := range s.AnyOf {
		if len(validate(value, branch, path)) == 0 {
			return nil
		}
	}
	return []string{at(path) + "does not match any allowed shape"}
}

func validateObject(value any, s *Schema, path string) []string {
	obj, ok := value.(map[string]any)
	if !ok {
		return []string{at(path) + "must be an object"}
	}
	for _, req := range s.Required {
		if _, present := obj[req]; !present {
			return []string{at(path) + "missing required property " + req}
		}
	}
	if string(s.AdditionalProperties) == "false" {
		for k := range obj {
			if _, declared := s.Properties[k]; !declared {
				return []string{at(path) + "unknown property " + k}
			}
		}
	}
	for name, sub := range s.Properties {
		if v, present := obj[name]; present {
			if msgs := validate(v, sub, join(path, name)); len(msgs) > 0 {
				return msgs
			}
		}
	}
	return nil
}

func validateArray(value any, s *Schema, path string) []string {
	arr, ok := value.([]any)
	if !ok {
		return []string{at(path) + "must be an array"}
	}
	if s.MinItems != nil && len(arr) < *s.MinItems {
		return []string{at(path) + fmt.Sprintf("must have at least %d items", *s.MinItems)}
	}
	for i, item := range arr {
		if msgs := validate(item, s.Items, fmt.Sprintf("%s[%d]", path, i)); len(msgs) > 0 {
			return msgs
		}
	}
	return nil
}

func validateString(value any, s *Schema, path string) []string {
	str, ok := value.(string)
	if !ok {
		return []string{at(path) + "must be a string"}
	}
	if s.MinLength != nil && len(str) < *s.MinLength {
		return []string{at(path) + fmt.Sprintf("must be at least %d characters", *s.MinLength)}
	}
	if len(s.Enum) > 0 && !enumContains(s.Enum, str) {
		return []string{at(path) + fmt.Sprintf("%q is not an allowed value", str)}
	}
	return nil
}

func enumContains(enum []any, v string) bool {
	for _, e := range enum {
		if es, ok := e.(string); ok && es == v {
			return true
		}
	}
	return false
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func at(path string) string {
	if path == "" {
		return ""
	}
	return path + ": "
}

// this to prove its artifact uses no keyword the interpreter silently ignores.
// It has no production caller by design: the check belongs in CI, not on a
// request path.
//
// UnsupportedKeywords walks a raw schema document and returns every keyword it
// uses that this interpreter does not act on, deepest-first order unspecified.
// Consuming packages assert this is empty for their vendored artifact.
//
//deadcode:keep test seam — each consuming package's vendored-schema test calls
func UnsupportedKeywords(raw []byte) []string {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []string{"(document does not parse)"}
	}
	seen := map[string]bool{}
	walkKeywords(doc, seen)
	var out []string
	for k := range seen {
		if !SupportedKeywords[k] {
			out = append(out, k)
		}
	}
	return out
}

// itself a CI-only check (see its marker).
//
// walkKeywords collects the property names of every object that is a schema
// node. `properties` maps carry USER keys (a design field named "type" is not
// the `type` keyword), so its values are walked but its keys are not.
//
//deadcode:keep test seam — the recursive half of UnsupportedKeywords, which is
func walkKeywords(node any, seen map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for k, sub := range v {
			seen[k] = true
			if k == "properties" {
				if props, ok := sub.(map[string]any); ok {
					for _, p := range props {
						walkKeywords(p, seen)
					}
				}
				continue
			}
			// enum/const/required hold VALUES, not nested schemas.
			if k == "enum" || k == "const" || k == "required" {
				continue
			}
			walkKeywords(sub, seen)
		}
	case []any:
		for _, item := range v {
			walkKeywords(item, seen)
		}
	}
}
