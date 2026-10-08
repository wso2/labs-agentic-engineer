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

// Package webhook holds the GitHub webhook route's two halves: the HMAC check
// on a delivery and the Forwarder that hands a verified delivery to aep-api.
package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

const (
	// forwardBudget bounds one Forward, retries included: it must end inside
	// GitHub's 10 s delivery window (a delivery not answered in 10 s is marked
	// failed) so GitHub sees our answer.
	forwardBudget = 8 * time.Second
	// forwardRetries is how many times a transport error or a 5xx is retried.
	forwardRetries = 2
	// retryBackoff is the pause before a retry.
	retryBackoff = 250 * time.Millisecond
	// drainBytes bounds how much of aep-api's answer is read before closing,
	// so the connection can be reused; the answer itself decides nothing.
	drainBytes = 64 << 10
)

// ErrUpstreamUnavailable is a Forward failure GitHub should see as 503:
// aep-api answered 5xx (after the retries), refused the pod's own
// credentials, throttled it, or could not be reached.
var ErrUpstreamUnavailable = errors.New("aep-api unavailable")

// ingester is the generated aep-api client's ingest-webhook-event operation
// (POST /internal/v1/ae-studio/webhook-events).
type ingester interface {
	IngestWebhookEventWithBody(ctx context.Context, params *aepapi.IngestWebhookEventParams, contentType string, body io.Reader, reqEditors ...aepapi.RequestEditorFn) (*http.Response, error)
}

// Forwarder hands a verified delivery to aep-api's ingest-webhook-event
// (ADR-0048), synchronously: there is no buffer in the pod. GitHub
// does not redeliver by itself: a 503 marks the delivery failed, and it is
// redelivered only by hand (or by an API call), so what aep-api did not take
// waits there.
type Forwarder struct {
	client  ingester
	budget  time.Duration
	backoff time.Duration
}

// NewForwarder forwards over c, the generated aep-api client built by
// platform.NewAEPAPI with the org's ae-studio client credentials (the only
// token ingest-webhook-event accepts; its transport mints the bearer and
// retries once on a 401).
func NewForwarder(c ingester) *Forwarder {
	return &Forwarder{client: c, budget: forwardBudget, backoff: retryBackoff}
}

// Forward posts body, byte for byte, with GitHub's delivery id and event
// name, and answers aep-api's last status (0 when it was not reached).
//
// nil: aep-api took the delivery (2xx: dispatched, held or duplicate) or
// refused it for good (any other 4xx, e.g. 404 repository_unknown); GitHub
// redelivering it by hand would change nothing. ErrUpstreamUnavailable: a transport
// error or a 5xx on the first try and both retries, or a 401, 403 or 429 (the
// pod's credentials or rate, not the delivery, were refused; not retried
// here). Each try gets an equal share of what is left of the budget, so a
// hung aep-api still leaves time for the retries.
func (f *Forwarder) Forward(ctx context.Context, delivery, event string, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, f.budget)
	defer cancel()
	params := &aepapi.IngestWebhookEventParams{XGitHubDelivery: delivery, XGitHubEvent: event}
	for attempt := 0; ; attempt++ {
		status, err := f.try(ctx, forwardRetries+1-attempt, params, body)
		switch {
		case err == nil && delivered(status):
			return status, nil
		case err == nil && status < http.StatusInternalServerError:
			return status, fmt.Errorf("%w: aep-api answered %d", ErrUpstreamUnavailable, status)
		}
		if attempt == forwardRetries || !f.pause(ctx) {
			if err != nil {
				return status, fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
			}
			return status, fmt.Errorf("%w: aep-api answered %d", ErrUpstreamUnavailable, status)
		}
	}
}

// try sends one request with its share of the budget left: the time to the
// deadline divided among the tries still to run. A transport error answers 0.
func (f *Forwarder) try(ctx context.Context, triesLeft int, params *aepapi.IngestWebhookEventParams, body []byte) (int, error) {
	share := f.budget
	if deadline, ok := ctx.Deadline(); ok {
		share = time.Until(deadline) / time.Duration(triesLeft)
	}
	tryCtx, cancel := context.WithTimeout(ctx, share)
	defer cancel()
	resp, err := f.client.IngestWebhookEventWithBody(tryCtx, params, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// pause waits the backoff before a retry; false when ctx ended first.
func (f *Forwarder) pause(ctx context.Context) bool {
	if f.backoff <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(f.backoff)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// delivered reports whether aep-api's answer settles the delivery: a 2xx, or
// a 4xx that refuses the delivery itself. 401 and 403 (the pod's credentials)
// and 429 (its rate) do not.
func delivered(status int) bool {
	switch {
	case status >= 200 && status < 300:
		return true
	case status == http.StatusUnauthorized, status == http.StatusForbidden, status == http.StatusTooManyRequests:
		return false
	default:
		return status >= 400 && status < 500
	}
}
