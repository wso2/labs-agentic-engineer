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

package dependencies

import (
	"context"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
)

// stubResourceTypeLister satisfies ResourceTypeLister for registration/spec and
// mapping tests — no request reaches it in the spec test.
type stubResourceTypeLister struct {
	types []resources.PlatformResourceType
	err   error
}

func (s stubResourceTypeLister) List(context.Context) ([]resources.PlatformResourceType, error) {
	return s.types, s.err
}

// TestRegisterResourceTypes_Spec is a registration/spec check: RegisterResourceTypes
// does not panic and the generated OpenAPI carries the discovery operation, its
// path, the org-scoped security scheme, and the DTO schema. It sends no requests
// — the tenant gate defaults to enforce, and (per bff-component-testing) spec
// tests must not flip the request-scoped gate mode; behavioral status/auth
// coverage of the shared chain lives in the componenttest harness.
func TestRegisterResourceTypes_Spec(t *testing.T) {
	t.Parallel()

	_, api := humatest.New(t)
	RegisterResourceTypes(api, stubResourceTypeLister{})

	spec, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	for _, want := range []string{
		"list-platform-resource-types",
		"/dependencies/platform-resource-types",
		"PlatformResourceTypeDTO",
		"userJWT",
	} {
		if !strings.Contains(string(spec), want) {
			t.Fatalf("spec missing %q", want)
		}
	}
}

// TestToPlatformResourceTypeDTOs verifies the domain→DTO projection: the
// architect-facing fields (name, description, parameters, outputs) map through,
// and a type with no parameters/outputs projects to empties without panicking.
func TestToPlatformResourceTypeDTOs(t *testing.T) {
	t.Parallel()

	in := []resources.PlatformResourceType{
		{
			Name:        "postgres-cnpg",
			Description: "Managed PostgreSQL",
			Parameters:  map[string]any{"size": map[string]any{"type": "string"}},
			Outputs:     []string{"host", "port", "database"},
		},
		{Name: "redis-cache"}, // minimal: no description/params/outputs
	}

	got := toPlatformResourceTypeDTOs(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "postgres-cnpg" || got[0].Description != "Managed PostgreSQL" {
		t.Errorf("name/description not mapped: %+v", got[0])
	}
	if _, ok := got[0].Parameters["size"]; !ok {
		t.Errorf("parameters not mapped: %+v", got[0].Parameters)
	}
	if len(got[0].Outputs) != 3 || got[0].Outputs[0] != "host" {
		t.Errorf("outputs not mapped: %+v", got[0].Outputs)
	}
	if got[1].Name != "redis-cache" || got[1].Parameters != nil || got[1].Outputs != nil {
		t.Errorf("minimal type not projected cleanly: %+v", got[1])
	}
}
