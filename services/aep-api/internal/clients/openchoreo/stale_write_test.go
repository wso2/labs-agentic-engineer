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
	"fmt"
	"testing"
	"time"
)

// UNIT tier for the read-modify-write retry that guards every GET-then-PUT
// against an OpenChoreo CR. The scenario it exists for: OC's Component
// controller rewrites `ReleaseBinding.spec.releaseName` in the window between
// our read and our write, k8s rejects the stale resourceVersion, and
// openchoreo-api reports that conflict as a bare 500.

// noStaleWriteBackoff removes the sleeps for the duration of a test so the
// retry loop runs at memory speed.
func noStaleWriteBackoff(t *testing.T) {
	t.Helper()
	orig := staleWriteBackoff
	staleWriteBackoff = []time.Duration{0}
	t.Cleanup(func() { staleWriteBackoff = orig })
}

// TestRetryStaleWrite_RetriesConflictSurfacedAs500 — the regression that let a
// protected API deploy with no jwtAuth: openchoreo-api maps the k8s conflict
// onto ErrInternalServerError, so the retry MUST treat 500 as retryable or the
// write is lost on the first race.
func TestRetryStaleWrite_RetriesConflictSurfacedAs500(t *testing.T) {
	noStaleWriteBackoff(t)
	attempts := 0
	err := retryStaleWrite(context.Background(), "releasebinding/x", func(context.Context) error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("%w: Internal server error", ErrInternalServerError)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryStaleWrite should have converged, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("want 3 attempts (2 conflicts then success), got %d", attempts)
	}
}

// TestRetryStaleWrite_RetriesTrueConflict — if OC ever starts returning a real
// 409 for the same case, the retry still covers it.
func TestRetryStaleWrite_RetriesTrueConflict(t *testing.T) {
	noStaleWriteBackoff(t)
	attempts := 0
	err := retryStaleWrite(context.Background(), "component/x", func(context.Context) error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("%w: the object has been modified", ErrConflict)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryStaleWrite: %v", err)
	}
	if attempts != 2 {
		t.Errorf("want 2 attempts, got %d", attempts)
	}
}

// TestRetryStaleWrite_SucceedsFirstTry — the happy path costs exactly one call;
// the helper must not add a speculative retry.
func TestRetryStaleWrite_SucceedsFirstTry(t *testing.T) {
	noStaleWriteBackoff(t)
	attempts := 0
	if err := retryStaleWrite(context.Background(), "x", func(context.Context) error {
		attempts++
		return nil
	}); err != nil {
		t.Fatalf("retryStaleWrite: %v", err)
	}
	if attempts != 1 {
		t.Errorf("want 1 attempt, got %d", attempts)
	}
}

// TestRetryStaleWrite_NonRetryableFailsFast — a 400/403/404 is a real rejection,
// not a race. Retrying it would only multiply the latency of a doomed write.
func TestRetryStaleWrite_NonRetryableFailsFast(t *testing.T) {
	noStaleWriteBackoff(t)
	for _, sentinel := range []error{ErrBadRequest, ErrForbidden, ErrNotFound, ErrUnauthorized} {
		attempts := 0
		err := retryStaleWrite(context.Background(), "x", func(context.Context) error {
			attempts++
			return fmt.Errorf("%w: nope", sentinel)
		})
		if !errors.Is(err, sentinel) {
			t.Errorf("want %v preserved, got %v", sentinel, err)
		}
		if attempts != 1 {
			t.Errorf("%v should not be retried; got %d attempts", sentinel, attempts)
		}
	}
}

// TestRetryStaleWrite_ExhaustsAndReturnsLastError — a permanently conflicting
// object gives up after the bounded attempt count and surfaces the cause, so
// the caller (and Temporal, above it) sees a failure rather than a false success.
func TestRetryStaleWrite_ExhaustsAndReturnsLastError(t *testing.T) {
	noStaleWriteBackoff(t)
	attempts := 0
	err := retryStaleWrite(context.Background(), "x", func(context.Context) error {
		attempts++
		return fmt.Errorf("%w: Internal server error", ErrInternalServerError)
	})
	if !errors.Is(err, ErrInternalServerError) {
		t.Errorf("want ErrInternalServerError after exhaustion, got %v", err)
	}
	if attempts != staleWriteAttempts {
		t.Errorf("want %d attempts, got %d", staleWriteAttempts, attempts)
	}
}

// TestRetryStaleWrite_HonorsCancellation — a cancelled context stops the loop at
// the backoff instead of burning the remaining attempts.
func TestRetryStaleWrite_HonorsCancellation(t *testing.T) {
	orig := staleWriteBackoff
	staleWriteBackoff = []time.Duration{time.Hour}
	t.Cleanup(func() { staleWriteBackoff = orig })

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryStaleWrite(ctx, "x", func(context.Context) error {
		attempts++
		cancel()
		return fmt.Errorf("%w: Internal server error", ErrInternalServerError)
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("want the loop to stop at the first backoff; got %d attempts", attempts)
	}
}
