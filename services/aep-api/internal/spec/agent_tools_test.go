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
)

// afmWithTools builds an afmFrontMatter carrying exactly one
// x-aep.tools.openapi entry naming component/operation — the minimal shape
// ComputeAgentToolStatus needs, mirroring the table-test helpers already used
// for ComputeDependencyStatus.
func afmWithTools(component, operation string) afmFrontMatter {
	return afmFrontMatter{
		Tools: []afmOpenAPITool{
			{Component: component, Allow: []string{operation}},
		},
	}
}

func TestComputeAgentToolStatus(t *testing.T) {
	deps := []Dependency{{Kind: DependencyKindComponent, Name: "lunch-api"}}
	ops := map[string][]string{"lunch-api": {"addItem", "listItems", "closeRound"}}

	t.Run("allowed operation that exists resolves", func(t *testing.T) {
		got := ComputeAgentToolStatus(afmWithTools("lunch-api", "addItem"), deps, ops)
		if len(got) != 1 || got[0].Status != DependencyStatusResolved {
			t.Fatalf("want resolved, got %+v", got)
		}
	})

	t.Run("operation absent from the contract is unresolved", func(t *testing.T) {
		got := ComputeAgentToolStatus(afmWithTools("lunch-api", "addItemm"), deps, ops)
		if got[0].Status != DependencyStatusUnresolved {
			t.Fatalf("want unresolved, got %+v", got[0])
		}
		if !strings.Contains(got[0].Reason, "not an operation") {
			t.Errorf("reason %q should name the cause", got[0].Reason)
		}
	})

	t.Run("component that is not a declared dependency is unresolved", func(t *testing.T) {
		got := ComputeAgentToolStatus(afmWithTools("other-api", "addItem"), deps, ops)
		if got[0].Status != DependencyStatusUnresolved {
			t.Fatalf("want unresolved, got %+v", got[0])
		}
		if !strings.Contains(got[0].Reason, "not a declared") {
			t.Errorf("reason %q should name the cause", got[0].Reason)
		}
	})

	t.Run("declared dependency with no contract in the tree is unchecked", func(t *testing.T) {
		got := ComputeAgentToolStatus(afmWithTools("lunch-api", "addItem"), deps, map[string][]string{})
		if got[0].Status != AgentToolStatusUnchecked {
			t.Fatalf("want unchecked, got %+v", got[0])
		}
	})
}
