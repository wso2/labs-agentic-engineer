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

package mcp

import (
	"reflect"
	"testing"
)

// The pod's list is pinned: changing it is a reviewed decision, not
// a side effect of what aep-api serves.
func TestAllowedTools_Pinned(t *testing.T) {
	want := []string{
		"list_external_resources", "get_external_resource_schema", "list_org_endpoints",
		"list_org_component_endpoints", "list_platform_resource_types", "list_groups",
		"list_guardrail_policies",
		"get_remote_git_file_contents", "search_remote_git_code",
		"validate_openapi_spec", "fetch_openapi_spec", "slice_openapi_spec",
	}
	if !reflect.DeepEqual(AllowedTools, want) {
		t.Fatalf("AllowedTools = %v", AllowedTools)
	}
}

// Every allowed tool has exactly one descriptor, in list order, with an
// object input schema the model can call against.
func TestTools_DescribeExactlyTheAllowedTools(t *testing.T) {
	tools := Tools()
	if len(tools) != len(AllowedTools) {
		t.Fatalf("descriptors = %d", len(tools))
	}
	for i, tl := range tools {
		if tl.Name != AllowedTools[i] {
			t.Fatalf("descriptor %d = %s, want %s", i, tl.Name, AllowedTools[i])
		}
		if tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Fatalf("%s: incomplete descriptor", tl.Name)
		}
	}
}

func TestRemoteGitTools_AreAllowed(t *testing.T) {
	for _, name := range []string{toolGetFileContents, toolSearchCode} {
		if !allowed(name) {
			t.Fatalf("%s not in the allow-list", name)
		}
	}
	if allowed("drop_database") || allowed("") {
		t.Fatal("an unlisted name is allowed")
	}
}
