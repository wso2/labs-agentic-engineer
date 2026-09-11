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

package designspec

import (
	"os"
	"testing"
)

// TestVendoredSchemaMatchesContracts is the anti-drift guard for the vendored
// JSON Schema. The single source of truth is
// packages/contracts/schemas/component-design.schema.json (generated from the
// Zod componentDesignSchema so the agent's write-gate and this Go save-gate
// validate ONE definition, docs/design/agents-generation-migration.md §8). That
// file is go:embed'd here as a vendored copy because go:embed cannot cross the
// aep-api Go module boundary.
//
// If the two ever diverge, the BFF's hard save-gate would validate against a
// different schema than the agent's author-time gate — exactly the two-hand-kept
// -copies failure §8 forbids. This test fails byte-for-byte on any drift;
// re-sync by regenerating the contract (`pnpm --filter @aep/agent-stream gen`)
// and copying it over this file.
func TestVendoredSchemaMatchesContracts(t *testing.T) {
	for _, name := range []string{"component-design.schema.json", "dependency-design.schema.json"} {
		t.Run(name, func(t *testing.T) {
			// designspec → platform → internal → aep-api → services → repo root.
			source := "../../../../../packages/contracts/schemas/" + name
			got, err := os.ReadFile(name)
			if err != nil {
				t.Fatalf("read vendored schema: %v", err)
			}
			want, err := os.ReadFile(source)
			if err != nil {
				t.Fatalf("read contracts schema (%s) — layout drift?: %v", source, err)
			}
			if string(got) != string(want) {
				t.Fatalf("vendored %s differs from packages/contracts/schemas — re-sync (§8): "+
					"regenerate the contract (pnpm --filter @aep/agent-stream gen) and copy it into this package", name)
			}
		})
	}
}
