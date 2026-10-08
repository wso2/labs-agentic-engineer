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
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/async"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// isLookupNotFound reports whether err is a 404 surfaced by the routing
// lookup. A 404 means "this event is for a repo or installation that
// isn't connected to AEP" — ack noop rather than report a failure.
func isLookupNotFound(err error) bool {
	var nfe *organization.NotFoundError
	if errors.As(err, &nfe) {
		return true
	}
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "not found") || strings.Contains(s, "no rows")
}

// WebhookController is the BFF's inbound GitHub webhook receiver.
//
// Pipeline order (github-integration-phase0.md §8.2):
//
//  1. Read raw body.
//  2. Parse routing key (installation.id for App-mode events,
//     repository.full_name for per-repo events).
//  3. Resolve ocOrgID via git-service (60s in-process cache).
//  4. HMAC-validate against that org's secrets.
//  5. Dedup INSERT into webhook_deliveries, claiming the delivery's lease.
//  6. Ack 202 — BEFORE any handler runs. GitHub closes the connection at 10
//     seconds, so nothing the handlers do may depend on it.
//  7. Dispatch the handlers detached from the request (deliveryRunner), then
//     mark processed, or mark failed and hold the delivery for its backoff.
//
// GitHub never redelivers on its own; a failed delivery comes back through the
// Replayer (replayer.go), and a manual redelivery is one more duplicate of it.
type WebhookController interface {
	Receive(w http.ResponseWriter, r *http.Request)
}

type webhookController struct {
	verifier   *Verifier
	deliveries *sourcecontrol.DeliveryStore
	runner     deliveryRunner
	lookup     OcOrgIDLookup // served by CredentialService
	cache      *RoutingCache // 60s in-process cache
}

// NewWebhookController wires the receiver. lookup + cache are required;
// passing nil disables the receiver.
func NewWebhookController(verifier *Verifier, deliveries *sourcecontrol.DeliveryStore, router *Router, lookup OcOrgIDLookup, cache *RoutingCache) WebhookController {
	return &webhookController{
		verifier:   verifier,
		deliveries: deliveries,
		runner:     deliveryRunner{deliveries: deliveries, router: router},
		lookup:     lookup,
		cache:      cache,
	}
}

func (c *webhookController) Receive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Stage timings for the accept log: everything before the ack counts
	// against GitHub's 10-second delivery timeout.
	received := time.Now()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.ErrorContext(ctx, "webhook: read body", "error", err)
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	event := r.Header.Get("X-GitHub-Event")
	signature := r.Header.Get("X-Hub-Signature-256")
	if deliveryID == "" || event == "" {
		http.Error(w, "missing X-GitHub-Delivery or X-GitHub-Event", http.StatusBadRequest)
		return
	}

	read := time.Now()
	ocOrgID, err := ResolveOcOrgID(ctx, c.lookup, c.cache, event, body)
	if err != nil {
		// No-routing-key events (ping, etc.) are 200 ack'd.
		if errors.Is(err, ErrNoRoutingKey) {
			slog.DebugContext(ctx, "webhook: no routing key — ack noop",
				"event", event, "deliveryId", deliveryID, "result", "no_routing_key")
			w.WriteHeader(http.StatusOK)
			return
		}
		// 404 from the routing lookup means "the install / repo isn't
		// connected to this AEP instance." Ack 200 noop — the event is
		// genuinely not for us. Other errors (5xx, network) answer 503, and
		// since nothing was persisted yet nothing replays them: only a
		// manual redelivery (or a reconcile sweep, for what it heals) does.
		if isLookupNotFound(err) {
			slog.InfoContext(ctx, "webhook: routing miss — ack noop (event not for this instance)",
				"event", event, "deliveryId", deliveryID, "error", err, "result", "routing_miss")
			w.WriteHeader(http.StatusOK)
			return
		}
		slog.WarnContext(ctx, "webhook: routing failed (transient — will be retried)",
			"deliveryId", deliveryID, "event", event, "error", err, "result", "routing_failed")
		http.Error(w, "routing", http.StatusServiceUnavailable)
		return
	}

	// Refetch limiter key — bucket per (ocOrgID, sourceIP) so a single
	// remote can't amplify forged-event load against git-service.
	limiterKey := ocOrgID + "|" + r.RemoteAddr
	routed := time.Now()

	if err := c.verifier.VerifyWithKey(ctx, ocOrgID, limiterKey, signature, body); err != nil {
		if errors.Is(err, ErrSignatureMismatch) || errors.Is(err, ErrSignatureMalformed) {
			slog.WarnContext(ctx, "webhook: signature rejected",
				"deliveryId", deliveryID, "event", event, "ocOrgId", ocOrgID, "error", err, "result", "hmac_failed")
			http.Error(w, "signature", http.StatusUnauthorized)
			return
		}
		slog.ErrorContext(ctx, "webhook: verify error",
			"deliveryId", deliveryID, "event", event, "ocOrgId", ocOrgID, "error", err, "result", "verify_error")
		http.Error(w, "verify", http.StatusInternalServerError)
		return
	}

	verified := time.Now()
	action := actionFromPayload(body)
	res, err := c.deliveries.Persist(ctx, deliveryID, ocOrgID, event, action,
		redactPublishedCredentials(body), deliveryLease)
	if err != nil {
		slog.ErrorContext(ctx, "webhook: persist failed",
			"deliveryId", deliveryID, "event", event, "error", err, "result", "persist_failed")
		http.Error(w, "persist", http.StatusInternalServerError)
		return
	}
	if res.AlreadyProcessed {
		// Replay of finished work — ack and move on.
		slog.InfoContext(ctx, "webhook: dedup — already processed",
			"deliveryId", deliveryID, "event", event, "action", action, "ocOrgId", ocOrgID, "result", "dedup")
		w.WriteHeader(http.StatusOK)
		return
	}
	if !res.Claimed {
		// A duplicate of a delivery another attempt holds: its first run is still
		// going, or it failed and is inside its retry backoff. Either way the
		// holder settles it, and running it here too would run it twice.
		slog.InfoContext(ctx, "webhook: duplicate of a held delivery — ack without running",
			"deliveryId", deliveryID, "event", event, "action", action, "ocOrgId", ocOrgID, "result", "held")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Ack FIRST, then run the handlers detached from the request. GitHub closes
	// a delivery's connection at 10 seconds, which cancels r.Context(); a
	// handler running on it lost every in-flight GitHub and OpenChoreo call
	// with it. The handlers get context.WithoutCancel (the correlation id
	// survives, the cancellation does not) under their own handlerBudget, and
	// this attempt holds the delivery's lease until it settles. A pod that dies
	// mid-run leaves the lease to lapse, and the Replayer runs it again.
	persisted := time.Now()
	slog.InfoContext(ctx, "webhook: accepted — dispatching",
		"deliveryId", deliveryID, "event", event, "action", action, "ocOrgId", ocOrgID,
		"attempt", res.Attempts, "result", "dispatched",
		"readMs", read.Sub(received).Milliseconds(), "routeMs", routed.Sub(read).Milliseconds(),
		"verifyMs", verified.Sub(routed).Milliseconds(), "persistMs", persisted.Sub(verified).Milliseconds(),
		"ackMs", persisted.Sub(received).Milliseconds())
	w.WriteHeader(http.StatusAccepted)
	attempt := deliveryAttempt{
		deliveryID: deliveryID, event: event, action: action, ocOrgID: ocOrgID,
		attempt: res.Attempts, receivedAt: res.ReceivedAt, payload: body, source: "receiver",
	}
	async.Go(context.WithoutCancel(ctx), "webhook:"+event, func(ctx context.Context) {
		c.runner.run(ctx, attempt)
	})
}

func actionFromPayload(body []byte) string {
	var withAction struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(body, &withAction)
	return withAction.Action
}
