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

package projectusage

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

const model = "claude-fable-5"

type fakeTurns struct{ all, draft contracts.TokenUsage }

func (f fakeTurns) ProjectTurnUsage(context.Context, string, string) (contracts.TokenUsage, contracts.TokenUsage, error) {
	return f.all, f.draft, nil
}

type fakeExecs struct{ byKind map[string]contracts.TokenUsage }

func (f fakeExecs) SumUsageByKind(context.Context, string, string) (map[string]contracts.TokenUsage, error) {
	return f.byKind, nil
}

func pricer() *modelcost.Pricer {
	return modelcost.New(modelcost.Rates{
		ModelID: model, InputUSDPerMTok: 10, OutputUSDPerMTok: 50,
		CacheReadUSDPerMTok: 1, CacheWriteUSDPerMTok: 12.5,
	})
}

func usage(in, out int64) contracts.TokenUsage {
	return contracts.TokenUsage{InputTokens: in, OutputTokens: out, Model: model}
}

func TestGetProjectUsageAssemblesPhases(t *testing.T) {
	h := New(
		fakeTurns{all: usage(1_000_000, 0), draft: usage(200_000, 0)},
		fakeExecs{byKind: map[string]contracts.TokenUsage{
			"coding": usage(0, 100_000),
			"build":  usage(0, 20_000),
			"ops":    usage(50_000, 0),
		}},
		pricer(),
	)
	resp, err := h.GetProjectUsage(context.Background(), gen.GetProjectUsageRequestObject{ProjectName: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	body := resp.(gen.GetProjectUsage200JSONResponse)
	if body.Spec.InputTokens != 1_000_000 || body.Spec.CostUsd == nil || *body.Spec.CostUsd != 10 {
		t.Fatalf("spec = %+v", body.Spec)
	}
	if body.DraftCycle.InputTokens != 200_000 {
		t.Fatalf("draftCycle = %+v", body.DraftCycle)
	}
	// build = coding + build kinds, priced as one figure.
	if body.Build.OutputTokens != 120_000 || body.Build.CostUsd == nil || *body.Build.CostUsd != 6 {
		t.Fatalf("build = %+v", body.Build)
	}
	if body.Validation.InputTokens != 50_000 {
		t.Fatalf("validation = %+v", body.Validation)
	}
}

func TestGetProjectUsageUnwiredPortsAnswerZero(t *testing.T) {
	h := New(nil, nil, nil)
	resp, err := h.GetProjectUsage(context.Background(), gen.GetProjectUsageRequestObject{ProjectName: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	body := resp.(gen.GetProjectUsage200JSONResponse)
	if body.Spec.InputTokens != 0 || body.Build.InputTokens != 0 {
		t.Fatalf("expected zero rollups, got %+v", body)
	}
}
