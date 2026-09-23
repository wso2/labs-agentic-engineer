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

// TestVendoredSchemaMatchesContracts is the anti-drift guard for the vendored
// JSON Schema, the twin of securityspec's. The single source of truth is
// packages/contracts/schemas/prototype-model.schema.json, generated from the
// Zod prototypeModelSchema in @aep/prototype-model so the agent's write gate and
// this Go gate validate ONE definition. It is go:embed'd here because go:embed
// cannot cross the aep-api Go module boundary.
//
// Re-sync on failure: `pnpm --filter @aep/prototype-model gen`, then copy the
// contract over this package's copy.
func TestVendoredSchemaMatchesContracts(t *testing.T) {
	const vendored = "prototype-model.schema.json"
	// prototypespec → platform → internal → aep-api → services → repo root.
	const source = "../../../../../packages/contracts/schemas/prototype-model.schema.json"

	got, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatalf("read vendored schema: %v", err)
	}
	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read contracts schema (%s) — layout drift?: %v", source, err)
	}
	if string(got) != string(want) {
		t.Fatalf("vendored prototype-model.schema.json differs from packages/contracts/schemas " +
			"— re-sync: regenerate the contract (pnpm --filter @aep/prototype-model gen) and copy it here")
	}
}

// TestVendoredSchemaUsesOnlySupportedKeywords is the other half of the guard:
// the interpreter ignores a keyword it does not implement, so a Zod change that
// emits one would leave this gate quietly validating less than the agent's.
func TestVendoredSchemaUsesOnlySupportedKeywords(t *testing.T) {
	if unsupported := jsonschema.UnsupportedKeywords(schemaJSON); len(unsupported) > 0 {
		t.Fatalf("prototype-model.schema.json uses keywords the Go interpreter ignores: %v — "+
			"implement them in internal/platform/jsonschema, or the Go gate validates less than the agent's",
			unsupported)
	}
}
