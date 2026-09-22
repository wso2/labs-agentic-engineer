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
// consuming package (designspec, securityspec, prototypespec) vendors its own
// schema and calls in here.
//
// It supports exactly the keywords those artifacts use, and no more — an
// unsupported keyword is IGNORED, not an error, so adding one to a Zod schema
// silently weakens the Go side. `TestSupportsEveryKeyword` in each consuming
// package walks its vendored schema and fails on a keyword this file does not
// implement, which is what makes that weakening loud.
//
// Recursive schemas (the prototype model's node tree) render as `$defs` plus
// local `$ref`s; MustParse links every `$ref` to its definition once, so
// validation follows a pointer rather than resolving a string per node.
package jsonschema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Schema is one node of a parsed schema document.
type Schema struct {
	Type       string             `json:"type"`
	Properties map[string]*Schema `json:"properties"`
	Required   []string           `json:"required"`
	// AdditionalProperties is either a boolean or a subschema object (draft
	// 2020-12), so it is decoded raw: the literal `false` triggers the strict
	// "no unknown properties" check, a subschema (a string-map property) is
	// what every undeclared property's value must satisfy, and `true` admits
	// anything.
	AdditionalProperties json.RawMessage `json:"additionalProperties"`
	// PropertyNames is what every key of an object must satisfy (a string-map
	// property's key schema).
	PropertyNames *Schema `json:"propertyNames"`
	Enum          []any   `json:"enum"`
	// Const pins a single allowed value (z.literal). Compared structurally
	// against the decoded JSON value.
	Const     json.RawMessage `json:"const"`
	MinLength *int            `json:"minLength"`
	MaxLength *int            `json:"maxLength"`
	MinItems  *int            `json:"minItems"`
	Minimum   *float64        `json:"minimum"`
	Maximum   *float64        `json:"maximum"`
	Items     *Schema         `json:"items"`
	// AnyOf is how a nullable or union-typed field renders (z.string().nullable()
	// becomes anyOf[{type:string},{type:null}]). The value must satisfy at least
	// one branch.
	AnyOf []*Schema `json:"anyOf"`
	// OneOf is how a discriminated union renders (z.discriminatedUnion). The
	// value must satisfy exactly one branch.
	OneOf []*Schema `json:"oneOf"`
	// Ref is a local reference (`#/$defs/<name>`) into the root's Defs; MustParse
	// resolves it into ref.
	Ref  string             `json:"$ref"`
	Defs map[string]*Schema `json:"$defs"`

	additional *Schema // AdditionalProperties when it is a subschema
	ref        *Schema // the definition Ref names
}

// SupportedKeywords is every keyword this interpreter acts on. A consuming
// package's vendored-schema test walks its artifact against this set so a Zod
// change that emits something new fails loudly instead of silently validating
// less. `$schema` is a document annotation, not a constraint, and is listed so
// the walk does not trip on it.
var SupportedKeywords = map[string]bool{
	"$schema":              true,
	"$defs":                true,
	"$ref":                 true,
	"type":                 true,
	"properties":           true,
	"required":             true,
	"additionalProperties": true,
	"propertyNames":        true,
	"enum":                 true,
	"const":                true,
	"minLength":            true,
	"maxLength":            true,
	"minItems":             true,
	"minimum":              true,
	"maximum":              true,
	"items":                true,
	"anyOf":                true,
	"oneOf":                true,
	// Emitted by Zod for `z.number().int().positive()` alongside `type:integer`.
	// The integer type check already rejects the shapes that matter here (a
	// string, an object) and the referential checks own the rest, so it is
	// deliberately accepted-and-ignored rather than left to trip the walk.
	"exclusiveMinimum": true,
}

// MustParse decodes a schema document and links its `$ref`s, panicking on
// malformed input or a reference to a definition the document does not hold.
// Callers embed a checked-in artifact, so either is a build-time defect.
func MustParse(raw []byte) *Schema {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		panic("jsonschema: cannot parse embedded schema: " + err.Error())
	}
	if err := link(&s, &s, map[*Schema]bool{}); err != nil {
		panic("jsonschema: " + err.Error())
	}
	return &s
}

// link resolves every `$ref` and decodes every subschema-valued
// additionalProperties under node. Definitions are linked through root.Defs
// like any other subtree; `seen` stops a node being walked twice.
func link(node, root *Schema, seen map[*Schema]bool) error {
	if node == nil || seen[node] {
		return nil
	}
	seen[node] = true
	if node.Ref != "" {
		name, ok := strings.CutPrefix(node.Ref, "#/$defs/")
		target := root.Defs[name]
		if !ok || target == nil {
			return fmt.Errorf("unresolvable $ref %q", node.Ref)
		}
		node.ref = target
	}
	if raw := strings.TrimSpace(string(node.AdditionalProperties)); strings.HasPrefix(raw, "{") {
		var sub Schema
		if err := json.Unmarshal(node.AdditionalProperties, &sub); err != nil {
			return fmt.Errorf("additionalProperties: %w", err)
		}
		node.additional = &sub
	}
	children := []*Schema{node.Items, node.PropertyNames, node.additional}
	children = append(children, node.AnyOf...)
	children = append(children, node.OneOf...)
	for _, p := range node.Properties {
		children = append(children, p)
	}
	for _, d := range node.Defs {
		children = append(children, d)
	}
	for _, c := range children {
		if err := link(c, root, seen); err != nil {
			return err
		}
	}
	return nil
}

// Issue is one schema violation: where, as a path (`screens[0].content[2]`,
// empty for the root), and what is wrong.
type Issue struct {
	Path    string
	Message string
}

// Validate returns the schema-violation messages for value (empty on success),
// each spelled `path: message`. It stops at the first, most-specific failure
// per node so a caller can report one actionable message.
func Validate(value any, s *Schema) []string {
	issues := Check(value, s)
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, at(i.Path)+i.Message)
	}
	return out
}

// Check is Validate with the path and the message kept apart, for a gate that
// reports each finding at its JSON path.
func Check(value any, s *Schema) []Issue { return validate(value, s, "") }

func fail(path, format string, args ...any) []Issue {
	return []Issue{{Path: path, Message: fmt.Sprintf(format, args...)}}
}

func validate(value any, s *Schema, path string) []Issue {
	if s == nil {
		return nil
	}
	if s.ref != nil {
		if issues := validate(value, s.ref, path); len(issues) > 0 {
			return issues
		}
	}
	if len(s.Const) > 0 {
		if issues := validateConst(value, s, path); len(issues) > 0 {
			return issues
		}
	}
	if len(s.AnyOf) > 0 {
		if issues := validateAnyOf(value, s, path); len(issues) > 0 {
			return issues
		}
	}
	if len(s.OneOf) > 0 {
		if issues := validateOneOf(value, s, path); len(issues) > 0 {
			return issues
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
			return fail(path, "must be a boolean")
		}
	case "integer":
		f, ok := value.(float64)
		if !ok || f != float64(int64(f)) {
			return fail(path, "must be an integer")
		}
		return validateBounds(f, s, path)
	case "number":
		f, ok := value.(float64)
		if !ok {
			return fail(path, "must be a number")
		}
		return validateBounds(f, s, path)
	case "null":
		if value != nil {
			return fail(path, "must be null")
		}
	}
	return nil
}

// validateConst compares the value against the pinned literal. The comparison is
// on re-encoded JSON so it works for any literal kind without a type switch.
func validateConst(value any, s *Schema, path string) []Issue {
	want := string(s.Const)
	got, err := json.Marshal(value)
	if err != nil || string(got) != want {
		return fail(path, "must be %s", want)
	}
	return nil
}

// validateAnyOf accepts the value when at least one branch accepts it. Branch
// messages are discarded: reporting "failed all of N branches" with every
// branch's complaint is noise, and the branches of the schemas we publish are
// alternatives of shape (a string, or null), not of meaning.
func validateAnyOf(value any, s *Schema, path string) []Issue {
	for _, branch := range s.AnyOf {
		if len(validate(value, branch, path)) == 0 {
			return nil
		}
	}
	return fail(path, "does not match any allowed shape")
}

// validateOneOf accepts the value when exactly one branch accepts it. The
// branches we publish are discriminated unions — each pins a `kind` (or
// similar) property to a const — so only the branches whose consts the value
// carries are tried: that keeps a recursive union linear in the document, and
// when the one intended branch refuses, its complaint is the useful one. A
// value that names no branch is told so at its discriminator.
func validateOneOf(value any, s *Schema, path string) []Issue {
	candidates := s.OneOf
	obj, isObject := value.(map[string]any)
	if isObject {
		candidates = nil
		for _, branch := range s.OneOf {
			if !pinsConst(branch) || constsMatch(obj, branch) {
				candidates = append(candidates, branch)
			}
		}
	}
	matched := 0
	var first []Issue
	for _, branch := range candidates {
		issues := validate(value, branch, path)
		if len(issues) == 0 {
			matched++
		} else if first == nil {
			first = issues
		}
	}
	switch {
	case matched == 1:
		return nil
	case matched > 1:
		return fail(path, "matches more than one allowed shape")
	case len(candidates) == 1 && pinsConst(candidates[0]):
		return first
	}
	if key, ok := discriminator(s.OneOf); ok && isObject {
		return fail(join(path, key), "is not one of the allowed kinds")
	}
	return fail(path, "does not match any allowed shape")
}

// pinsConst reports whether branch pins any property to a const.
func pinsConst(branch *Schema) bool {
	for _, prop := range branch.Properties {
		if len(prop.Const) > 0 {
			return true
		}
	}
	return false
}

// constsMatch reports whether obj carries every const-pinned property of
// branch with the pinned value.
func constsMatch(obj map[string]any, branch *Schema) bool {
	for name, prop := range branch.Properties {
		if len(prop.Const) > 0 && len(validateConst(obj[name], prop, "")) > 0 {
			return false
		}
	}
	return true
}

// discriminator is the one property every branch pins to a const, when there
// is one.
func discriminator(branches []*Schema) (string, bool) {
	var key string
	for i, branch := range branches {
		var pinned []string
		for name, prop := range branch.Properties {
			if len(prop.Const) > 0 {
				pinned = append(pinned, name)
			}
		}
		if len(pinned) != 1 || (i > 0 && pinned[0] != key) {
			return "", false
		}
		key = pinned[0]
	}
	return key, key != ""
}

func validateObject(value any, s *Schema, path string) []Issue {
	obj, ok := value.(map[string]any)
	if !ok {
		return fail(path, "must be an object")
	}
	for _, req := range s.Required {
		if _, present := obj[req]; !present {
			return fail(path, "missing required property %s", req)
		}
	}
	strict := string(s.AdditionalProperties) == "false"
	for _, k := range sortedKeys(obj) {
		v := obj[k]
		if s.PropertyNames != nil {
			if issues := validate(k, s.PropertyNames, join(path, k)); len(issues) > 0 {
				return issues
			}
		}
		if _, declared := s.Properties[k]; declared {
			continue
		}
		if strict {
			return fail(path, "unknown property %s", k)
		}
		if s.additional != nil {
			if issues := validate(v, s.additional, join(path, k)); len(issues) > 0 {
				return issues
			}
		}
	}
	for _, name := range sortedKeys(s.Properties) {
		if v, present := obj[name]; present {
			if issues := validate(v, s.Properties[name], join(path, name)); len(issues) > 0 {
				return issues
			}
		}
	}
	return nil
}

func validateArray(value any, s *Schema, path string) []Issue {
	arr, ok := value.([]any)
	if !ok {
		return fail(path, "must be an array")
	}
	if s.MinItems != nil && len(arr) < *s.MinItems {
		return fail(path, "must have at least %d items", *s.MinItems)
	}
	for i, item := range arr {
		if issues := validate(item, s.Items, fmt.Sprintf("%s[%d]", path, i)); len(issues) > 0 {
			return issues
		}
	}
	return nil
}

func validateString(value any, s *Schema, path string) []Issue {
	str, ok := value.(string)
	if !ok {
		return fail(path, "must be a string")
	}
	if s.MinLength != nil && len(str) < *s.MinLength {
		return fail(path, "must be at least %d characters", *s.MinLength)
	}
	if s.MaxLength != nil && len(str) > *s.MaxLength {
		return fail(path, "must be at most %d characters", *s.MaxLength)
	}
	if len(s.Enum) > 0 && !enumContains(s.Enum, str) {
		return fail(path, "%q is not an allowed value", str)
	}
	return nil
}

func validateBounds(f float64, s *Schema, path string) []Issue {
	if s.Minimum != nil && f < *s.Minimum {
		return fail(path, "must be at least %v", *s.Minimum)
	}
	if s.Maximum != nil && f > *s.Maximum {
		return fail(path, "must be at most %v", *s.Maximum)
	}
	return nil
}

// sortedKeys makes "the first failure" the same failure on every run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
// node. `properties` and `$defs` maps carry USER keys (a design field named
// "type" is not the `type` keyword), so their values are walked but their keys
// are not.
//
//deadcode:keep test seam — the recursive half of UnsupportedKeywords, which is
func walkKeywords(node any, seen map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for k, sub := range v {
			seen[k] = true
			if k == "properties" || k == "$defs" {
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
