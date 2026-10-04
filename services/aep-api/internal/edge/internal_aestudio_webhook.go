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

package edge

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

// ingest-webhook-event: the org's AE Studio tools pod verified a GitHub hook
// delivery's signature and hands it over, one per request (03 §3). The org is
// the gate's (the ae-studio client's recorded org), never the payload's; the
// body is the generated json.RawMessage, the bytes as they arrived (the
// schema declares no defaults, so the validator restores them unchanged).

// WebhookIngestor accepts one verified delivery for an org (webhook.Ingestor).
type WebhookIngestor interface {
	Ingest(ctx context.Context, org, deliveryID, event string, body []byte) (webhook.IngestResult, error)
}

// codeRepositoryUnknown is the Error code for a delivery naming no repository
// of the caller's org.
const codeRepositoryUnknown = "repository_unknown"

// ingestBodyCapBytes is ingest-webhook-event's body cap: GitHub's payload
// maximum (internalBodyCaps).
const ingestBodyCapBytes int64 = 25 << 20

func (s *internalServer) IngestWebhookEvent(ctx context.Context, request igen.IngestWebhookEventRequestObject) (igen.IngestWebhookEventResponseObject, error) {
	if s.deps.WebhookIngestor == nil {
		return nil, errServiceUnavailable("webhook ingest not configured")
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("webhook payload required")
	}
	org := tenant.BoundOrgFromContext(ctx)
	res, err := s.deps.WebhookIngestor.Ingest(ctx, org, request.Params.XGitHubDelivery, request.Params.XGitHubEvent, *request.Body)
	switch {
	case errors.Is(err, webhook.ErrRepositoryUnknown):
		slog.InfoContext(ctx, "ae-studio webhook ingest: repository unknown in the org",
			"org", org, "deliveryId", request.Params.XGitHubDelivery, "event", request.Params.XGitHubEvent)
		return nil, apierr.New(http.StatusNotFound, codeRepositoryUnknown, "the delivery names no repository of the org", nil)
	case err != nil:
		slog.ErrorContext(ctx, "ae-studio webhook ingest failed",
			"org", org, "deliveryId", request.Params.XGitHubDelivery, "event", request.Params.XGitHubEvent, "error", err)
		return nil, errInternal("failed to ingest the webhook delivery")
	}
	out := igen.AEStudioWebhookEventResult{Result: igen.AEStudioWebhookEventResultResult(res)}
	if res == webhook.IngestDuplicate {
		return igen.IngestWebhookEvent200JSONResponse(out), nil
	}
	return igen.IngestWebhookEvent202JSONResponse(out), nil
}
