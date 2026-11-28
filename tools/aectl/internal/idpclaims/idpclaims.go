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

// Package idpclaims resolves the platform IdP's claim names and values: the
// platform.idp.claims block of aectl's config, which says how a token names the
// caller's org and whether it was issued to a machine or to a user. No
// standard defines those, so each IdP has its own; aectl passes the resolved
// values to the chart, and every reader of the IdP's tokens (aep-api, the AE
// Studio pod, the console) reads by them and knows no IdP.
//
// A value comes from two layers, lowest first: a preset shipped with aectl
// (a data file per IdP AE has been run against), then the config's
// platform.idp.claims.* keys. The preset names live only here: nothing downstream of
// aectl sees one.
package idpclaims

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed presets/*.yaml
var presetFiles embed.FS

// DefaultPreset is the preset an install starts from when it names none: the
// ThunderID aectl installs by default.
const DefaultPreset = "thunder"

// NoPreset is the preset name that starts from nothing: every required value
// then has to be set in the config. It is for an IdP aectl ships no preset
// for, so that a value left out is an error and not another IdP's name.
const NoPreset = "none"

// Field is one value of the platform.idp.claims block.
type Field struct {
	// Key is the field's config key, under platform.idp.claims (for example
	// "org.handle"); "platform.idp.claims."+Key is the whole key.
	Key string
	// Required marks a value a token reader cannot work without.
	Required bool
}

// Fields are the values of the platform.idp.claims block, in the order they are shown.
var Fields = []Field{
	{Key: "org.handle", Required: true},
	{Key: "org.id", Required: true},
	{Key: "org.name"},
	{Key: "caller_type.claim", Required: true},
	{Key: "caller_type.machine", Required: true},
	{Key: "caller_type.user"},
}

// Claims are the resolved values. Org* and CallerType are names of top-level
// text claims; Machine and User are values of the CallerType claim. OrgName
// and User may be empty: the org's display name is optional, and with no
// User value any value other than Machine is read as a user's.
type Claims struct {
	OrgHandle, OrgID, OrgName string
	CallerType, Machine, User string
}

// Source says which layer a resolved value came from.
type Source string

// The layers, as shown to the operator.
const (
	SourceConfig Source = "config"
	// SourceUnset is an optional value no layer set.
	SourceUnset Source = "not set"
)

// SourcePreset is the source of a value taken from the named preset.
func SourcePreset(name string) Source { return Source("preset " + name) }

// Value is one resolved value and where it came from.
type Value struct {
	Field
	Value  string
	Source Source
}

// Resolved is the platform.idp.claims block after layering.
type Resolved struct {
	Preset string
	Values []Value
}

// Override answers the value a higher layer sets for a field's key, and that
// layer's source; "" means the layer does not set it. The caller supplies it
// from the config.
type Override func(key string) (value string, source Source)

// ErrUnknownPreset is a preset name aectl does not ship.
var ErrUnknownPreset = errors.New("unknown preset")

// Presets lists the values platform.idp.claims.preset takes: the shipped presets,
// sorted, then NoPreset.
func Presets() []string {
	entries, _ := presetFiles.ReadDir("presets")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return append(names, NoPreset)
}

// presetValues reads a preset file into field key → value. NoPreset is
// empty.
func presetValues(name string) (map[string]string, error) {
	if name == NoPreset {
		return map[string]string{}, nil
	}
	raw, err := presetFiles.ReadFile("presets/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("platform.idp.claims.preset: %w %q (shipped: %s)", ErrUnknownPreset, name, strings.Join(Presets(), ", "))
	}
	var doc map[string]map[string]string
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("preset %s: %w", name, err)
	}
	out := map[string]string{}
	for group, values := range doc {
		for key, value := range values {
			out[group+"."+key] = value
		}
	}
	return out, nil
}

// Resolve layers the platform.idp.claims block: the preset (DefaultPreset when preset
// is empty), then override. A required value no layer sets is an error
// naming every such key: readers of the IdP's tokens refuse an incomplete
// set, so the install stops here instead. A shipped preset is complete, so
// that only happens under NoPreset.
func Resolve(preset string, override Override) (Resolved, error) {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		preset = DefaultPreset
	}
	base, err := presetValues(preset)
	if err != nil {
		return Resolved{}, err
	}
	res := Resolved{Preset: preset}
	var missing []string
	for _, f := range Fields {
		v := Value{Field: f, Value: base[f.Key], Source: SourcePreset(preset)}
		if over, src := override(f.Key); strings.TrimSpace(over) != "" {
			v.Value, v.Source = strings.TrimSpace(over), src
		}
		if v.Value == "" {
			v.Source = SourceUnset
			if f.Required {
				missing = append(missing, "platform.idp.claims."+f.Key)
			}
		}
		res.Values = append(res.Values, v)
	}
	if len(missing) != 0 {
		return Resolved{}, fmt.Errorf("the platform IdP's claims are incomplete: %s not set", strings.Join(missing, ", "))
	}
	return res, nil
}

// Claims is the resolved block as values.
func (r Resolved) Claims() Claims {
	get := map[string]string{}
	for _, v := range r.Values {
		get[v.Key] = v.Value
	}
	return Claims{
		OrgHandle: get["org.handle"], OrgID: get["org.id"], OrgName: get["org.name"],
		CallerType: get["caller_type.claim"], Machine: get["caller_type.machine"], User: get["caller_type.user"],
	}
}

// Describe is the resolved block as lines for the operator: each value with
// the layer it came from, and what an unset user value means. The values are
// claim names, never secrets.
func (r Resolved) Describe() []string {
	var lines []string
	for _, v := range r.Values {
		if v.Value == "" {
			lines = append(lines, fmt.Sprintf("platform.idp.claims.%-20s %-20s %s", v.Key, "(none)", v.Source))
			continue
		}
		lines = append(lines, fmt.Sprintf("platform.idp.claims.%-20s %-20s from %s", v.Key, v.Value, v.Source))
	}
	if c := r.Claims(); c.User == "" {
		lines = append(lines, fmt.Sprintf("no caller_type.user value: any %s other than %q is read as a user's", c.CallerType, c.Machine))
	}
	return lines
}
