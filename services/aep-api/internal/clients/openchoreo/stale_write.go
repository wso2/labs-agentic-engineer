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

package openchoreo

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// OpenChoreo exposes no PATCH for its CRs — every field-level edit is a
// read-modify-write over the whole object (GET, mutate one field, PUT). Those
// edits race OC's own controllers, which write the same objects continuously,
// and the loser gets k8s' optimistic-concurrency error. openchoreo-api reports
// it as a GENERIC HTTP 500: the cause appears only in openchoreo-api's log,
// never in the response body.
//
//	openchoreo-api  INFO   UpdateReleaseBinding called
//	openchoreo-api  ERROR  Failed to update release binding CR
//	  error="Operation cannot be fulfilled on releasebindings.openchoreo.dev
//	         \"…-workout-api-development\": the object has been modified;
//	         please apply your changes to the latest version and try again"
//	openchoreo-api  ACCESS-LOG  PUT /api/v1/…/releasebindings/…  status=500
//
// The stale resourceVersion is openchoreo-api's own, not ours — the wire types
// here carry no resourceVersion at all (see gen.ObjectMeta), so openchoreo-api
// re-reads the CR per request and applies our body onto it. Two consequences:
//
//   - The 500 is genuinely transient. The same request re-issued a moment later
//     picks up a fresh server-side read and succeeds. But nothing retried it:
//     the HTTP transport deliberately excludes 500 from its retryable set for
//     non-idempotent methods (buildRetryConfig → TransientHTTPErrorCodes),
//     because blindly replaying an arbitrary PUT/POST is not safe in general.
//     Retrying has to be an opt-in by a caller that knows its write IS
//     idempotent — which is what this helper is.
//   - We re-read anyway, inside the retried closure. Not for the
//     resourceVersion, but because the MERGE is ours: these writes preserve map
//     keys they don't own (other traits' env configs), and a merge computed
//     from a stale read would silently clobber a key a concurrent writer just
//     added. `write` must therefore do its own read on every invocation.
//
// The BFF cannot tell a conflict-as-500 from a genuine fault, so retryStaleWrite
// treats BOTH 409 and 500 as retryable. That is safe for the callers here
// because each is an idempotent converge-to-desired-state reconcile: re-running
// it re-reads and re-applies the same desired subset, so a retry after a
// partially-visible failure can only reach the same end state. Do NOT reach for
// this helper from a create/append path, where a retried write is not the same
// as one write.
const staleWriteAttempts = 4

// staleWriteBackoff is the pause before attempt N+1 (index 0 ⇒ before the first
// retry). Short by design: the conflict window is one controller reconcile, and
// OC settles in milliseconds. Overridden to zero in tests.
var staleWriteBackoff = []time.Duration{
	50 * time.Millisecond,
	200 * time.Millisecond,
	600 * time.Millisecond,
}

// retryStaleWrite runs an idempotent read-modify-write reconcile, retrying when
// the write loses an optimistic-concurrency race against an OpenChoreo
// controller.
//
// `write` MUST perform its own read on every invocation — retrying a closure
// that captured the object from an earlier read re-sends the same stale
// resourceVersion and can never succeed. `target` names what is being written,
// for the exhaustion log.
func retryStaleWrite(ctx context.Context, target string, write func(context.Context) error) error {
	var err error
	for attempt := 0; attempt < staleWriteAttempts; attempt++ {
		if attempt > 0 {
			slog.DebugContext(ctx, "openchoreo: retrying stale read-modify-write",
				"target", target, "attempt", attempt+1, "error", err)
			if waitErr := sleepCtx(ctx, staleWriteBackoff[min(attempt-1, len(staleWriteBackoff)-1)]); waitErr != nil {
				return waitErr
			}
		}
		err = write(ctx)
		if err == nil {
			return nil
		}
		if !isStaleWriteError(err) {
			return err
		}
	}
	slog.WarnContext(ctx, "openchoreo: read-modify-write still conflicting after retries",
		"target", target, "attempts", staleWriteAttempts, "error", err)
	return err
}

// isStaleWriteError reports whether err is worth a re-read-and-retry. 500 is in
// the set because openchoreo-api flattens the k8s conflict onto it (see the
// package comment above); if OC ever starts returning a true 409 for that case,
// dropping ErrInternalServerError here narrows the retry without breaking it.
func isStaleWriteError(err error) bool {
	return errors.Is(err, ErrConflict) || errors.Is(err, ErrInternalServerError)
}

// sleepCtx waits for d, or returns early with the context's error if the
// caller is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
