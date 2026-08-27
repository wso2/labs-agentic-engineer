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

package rolesspec

import (
	"os"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/jsonschema"
)

// TestVendoredSchemaMatchesContracts is the anti-drift guard for the vendored
// JSON Schema, the twin of designspec's. The single source of truth is
// packages/contracts/schemas/roles-design.schema.json, generated from the Zod
// rolesDesignSchema so the agent's write-gate and this Go gate validate ONE
// definition. It is go:embed'd here because go:embed cannot cross the aep-api
// Go module boundary.
//
// Re-sync on failure: `pnpm --filter @aep/agent-stream gen`, then copy the
// contract over this package's copy.
func TestVendoredSchemaMatchesContracts(t *testing.T) {
	const vendored = "roles-design.schema.json"
	// rolesspec → platform → internal → aep-api → services → repo root.
	const source = "../../../../../packages/contracts/schemas/roles-design.schema.json"

	got, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatalf("read vendored schema: %v", err)
	}
	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read contracts schema (%s) — layout drift?: %v", source, err)
	}
	if string(got) != string(want) {
		t.Fatalf("vendored roles-design.schema.json differs from packages/contracts/schemas " +
			"— re-sync: regenerate the contract (pnpm --filter @aep/agent-stream gen) and copy it here")
	}
}

// TestVendoredSchemaUsesOnlySupportedKeywords is the OTHER half of the guard,
// and the more subtle one. The interpreter ignores a keyword it does not
// implement, so a Zod change that emits (say) `pattern` would leave the Go gate
// quietly validating LESS than the agent's — a document the agent rejects would
// then acquire a tag. This fails loudly instead.
func TestVendoredSchemaUsesOnlySupportedKeywords(t *testing.T) {
	if unsupported := jsonschema.UnsupportedKeywords(schemaJSON); len(unsupported) > 0 {
		t.Fatalf("roles-design.schema.json uses keywords the Go interpreter ignores: %v — "+
			"implement them in internal/platform/jsonschema, or the Go gate validates less than the agent's",
			unsupported)
	}
}
