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
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/platform/contracttest"
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
	if !slices.Equal(orgconfig.AgentRuntimes, want) {
		t.Errorf("AgentRuntimes = %v, contract AgentRuntime enum = %v", orgconfig.AgentRuntimes, want)
	}
}

func TestCodingAgentModelsMatchTheContract(t *testing.T) {
	want := enumFromContract(t, "CodingAgentModel")
	if !slices.Equal(orgconfig.CodingAgentModels, want) {
		t.Errorf("CodingAgentModels = %v, contract CodingAgentModel enum = %v", orgconfig.CodingAgentModels, want)
	}
}

// Membership of the enum is not availability — the contract says so in prose and
// this is the assertion behind it. A supported runtime the enum does not carry
// could never be selected; the reverse is the ordinary state today.
func TestSupportedRuntimesAreASubsetOfTheEnum(t *testing.T) {
	if len(orgconfig.SupportedAgentRuntimes) == 0 {
		t.Fatal("no runtime is supported: every organization's dispatch would be unrunnable")
	}
	for _, r := range orgconfig.SupportedAgentRuntimes {
		if !slices.Contains(orgconfig.AgentRuntimes, r) {
			t.Errorf("supported runtime %q is not in the contract's AgentRuntime enum", r)
		}
	}
}

// An org that never opens the setting runs on these, so a default outside the
// enum would make every such org's dispatch invalid — and an UNSUPPORTED default
// would make it unrunnable.
func TestDefaultsAreSelectableAndSupported(t *testing.T) {
	def := orgconfig.DefaultCodingAgent()
	if !slices.Contains(orgconfig.SupportedAgentRuntimes, def.Runtime) {
		t.Errorf("default runtime %q is not one this build can run", def.Runtime)
	}
	if !slices.Contains(orgconfig.CodingAgentModels, def.Model) {
		t.Errorf("default model %q is not in the contract's CodingAgentModel enum", def.Model)
	}
	// Nobody chose them — which is exactly what the console reads to tell "on
	// the defaults" apart from "chose the defaults".
	if def.UpdatedAt != nil || def.UpdatedBy != nil {
		t.Errorf("the default projection claims an author: updatedAt=%v updatedBy=%v", def.UpdatedAt, def.UpdatedBy)
	}
}
