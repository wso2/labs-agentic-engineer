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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
)

func orgRecordType(t *testing.T) *openchoreo.ResourceType {
	t.Helper()
	rt, err := openchoreo.BuildExternalResourceType(openchoreo.ExternalResourceTypeSpec{
		Name:                    "currency-service",
		Description:             "Live FX rates.",
		Keys:                    []openchoreo.ExternalResourceConfigKey{{Key: "OPENEXCHANGERATES_APP_ID", Secret: true}},
		Scope:                   openchoreo.ExternalResourceScopeOrg,
		Provider:                "Open Exchange Rates",
		ConsumptionInstructions: "Call /latest.json once per approval.",
	})
	if err != nil {
		t.Fatalf("build record type: %v", err)
	}
	return rt
}

// An org-scoped type's name is a function of the logical name and the key
// schema, so a type of that name that a project authored before the scope
// marker existed squats the record's name. Ensure adopts it — rewrites it as
// the record — instead of reporting success while nothing changed.
func TestExternalResourceCatalog_Ensure_AdoptsASquattingType(t *testing.T) {
	t.Parallel()
	record := orgRecordType(t)
	// A pre-marker type: the same schema under the same name, carrying only
	// the logical name — no scope, provider or instructions.
	squatter := *record
	squatter.Metadata.Annotations = map[string]string{"aep.wso2.com/external-name": "currency-service"}
	var updated *openchoreo.ResourceType
	rc := &ocmocks.ResourceClientMock{
		EnsureResourceTypeFunc: func(_ context.Context, _ string, _ *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
			return &squatter, nil // 409 → the existing type comes back untouched
		},
		UpdateResourceTypeFunc: func(_ context.Context, _ string, rt *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
			updated = rt
			return rt, nil
		},
	}
	if err := NewExternalResourceCatalog(rc).Ensure(context.Background(), "default", record); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if updated == nil || updated.Metadata.Name != record.Metadata.Name {
		t.Fatalf("the squatting type must be rewritten as the record, got %+v", updated)
	}
	def, ok := openchoreo.ExternalDefinitionFromRT(updated)
	if !ok || !def.Registered() || def.Provider != "Open Exchange Rates" {
		t.Fatalf("adopted type is not the record: %+v", def)
	}
}

// A type that already is a record is left alone: Ensure is idempotent.
func TestExternalResourceCatalog_Ensure_LeavesARecordAlone(t *testing.T) {
	t.Parallel()
	record := orgRecordType(t)
	rc := &ocmocks.ResourceClientMock{
		EnsureResourceTypeFunc: func(_ context.Context, _ string, _ *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
			return record, nil
		},
		UpdateResourceTypeFunc: func(_ context.Context, _ string, rt *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
			t.Fatalf("a record must not be rewritten on Ensure: %+v", rt)
			return nil, nil
		},
	}
	if err := NewExternalResourceCatalog(rc).Ensure(context.Background(), "default", record); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
}
