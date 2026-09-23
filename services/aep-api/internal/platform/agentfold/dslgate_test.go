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

package agentfold

// Accept/reject parity with the TS gate
// (packages/agent-stream/test/wireframe-layout-gate.test.ts): a verdict
// mismatch fails healthy turns on the manifest check, so every case here
// mirrors a TS-side case.

import "testing"

const dslPath = "specs/design/components/shop-webapp/wireframes.dsl"

const dslClean = `screen Dashboard "Admin overview"
  navbar "Hub"
  sidebar "Home | Reports"
  row
    heading "Good morning"
    right
    button "New audit" primary
  row
    card "Open items | 47 | across 5 projects"
    card "Overdue | 12 | needs escalation"
  table "A | B"
    row "1 | 2"
  split 60/40
    left
      text "a"
    right
      card "Discussion"
        text "hello"
        badge "Open" info
`

const dslLegacy = `screen Dashboard "Admin overview"
  navbar "Hub"
  heading "Overview" 280,84
`

const dslTypo = `screen Dashboard
  navbar "Hub"
  crd "Open items | 47 | across 5 projects"
`

func TestDslGateAcceptsCleanFlowDialect(t *testing.T) {
	if code, msg := checkWireframeDslGuard(dslPath, dslClean); code != "" {
		t.Fatalf("clean flow dialect rejected: %s %s", code, msg)
	}
}

func TestDslGateRejectsLegacyCoordinates(t *testing.T) {
	code, _ := checkWireframeDslGuard(dslPath, dslLegacy)
	if code != ErrInvalidDSL {
		t.Fatalf("legacy dialect not rejected, got %q", code)
	}
}

func TestDslGateRejectsUnknownKind(t *testing.T) {
	code, _ := checkWireframeDslGuard(dslPath, dslTypo)
	if code != ErrInvalidDSL {
		t.Fatalf("typo kind not rejected, got %q", code)
	}
}

func TestDslGateRejectsMisplacedStructure(t *testing.T) {
	for name, body := range map[string]string{
		"stray right":       "screen S\n  right\n",
		"table row outside": "screen S\n  row \"a | b\"\n",
		"row under table":   "screen S\n  table \"A | B\"\n    row\n",
	} {
		if code, _ := checkWireframeDslGuard(dslPath, body); code != ErrInvalidDSL {
			t.Fatalf("%s not rejected", name)
		}
	}
}

func TestDslGateAcceptsRowInsideCard(t *testing.T) {
	body := "screen S\n  card \"This week\"\n    row\n      text \"Workouts: 4\"\n      text \"Volume: 12,400 kg\"\n"
	if code, msg := checkWireframeDslGuard(dslPath, body); code != "" {
		t.Fatalf("row inside card rejected: %s %s", code, msg)
	}
}

func TestDslGateSkipsOtherPaths(t *testing.T) {
	if code, _ := checkWireframeDslGuard("specs/requirements/prd.md", dslLegacy); code != "" {
		t.Fatalf("markdown gated")
	}
	if code, _ := checkWireframeDslGuard("specs/design/components/api/erd.dsl", dslLegacy); code != "" {
		t.Fatalf("domain-model dsl gated")
	}
}
