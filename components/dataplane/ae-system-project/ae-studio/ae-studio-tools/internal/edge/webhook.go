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
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
)

// The unauthenticated /webhooks/github route reads a body before it can check
// the signature, so its reads are bounded in size, time and number.
const (
	// webhookBodyBytes is GitHub's maximum delivery size (ticket 04 §8).
	webhookBodyBytes int64 = 25 << 20
	// webhookReadTimeout bounds the body read. GitHub gives up on a delivery
	// after 10 s, so a body still arriving after that is never one GitHub
	// waits on; a sender trickling bytes cannot hold a connection longer.
	webhookReadTimeout = 10 * time.Second
	// webhookConcurrency caps the deliveries in flight (read, verified or
	// being forwarded). One org's repositories deliver a few events at a time;
	// 8 bodies at the 25 MiB cap are 200 MiB, a fifth of the tools
	// container's 1 GiB memory limit, which it shares with the git engine.
	webhookConcurrency = 8
	// webhookLogRunes bounds the delivery id and event name in a log line:
	// both are sender-chosen headers, logged before the signature is known.
	webhookLogRunes = 64
)

// WebhookForwarder hands a verified delivery to aep-api (webhook.Forwarder):
// it answers aep-api's last status (0 when not reached) and
// webhook.ErrUpstreamUnavailable when GitHub should retry.
type WebhookForwarder interface {
	Forward(ctx context.Context, delivery, event string, body []byte) (int, error)
}

// webhookLimits are the route's bounds; tests shrink them.
type webhookLimits struct {
	readTimeout time.Duration
	concurrency int
}

// WebhookHandler serves POST /webhooks/github (ticket 04 §8). Past
// webhookConcurrency deliveries in flight a request is 503 busy before its
// body is read; the body is capped at 25 MiB (413) and must arrive within
// webhookReadTimeout (408); the X-Hub-Signature-256 HMAC must be made with
// secret (401, not forwarded); a verified delivery goes to f. The reply rule:
// GitHub sees 200 when aep-api took the delivery or refused it for good
// (2xx, 4xx) and 503 when aep-api failed or was not reached. Events:
// webhook.forwarded {delivery, event, status} and webhook.rejected {delivery,
// event, reason[, status]} (status: aep-api's, 0 when not reached), with the
// delivery and event cut to 64 runes, never the body or a signature. An
// empty secret would let anyone sign, so it panics.
func WebhookHandler(secret string, f WebhookForwarder) http.Handler {
	return webhookHandler(secret, f, webhookLimits{readTimeout: webhookReadTimeout, concurrency: webhookConcurrency})
}

func webhookHandler(secret string, f WebhookForwarder, limits webhookLimits) http.Handler {
	if secret == "" || f == nil {
		panic("edge.WebhookHandler: secret and forwarder are required")
	}
	slots := make(chan struct{}, limits.concurrency)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivery, event := r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event")
		logDelivery, logEvent := truncateRunes(delivery, webhookLogRunes), truncateRunes(event, webhookLogRunes)
		reject := func(status int, reason, detail string) {
			slog.Warn("webhook.rejected", "delivery", logDelivery, "event", logEvent, "reason", reason)
			problem.Write(w, status, reason, detail)
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			reject(http.StatusServiceUnavailable, "busy", "too many webhook deliveries are in flight")
			return
		}
		if r.ContentLength > webhookBodyBytes {
			reject(http.StatusRequestEntityTooLarge, "payload_too_large", "the request body exceeds the size limit")
			return
		}
		body, err := readWebhookBody(w, r, limits.readTimeout)
		if err != nil {
			var maxErr *http.MaxBytesError
			var netErr net.Error
			switch {
			case errors.As(err, &maxErr):
				reject(http.StatusRequestEntityTooLarge, "payload_too_large", "the request body exceeds the size limit")
			case errors.As(err, &netErr) && netErr.Timeout():
				reject(http.StatusRequestTimeout, "request_timeout", "the request body did not arrive in time")
			default:
				reject(http.StatusBadRequest, "body_unreadable", "the request body could not be read")
			}
			return
		}
		if !webhook.Valid(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			reject(http.StatusUnauthorized, "signature_invalid", "the webhook signature is missing or invalid")
			return
		}
		upstream, err := f.Forward(r.Context(), delivery, event, body)
		if err != nil {
			slog.Warn("webhook.rejected", "delivery", logDelivery, "event", logEvent, "reason", "aep_api_unavailable", "status", upstream)
			problem.Write(w, http.StatusServiceUnavailable, "aep_api_unavailable", "aep-api could not take the delivery")
			return
		}
		slog.Info("webhook.forwarded", "delivery", logDelivery, "event", logEvent, "status", upstream)
		w.WriteHeader(http.StatusOK)
	})
}

// readWebhookBody reads the body up to the size cap under a read deadline on
// the connection. The deadline stays: the server resets it for the next
// request on the connection, and it is past by the time the server would
// drain an unread rest after a timed-out read, so nothing waits on it. A
// writer without deadline support (a test recorder) reads without one.
func readWebhookBody(w http.ResponseWriter, r *http.Request, timeout time.Duration) ([]byte, error) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(timeout))
	return io.ReadAll(http.MaxBytesReader(w, r.Body, webhookBodyBytes))
}

// truncateRunes returns s cut to at most n runes.
func truncateRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
