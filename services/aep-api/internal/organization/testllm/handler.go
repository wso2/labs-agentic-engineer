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

package testllm

import (
	"context"
	"errors"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// Handler serves test-llm-connection. The deny-by-default tenant gate binds the
// active org before this runs, so org is read from context, never from the
// request. All logic lives in organization.Service; this slice maps HTTP <->
// domain, and a refusal maps exactly as the save's llm section does.
type Handler struct{ config *organization.Service }

// New returns the slice's handler.
func New(config *organization.Service) *Handler { return &Handler{config: config} }

func (h *Handler) TestLlmConnection(ctx context.Context, request gen.TestLlmConnectionRequestObject) (gen.TestLlmConnectionResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	check, err := h.config.TestLLM(ctx, org, *request.Body)
	if errors.Is(err, organization.ErrLLMTestRateLimited) {
		return nil, apierr.New(http.StatusTooManyRequests, "llm_test_rate_limited",
			"too many connection tests from this organization; try again in a minute",
			[]gen.ErrorDetail{{Field: "body.llm", Message: "rate limited"}})
	}
	if err != nil {
		return nil, organization.MapConfigError(err)
	}
	return gen.TestLlmConnection200JSONResponse(*check), nil
}
