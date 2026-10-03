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

// Package usage delivers the records of finished turns to aep-api (07 §7).
// The agent hands each record in over the MCP socket (POST /turn-usage);
// Sender keeps them in a bounded in-memory outbox, coalesces what arrives
// inside a short window into one record-turn-usage call (at most
// MaxBatch records each), retries a failed call on a timer and sends what is
// left when the pod shuts down (07 §10). aep-api is idempotent on turnId, so
// a record sent twice is stored once.
package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

// TurnRecord is one finished turn, as record-turn-usage takes it.
type TurnRecord = aepapi.AEStudioTurnRecord

const (
	// DefaultWindow is how long the first record of a batch waits for
	// others: delivery within seconds, one call for a burst.
	DefaultWindow = 5 * time.Second
	// DefaultRetry is the wait after a failed call before the next attempt.
	DefaultRetry = 10 * time.Second
	// DefaultMax bounds the outbox: past it the oldest record is dropped.
	DefaultMax = 1000
	// MaxBatch is the contract's cap on the records of one call
	// (AEStudioTurnUsageRequest.records maxItems).
	MaxBatch = 100
)

// RejectedError is a call aep-api refused for good: a 404 means a record of
// the batch names a project that is not the org's, and the whole batch was
// refused (no row written); a 400, 413 or 422 means aep-api will never take
// the batch. Resending it can never succeed, so the batch is dropped and the
// records behind it are not held up.
type RejectedError struct {
	Status int
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("aep-api refused the batch with %d", e.Status)
}

// Sender is the outbox. Enqueue may be called from any goroutine; Run
// delivers in the background; Flush sends what is pending at once. Post is
// called by one goroutine at a time.
type Sender struct {
	// Post delivers one batch of at most MaxBatch records. A *RejectedError
	// drops the batch; any other error keeps it for a retry.
	Post func(ctx context.Context, records []TurnRecord) error
	// Window is the coalescing window, Retry the wait after a failed call,
	// Max the outbox bound.
	Window time.Duration
	Retry  time.Duration
	Max    int
	Log    *slog.Logger

	mu      sync.Mutex
	pending []TurnRecord
	// wake carries "a record arrived" to Run without blocking Enqueue.
	wake chan struct{}
	// sending is held while a batch is being sent, so Run and Flush never
	// post concurrently and a Flush can stop waiting at its deadline.
	sending chan struct{}
}

// New answers a Sender over post with the default window, retry and bound.
func New(post func(ctx context.Context, records []TurnRecord) error) *Sender {
	return &Sender{
		Post:    post,
		Window:  DefaultWindow,
		Retry:   DefaultRetry,
		Max:     DefaultMax,
		Log:     slog.Default(),
		wake:    make(chan struct{}, 1),
		sending: make(chan struct{}, 1),
	}
}

// Enqueue hands one record in. It never blocks on delivery; past Max the
// oldest pending record is dropped with a usage.dropped warn.
func (s *Sender) Enqueue(r TurnRecord) {
	s.mu.Lock()
	s.pending = append(s.pending, r)
	dropped := s.trimLocked()
	s.mu.Unlock()
	s.logOverflow(dropped)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run delivers until ctx ends: the first record of a batch opens the
// window, the window's end sends everything pending, and a failed send is
// retried every Retry (records handed in meanwhile join it). A send in
// flight when ctx ends is canceled and its records stay pending for Flush.
func (s *Sender) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		if !sleep(ctx, s.Window) {
			return
		}
		for {
			err := s.send(ctx)
			if err == nil || ctx.Err() != nil {
				break
			}
			if !sleep(ctx, s.Retry) {
				return
			}
		}
	}
}

// Flush sends everything pending now, once, and answers the first failure
// (those records stay pending) or ctx's error when a send in flight did not
// end before ctx did.
func (s *Sender) Flush(ctx context.Context) error {
	return s.send(ctx)
}

// send posts the pending records oldest first in batches of at most
// MaxBatch, until none is left or one fails: a rejected batch is dropped
// and the next one sent; any other failure puts the batch back in front and
// stops.
func (s *Sender) send(ctx context.Context) error {
	select {
	case s.sending <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.sending }()
	for {
		batch := s.takeBatch()
		if len(batch) == 0 {
			return nil
		}
		err := s.Post(ctx, batch)
		var rejected *RejectedError
		switch {
		case err == nil:
		case errors.As(err, &rejected):
			s.Log.Warn("usage.dropped", "count", len(batch), "reason", "rejected", "status", rejected.Status)
		default:
			s.putBack(batch)
			if ctx.Err() == nil {
				s.Log.Warn("usage.send_failed", "count", len(batch), "error", err)
			}
			return err
		}
	}
}

// takeBatch removes and answers the oldest MaxBatch pending records.
func (s *Sender) takeBatch() []TurnRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := min(len(s.pending), MaxBatch)
	batch := append([]TurnRecord(nil), s.pending[:n]...)
	s.pending = s.pending[n:]
	return batch
}

// putBack returns a failed batch to the front of the outbox, keeping the
// newest Max records when records arrived while it was in flight.
func (s *Sender) putBack(batch []TurnRecord) {
	s.mu.Lock()
	s.pending = append(batch, s.pending...)
	dropped := s.trimLocked()
	s.mu.Unlock()
	s.logOverflow(dropped)
}

// trimLocked drops the oldest records past Max and answers how many.
func (s *Sender) trimLocked() int {
	over := len(s.pending) - s.Max
	if over <= 0 {
		return 0
	}
	s.pending = append([]TurnRecord(nil), s.pending[over:]...)
	return over
}

func (s *Sender) logOverflow(dropped int) {
	if dropped > 0 {
		s.Log.Warn("usage.dropped", "count", dropped, "reason", "outbox_full")
	}
}

// sleep waits d or until ctx ends, and answers whether d passed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
