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

package migrate_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// Every model an organization can pick as its coding model must have a seeded
// rate row: cost stamping is all-or-nothing per capture, so one offered model
// without a rate blanks the whole cycle's cost on every run that touches it.
// The contract says "a model is offered only once the
// platform can price it"; this is the executable half of that sentence, over
// the real migrated schema.
func TestEveryOfferedCodingModelIsPriced_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)

	rows, err := migrate.LoadModelRates(context.Background(), db)
	if err != nil {
		t.Fatalf("load model rates: %v", err)
	}
	stamper := modelcost.NewStamper(rows)
	for _, model := range orgconfig.AgentModels {
		if stamper.Cost(modelcost.Tokens{ModelID: model, InputTokens: 1_000_000}) == nil {
			t.Errorf("offered model %q has no seeded model_rates row", model)
		}
	}
}
