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

package orgconfig_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/platform/contracttest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// enumFromContract reads one named schema's `enum` out of the COMMITTED
// contract — the file developers edit, not the copy baked into the binary.
func enumFromContract(t *testing.T, schema string) []string {
	t.Helper()
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Enum []string `yaml:"enum"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(contracttest.SourceYAML(t), &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	s, ok := doc.Components.Schemas[schema]
	if !ok {
		t.Fatalf("contract has no schema %q", schema)
	}
	if len(s.Enum) == 0 {
		t.Fatalf("contract schema %q carries no enum", schema)
	}
	return s.Enum
}

// The request validator rejects a value outside the contract's enum before any
// handler runs, so a Go list that has drifted OUT of the enum is dead code that
// looks like a feature, and one that has drifted AHEAD of it is a value the
// service will happily persist and the edge will never let through. Either way
// the failure is at the edge, far from these lists.
func TestAgentRuntimesMatchTheContract(t *testing.T) {
	want := enumFromContract(t, "AgentRuntime")
	got := make([]string, 0, len(orgconfig.AgentRuntimes))
	for _, r := range orgconfig.AgentRuntimes {
		got = append(got, string(r))
	}
	if !slices.Equal(got, want) {
		t.Errorf("AgentRuntimes = %v, contract AgentRuntime enum = %v", orgconfig.AgentRuntimes, want)
	}
}

// An org that never opens the setting runs on this, so a default outside the
// enum would make every such org's dispatch invalid.
func TestDefaultsAreSelectable(t *testing.T) {
	def := orgconfig.DefaultAgents()
	if !slices.Contains(orgconfig.AgentRuntimes, def.Runtime) {
		t.Errorf("default runtime %q is not in the contract's AgentRuntime enum", def.Runtime)
	}
	if def.Subscription != nil {
		t.Errorf("the default projection carries a subscription: %+v", def.Subscription)
	}
	// Nobody chose them — which is exactly what the console reads to tell "on
	// the defaults" apart from "chose the defaults".
	if def.UpdatedAt != nil || def.UpdatedBy != nil {
		t.Errorf("the default projection claims an author: updatedAt=%v updatedBy=%v", def.UpdatedAt, def.UpdatedBy)
	}
}

// The wire types carry `enum` struct tags that the contract's enums must agree
// with. Nothing reads the tags at runtime (the request validator reads the
// contract), so a tag that drifts is a lie in the one place a reader of the Go
// looks for the allowed values. Pinned field by field, both directions.
func TestAgentsEnumTagsMatchTheContract(t *testing.T) {
	cases := []struct {
		typ    reflect.Type
		field  string
		schema string
	}{
		{reflect.TypeOf(orgconfig.AgentsProjection{}), "Runtime", "AgentRuntime"},
		{reflect.TypeOf(orgconfig.AgentsWrite{}), "Runtime", "AgentRuntime"},
	}
	for _, tc := range cases {
		f, ok := tc.typ.FieldByName(tc.field)
		if !ok {
			t.Fatalf("%s has no field %s", tc.typ.Name(), tc.field)
		}
		got := strings.Split(f.Tag.Get("enum"), ",")
		if want := enumFromContract(t, tc.schema); !slices.Equal(got, want) {
			t.Errorf("%s.%s enum tag = %v, contract %s enum = %v", tc.typ.Name(), tc.field, got, tc.schema, want)
		}
	}
}

// propertyEnumFromContract reads the inline `enum` of schema.property.
func propertyEnumFromContract(t *testing.T, schema, property string) []string {
	t.Helper()
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `yaml:"enum"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(contracttest.SourceYAML(t), &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	p, ok := doc.Components.Schemas[schema].Properties[property]
	if !ok || len(p.Enum) == 0 {
		t.Fatalf("contract schema %s has no enum property %q", schema, property)
	}
	return p.Enum
}

// The llm wire types' enum tags, and the modelconn values behind them, against
// the contract's inline enums: a format or tristate the Go offers and the
// contract does not is a value the edge would refuse.
func TestLLMEnumTagsMatchTheContract(t *testing.T) {
	formats := make([]string, 0, len(modelconn.Formats))
	for _, f := range modelconn.Formats {
		formats = append(formats, string(f.Format))
	}
	cases := []struct {
		typ      reflect.Type
		field    string
		schema   string
		property string
	}{
		{reflect.TypeOf(orgconfig.LLMProjection{}), "Kind", "LLMProjection", "kind"},
		{reflect.TypeOf(orgconfig.LLMPatch{}), "Kind", "LLMPatch", "kind"},
		{reflect.TypeOf(orgconfig.LLMCheck{}), "Kind", "LLMCheck", "kind"},
		{reflect.TypeOf(orgconfig.LLMCheck{}), "ModelListed", "LLMCheck", "modelListed"},
		{reflect.TypeOf(orgconfig.LLMCheck{}), "Warning", "LLMCheck", "warning"},
		{reflect.TypeOf(orgconfig.LLMFormatOption{}), "Kind", "LLMFormatOption", "kind"},
		{reflect.TypeOf(orgconfig.LLMCapabilities{}), "WebSearch", "LLMCapabilities", "webSearch"},
		{reflect.TypeOf(orgconfig.LLMCapabilities{}), "ImageInput", "LLMCapabilities", "imageInput"},
	}
	for _, tc := range cases {
		f, ok := tc.typ.FieldByName(tc.field)
		if !ok {
			t.Fatalf("%s has no field %s", tc.typ.Name(), tc.field)
		}
		got := strings.Split(f.Tag.Get("enum"), ",")
		if want := propertyEnumFromContract(t, tc.schema, tc.property); !slices.Equal(got, want) {
			t.Errorf("%s.%s enum tag = %v, contract %s.%s enum = %v", tc.typ.Name(), tc.field, got, tc.schema, tc.property, want)
		}
	}
	if want := propertyEnumFromContract(t, "LLMPatch", "kind"); !slices.Equal(formats, want) {
		t.Errorf("modelconn.Formats = %v, contract LLMPatch.kind enum = %v", formats, want)
	}
}
