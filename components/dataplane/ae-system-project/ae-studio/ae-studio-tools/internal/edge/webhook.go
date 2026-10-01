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
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
)

// webhookBodyBytes is GitHub's maximum delivery size (ticket 04 §8).
const webhookBodyBytes int64 = 25 << 20

// WebhookHandler serves POST /webhooks/github (ticket 04 §8): the body is
// capped at 25 MiB (413), the X-Hub-Signature-256 HMAC must be made with
// secret (401, not forwarded), and a verified delivery is forwarded to f.
// GitHub sees 200 when f accepts it and 503 on any forward failure. Log events
// carry the delivery id, event name and outcome only, never the body or a
// signature. An empty secret would let anyone sign, so it panics.
func WebhookHandler(secret string, f webhook.Forwarder) http.Handler {
	if secret == "" || f == nil {
		panic("edge.WebhookHandler: secret and forwarder are required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivery, event := r.Header.Get("X-GitHub-Delivery"), r.Header.Get("X-GitHub-Event")
		reject := func(status int, reason, detail string) {
			slog.Warn("webhook.rejected", "delivery", delivery, "event", event, "reason", reason)
			problem.Write(w, status, reason, detail)
		}
		if r.ContentLength > webhookBodyBytes {
			reject(http.StatusRequestEntityTooLarge, "payload_too_large", "the request body exceeds the size limit")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookBodyBytes))
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				reject(http.StatusRequestEntityTooLarge, "payload_too_large", "the request body exceeds the size limit")
				return
			}
			reject(http.StatusBadRequest, "body_unreadable", "the request body could not be read")
			return
		}
		if !webhook.Valid(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			reject(http.StatusUnauthorized, "signature_invalid", "the webhook signature is missing or invalid")
			return
		}
		if err := f.Forward(r.Context(), delivery, event, body); err != nil {
			slog.Warn("webhook.forward_failed", "delivery", delivery, "event", event, "reason", "aep_api_unavailable")
			problem.Write(w, http.StatusServiceUnavailable, "aep_api_unavailable", "aep-api could not take the delivery")
			return
		}
		slog.Info("webhook.forwarded", "delivery", delivery, "event", event, "status", http.StatusOK)
		w.WriteHeader(http.StatusOK)
	})
}
