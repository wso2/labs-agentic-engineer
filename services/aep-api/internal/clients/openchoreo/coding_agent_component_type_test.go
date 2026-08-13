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

package openchoreo

import "testing"

func TestCodingAgentComponentType_Pins(t *testing.T) {
	ct := CodingAgentComponentType()
	if CodingAgentComponentTypeName != "coding-agent" {
		t.Fatalf("type name: %q", CodingAgentComponentTypeName)
	}
	meta, _ := ct["metadata"].(map[string]any)
	anns, _ := meta["annotations"].(map[string]string)
	if anns["aep.wso2.com/internal"] != "true" {
		t.Fatalf("missing internal annotation: %#v", anns)
	}
	spec, _ := ct["spec"].(map[string]any)
	if spec["workloadType"] != "job" {
		t.Fatalf("workloadType: %#v", spec["workloadType"])
	}
	// Assert schema defaults/max for backoffLimit, activeDeadlineSeconds,
	// ttlSecondsAfterFinished, and resources ceilings exist — exact path
	// depends on how the builder nests openAPIV3Schema; fail the test if
	// any pin is missing or wrong.
	schema := mustFindSchemaProps(t, spec)
	assertIntDefault(t, schema, "backoffLimit", 0)
	assertIntDefault(t, schema, "activeDeadlineSeconds", 3600)
	assertIntDefault(t, schema, "ttlSecondsAfterFinished", 86400)
	assertResourceCeilingsPresent(t, schema)
}

func mustFindSchemaProps(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()
	// Navigate to parameters.openAPIV3Schema.properties — panic/fail if absent.
	params, _ := spec["parameters"].(map[string]any)
	oas, _ := params["openAPIV3Schema"].(map[string]any)
	props, _ := oas["properties"].(map[string]any)
	if props == nil {
		t.Fatal("missing openAPIV3Schema.properties")
	}
	return props
}

func assertIntDefault(t *testing.T, props map[string]any, key string, want int) {
	t.Helper()
	p, _ := props[key].(map[string]any)
	if p == nil {
		t.Fatalf("missing property %q", key)
	}
	def, ok := p["default"].(int)
	if !ok {
		// JSON numbers often decode as float64 in maps — accept both.
		if f, ok := p["default"].(float64); ok {
			def = int(f)
		} else {
			t.Fatalf("%s.default type %T", key, p["default"])
		}
	}
	if def != want {
		t.Fatalf("%s.default=%d want %d", key, def, want)
	}
}

func assertResourceCeilingsPresent(t *testing.T, props map[string]any) {
	t.Helper()
	for _, k := range []string{"cpuRequest", "cpuLimit", "memoryRequest", "memoryLimit"} {
		p, ok := props[k].(map[string]any)
		if !ok {
			t.Fatalf("missing resource schema key %q", k)
		}
		if _, ok := p["enum"]; !ok {
			t.Fatalf("%s must have an enum ceiling (requests and limits)", k)
		}
	}
}
