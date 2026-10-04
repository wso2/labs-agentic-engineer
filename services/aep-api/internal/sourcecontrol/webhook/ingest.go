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

package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/platform/async"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Ingest: one verified GitHub delivery, handed over by the org's AE Studio
// tools pod (which checked GitHub's signature over the exact bytes), lands
// here with the org the caller's token binds. The tail is the receiver's:
// persist with dedup on the delivery id and claim the delivery's lease, answer
// at once, and run the handlers detached under the delivery org; a failed run
// comes back through the Replayer.

// ErrRepositoryUnknown is a delivery that names no repository of the caller's
// org: a repository-scoped event whose repository.full_name is absent or
// another org's, or an event that is not repository-scoped at all. Nothing of
// it is persisted.
var ErrRepositoryUnknown = errors.New("webhook: the delivery names no repository of the org")

// IngestResult is what became of an ingested delivery.
type IngestResult string

const (
	// IngestDispatched: persisted, claimed, and its handlers started.
	IngestDispatched IngestResult = "dispatched"
	// IngestHeld: a duplicate of a delivery another attempt holds (running, or
	// inside its retry backoff); that attempt settles it.
	IngestHeld IngestResult = "held"
	// IngestDuplicate: a redelivery of a delivery already processed.
	IngestDuplicate IngestResult = "duplicate"
)

// ingestEvents are the events an ingest accepts: the hook's events (the AE
// Studio tools pod registers pull_request, push, issue_comment and issues) and
// the hook's ping. Each carries repository.full_name, and it must be one of
// the org's repositories.
var ingestEvents = map[string]bool{
	"pull_request": true, "push": true, "issue_comment": true, "issues": true, "ping": true,
}

// RepoInOrg finds one of an org's repositories by its GitHub full name
// (sourcecontrol.RepoRepository); nil, nil when the org has none by that name.
type RepoInOrg interface {
	FindInOrgByFullName(ctx context.Context, org, fullName string) (*sourcecontrol.GitRepository, error)
}

// Ingestor persists, claims and dispatches deliveries.
type Ingestor struct {
	deliveries *sourcecontrol.DeliveryStore
	runner     deliveryRunner
	repos      RepoInOrg
}

// NewIngestor returns an Ingestor over the delivery ledger, the handler router
// and the org-scoped repository lookup.
func NewIngestor(deliveries *sourcecontrol.DeliveryStore, router *Router, repos RepoInOrg) *Ingestor {
	return &Ingestor{
		deliveries: deliveries,
		runner:     deliveryRunner{deliveries: deliveries, router: router},
		repos:      repos,
	}
}

// Ingest accepts one verified delivery for org. The delivery must name one of
// org's repositories (ErrRepositoryUnknown otherwise, nothing persisted). body
// is kept as it arrived: the handlers get these bytes, and the ledger stores
// them with only a published credential redacted (redactPublishedCredentials).
func (i *Ingestor) Ingest(ctx context.Context, org, deliveryID, event string, body []byte) (IngestResult, error) {
	if err := i.checkRepository(ctx, org, event, body); err != nil {
		return "", err
	}
	return i.accept(ctx, org, deliveryID, event, body, "ingest")
}

// checkRepository refuses a delivery that names no repository of org.
func (i *Ingestor) checkRepository(ctx context.Context, org, event string, body []byte) error {
	if !ingestEvents[event] {
		return ErrRepositoryUnknown
	}
	var p struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &p); err != nil || p.Repository.FullName == "" {
		return ErrRepositoryUnknown
	}
	repo, err := i.repos.FindInOrgByFullName(ctx, org, p.Repository.FullName)
	if err != nil {
		return fmt.Errorf("find repository in org: %w", err)
	}
	if repo == nil {
		return ErrRepositoryUnknown
	}
	return nil
}

// accept is the receiver's tail: persist (dedup on deliveryID, claiming the
// delivery's lease), then run the handlers detached from the request under
// their own handlerBudget. The caller answers as soon as this returns. A pod
// that dies mid-run leaves the lease to lapse, and the Replayer runs the
// delivery again. source names the caller in the run's logs.
func (i *Ingestor) accept(ctx context.Context, org, deliveryID, event string, body []byte, source string) (IngestResult, error) {
	action := actionFromPayload(body)
	attrs := []any{"deliveryId", deliveryID, "event", event, "action", action, "ocOrgId", org, "source", source}
	res, err := i.deliveries.Persist(ctx, deliveryID, org, event, action,
		redactPublishedCredentials(body), deliveryLease)
	if err != nil {
		slog.ErrorContext(ctx, "webhook: persist failed", append(attrs, "error", err, "result", "persist_failed")...)
		return "", fmt.Errorf("persist delivery: %w", err)
	}
	if res.AlreadyProcessed {
		slog.InfoContext(ctx, "webhook: dedup — already processed", append(attrs, "result", "dedup")...)
		return IngestDuplicate, nil
	}
	if !res.Claimed {
		// Its first run is still going, or it failed and is inside its retry
		// backoff. Either way the holder settles it; running it here too would
		// run it twice.
		slog.InfoContext(ctx, "webhook: duplicate of a held delivery — not run", append(attrs, "result", "held")...)
		return IngestHeld, nil
	}
	slog.InfoContext(ctx, "webhook: accepted — dispatching", append(attrs, "attempt", res.Attempts, "result", "dispatched")...)
	attempt := deliveryAttempt{
		deliveryID: deliveryID, event: event, action: action, ocOrgID: org,
		attempt: res.Attempts, payload: body, source: source,
	}
	// context.WithoutCancel: the correlation id survives, the request's
	// cancellation does not (the caller answers before the handlers finish).
	async.Go(context.WithoutCancel(ctx), "webhook:"+event, func(ctx context.Context) {
		i.runner.run(ctx, attempt)
	})
	return IngestDispatched, nil
}

func actionFromPayload(body []byte) string {
	var withAction struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(body, &withAction)
	return withAction.Action
}
