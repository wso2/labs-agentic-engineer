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

package gitrepo

import (
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

func TestBuildIssueBody_ExternalDepContractLine(t *testing.T) {
	const compName = "weather-frontend"
	const depName = "openweather"
	const specPath = "dependencies/openweather.openapi.yaml"
	const wantLine = "- **API contract — openweather:** `specs/design/components/weather-frontend/dependencies/openweather.openapi.yaml` — implement the client against these exact operations; do not invent endpoints."

	task := &models.ComponentTask{
		ComponentName: compName,
		IssueNumber:   42,
		Title:         "Implement weather-frontend",
	}
	comp := &models.DesignComponent{
		Name:          compName,
		ComponentType: "webApp",
		Language:      "TypeScript",
		Dependencies: []models.Dependency{
			{
				Kind:     models.DependencyKindExternal,
				Name:     depName,
				SpecPath: specPath,
			},
		},
	}

	body := BuildIssueBody(task, comp, "", "")

	if !strings.Contains(body, wantLine) {
		t.Errorf("expected body to contain contract line:\n  %q\n\ngot body:\n%s", wantLine, body)
	}
}

func TestBuildIssueBody_ExternalDepNoSpecPath_NoContractLine(t *testing.T) {
	const compName = "payment-service"
	const depName = "stripe"

	task := &models.ComponentTask{
		ComponentName: compName,
		IssueNumber:   7,
		Title:         "Implement payment-service",
	}
	comp := &models.DesignComponent{
		Name:          compName,
		ComponentType: "service",
		Language:      "Go",
		Dependencies: []models.Dependency{
			{
				Kind:     models.DependencyKindExternal,
				Name:     depName,
				SpecPath: "", // no spec stored
			},
		},
	}

	body := BuildIssueBody(task, comp, "", "")

	if strings.Contains(body, "**API contract —") {
		t.Errorf("expected body NOT to contain a contract line when SpecPath is empty, but got:\n%s", body)
	}
}

func TestBuildIssueBody_NonExternalDepNoContractLine(t *testing.T) {
	const compName = "order-service"

	task := &models.ComponentTask{
		ComponentName: compName,
		IssueNumber:   3,
		Title:         "Implement order-service",
	}
	comp := &models.DesignComponent{
		Name:          compName,
		ComponentType: "service",
		Language:      "Go",
		Dependencies: []models.Dependency{
			{
				Kind:    models.DependencyKindComponent,
				Name:    "inventory-service",
				// SpecPath deliberately absent — and it's not external either
				SpecPath: "dependencies/something.openapi.yaml",
			},
		},
	}

	body := BuildIssueBody(task, comp, "", "")

	if strings.Contains(body, "**API contract —") {
		t.Errorf("expected body NOT to contain a contract line for non-external dep, but got:\n%s", body)
	}
}
