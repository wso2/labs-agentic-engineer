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

package prototypespec

import (
	"os"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

// TestVendoredSchemaMatchesKit is the anti-drift guard for the vendored manifest
// JSON Schema, the twin of securityspec's. The single source of truth is
// packages/prototype-kit/schema/prototype-manifest.schema.json, generated from
// the kit's Zod manifestSchema so the agent's write gate and this Go gate
// validate ONE definition. It is go:embed'd here because go:embed cannot cross
// the aep-api Go module boundary. The copy is pinned byte for byte.
//
// Re-sync on failure: `pnpm --filter @wso2/prototype-kit gen`, then copy the
// kit's schema file over this package's copy.
func TestVendoredSchemaMatchesKit(t *testing.T) {
	const vendored = "prototype-manifest.schema.json"
	// prototypespec → platform → internal → aep-api → services → repo root.
	const source = "../../../../../packages/prototype-kit/schema/prototype-manifest.schema.json"

	got, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatalf("read vendored schema: %v", err)
	}
	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read the kit's schema (%s) — layout drift?: %v", source, err)
	}
	if string(got) != string(want) {
		t.Fatalf("vendored %s differs from the kit's schema — re-sync: regenerate it "+
			"(pnpm --filter @wso2/prototype-kit gen) and copy it over this package's copy", vendored)
	}
}

// TestVendoredSchemaUsesOnlySupportedKeywords is the other half of the guard:
// the interpreter ignores a keyword it does not implement, so a Zod change that
// emits one would leave this gate quietly validating less than the agent's.
func TestVendoredSchemaUsesOnlySupportedKeywords(t *testing.T) {
	if unsupported := jsonschema.UnsupportedKeywords(schemaJSON); len(unsupported) > 0 {
		t.Fatalf("prototype-manifest.schema.json uses keywords the Go interpreter ignores: %v — "+
			"implement them in internal/platform/jsonschema, or the Go gate validates less than the agent's",
			unsupported)
	}
}
