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

package usage

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// fakeAEPAPI is the Post the sender delivers to: it records every batch and
// answers the queued errors in order (nil once they run out).
type fakeAEPAPI struct {
	mu      sync.Mutex
	batches [][]TurnRecord
	answers []error
}

func (f *fakeAEPAPI) post(_ context.Context, records []TurnRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, append([]TurnRecord(nil), records...))
	if len(f.answers) == 0 {
		return nil
	}
	err := f.answers[0]
	f.answers = f.answers[1:]
	return err
}

func (f *fakeAEPAPI) posts() [][]TurnRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]TurnRecord(nil), f.batches...)
}

// syncBuffer is a log sink safe for the sender's goroutine.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newTestSender(f *fakeAEPAPI) (*Sender, *syncBuffer) {
	logs := &syncBuffer{}
	s := New(f.post)
	s.Log = slog.New(slog.NewJSONHandler(logs, nil))
	return s, logs
}

func record(n int) TurnRecord {
	return TurnRecord{TurnID: openapi_types.UUID{0: 0x5f, 14: byte(n >> 8), 15: byte(n)}, Project: "greeter"}
}

func turnIDs(batch []TurnRecord) []openapi_types.UUID {
	ids := make([]openapi_types.UUID, len(batch))
	for i, r := range batch {
		ids[i] = r.TurnID
	}
	return ids
}

// runSender starts Run in the bubble and answers its stop function, which
// cancels it and waits for it to return.
func runSender(s *Sender) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

func TestNew_Defaults(t *testing.T) {
	s := New(func(context.Context, []TurnRecord) error { return nil })
	if s.Window != 5*time.Second || s.Retry != 10*time.Second || s.Max != 1000 {
		t.Fatalf("defaults = window %v retry %v max %d", s.Window, s.Retry, s.Max)
	}
}

// Two records inside the coalescing window go out as one POST, once the
// window has passed and not before.
func TestSender_CoalescesInsideTheWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeAEPAPI{}
		s, _ := newTestSender(f)
		stop := runSender(s)
		defer stop()

		s.Enqueue(record(1))
		time.Sleep(2 * time.Second)
		s.Enqueue(record(2))
		synctest.Wait()
		if n := len(f.posts()); n != 0 {
			t.Fatalf("posted %d batches before the window ended", n)
		}
		time.Sleep(3 * time.Second)
		synctest.Wait()
		posts := f.posts()
		if len(posts) != 1 || len(posts[0]) != 2 {
			t.Fatalf("posts = %v, want one batch of 2", posts)
		}
		if got := turnIDs(posts[0]); got[0] != record(1).TurnID || got[1] != record(2).TurnID {
			t.Fatalf("batch order = %v", got)
		}

		// A later record opens a new window.
		s.Enqueue(record(3))
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if posts := f.posts(); len(posts) != 2 || len(posts[1]) != 1 {
			t.Fatalf("posts = %v, want a second batch of 1", posts)
		}
	})
}

// A failed POST keeps its records and retries them after Retry, with the
// records handed in meanwhile.
func TestSender_RetriesAfterRetryKeepingRecords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeAEPAPI{answers: []error{errors.New("aep-api answered 503")}}
		s, logs := newTestSender(f)
		stop := runSender(s)
		defer stop()

		s.Enqueue(record(1))
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if n := len(f.posts()); n != 1 {
			t.Fatalf("posts = %d, want the first attempt", n)
		}
		s.Enqueue(record(2))
		time.Sleep(9 * time.Second)
		synctest.Wait()
		if n := len(f.posts()); n != 1 {
			t.Fatalf("retried before Retry: %d posts", n)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		posts := f.posts()
		if len(posts) != 2 {
			t.Fatalf("posts = %d, want the retry", len(posts))
		}
		if got := turnIDs(posts[1]); len(got) != 2 || got[0] != record(1).TurnID || got[1] != record(2).TurnID {
			t.Fatalf("retry batch = %v, want the kept record first, then the new one", got)
		}
		if !strings.Contains(logs.String(), `"msg":"usage.send_failed"`) {
			t.Fatalf("no usage.send_failed log: %s", logs)
		}
		if strings.Contains(logs.String(), "greeter") {
			t.Fatalf("a log carries a record value: %s", logs)
		}
	})
}

// aep-api refusing a batch with 404 (a record names a project that is not
// the org's) drops the whole batch for good: never retried.
func TestSender_DropsARejectedBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeAEPAPI{answers: []error{&RejectedError{Status: 404}}}
		s, logs := newTestSender(f)
		stop := runSender(s)
		defer stop()

		s.Enqueue(record(1))
		s.Enqueue(record(2))
		time.Sleep(5 * time.Second)
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		if n := len(f.posts()); n != 1 {
			t.Fatalf("posts = %d, a rejected batch was resent", n)
		}
		if err := s.Flush(context.Background()); err != nil {
			t.Fatalf("Flush = %v", err)
		}
		if n := len(f.posts()); n != 1 {
			t.Fatalf("Flush resent the rejected batch: %d posts", n)
		}
		out := logs.String()
		if !strings.Contains(out, `"msg":"usage.dropped"`) || !strings.Contains(out, `"count":2`) || !strings.Contains(out, `"status":404`) {
			t.Fatalf("usage.dropped {count, status} not logged: %s", out)
		}
		if strings.Contains(out, "greeter") || strings.Contains(out, record(1).TurnID.String()) {
			t.Fatalf("a log carries a record value: %s", out)
		}
	})
}

// Flush sends what is pending at once, without waiting for the window.
func TestSender_FlushSendsPendingImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &fakeAEPAPI{}
		s, _ := newTestSender(f)
		stop := runSender(s)
		defer stop()

		s.Enqueue(record(1))
		s.Enqueue(record(2))
		if err := s.Flush(context.Background()); err != nil {
			t.Fatalf("Flush = %v", err)
		}
		posts := f.posts()
		if len(posts) != 1 || len(posts[0]) != 2 {
			t.Fatalf("posts = %v, want one batch of 2", posts)
		}
		// Nothing is left for the window to send.
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if n := len(f.posts()); n != 1 {
			t.Fatalf("posts = %d after Flush", n)
		}
	})
}

// Flush without Run (the sender is stopped before the shutdown flush) and
// with nothing pending posts nothing.
func TestSender_FlushWithoutRun(t *testing.T) {
	f := &fakeAEPAPI{}
	s, _ := newTestSender(f)
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("empty Flush = %v", err)
	}
	if n := len(f.posts()); n != 0 {
		t.Fatalf("empty Flush posted %d batches", n)
	}
	s.Enqueue(record(1))
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	if n := len(f.posts()); n != 1 {
		t.Fatalf("posts = %d", n)
	}
}

// A Flush whose POST fails keeps the records and answers the failure.
func TestSender_FlushFailureKeepsRecords(t *testing.T) {
	f := &fakeAEPAPI{answers: []error{errors.New("aep-api answered 502")}}
	s, _ := newTestSender(f)
	s.Enqueue(record(1))
	if err := s.Flush(context.Background()); err == nil {
		t.Fatal("Flush hid the failure")
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("second Flush = %v", err)
	}
	posts := f.posts()
	if len(posts) != 2 || len(posts[1]) != 1 || posts[1][0].TurnID != record(1).TurnID {
		t.Fatalf("posts = %v, want the kept record resent", posts)
	}
}

// A Flush that cannot get its turn before its deadline (a POST in flight
// holds it) answers the deadline instead of overrunning the shutdown budget.
func TestSender_FlushHonorsItsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var mu sync.Mutex
		posts := 0
		s := New(func(ctx context.Context, _ []TurnRecord) error {
			mu.Lock()
			posts++
			first := posts == 1
			mu.Unlock()
			if first {
				<-release
			}
			return nil
		})
		s.Log = slog.New(slog.NewJSONHandler(&syncBuffer{}, nil))
		s.Enqueue(record(1))
		go func() { _ = s.Flush(context.Background()) }()
		synctest.Wait()
		s.Enqueue(record(2))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Flush = %v, want the deadline", err)
		}
		close(release)
		synctest.Wait()
	})
}

// A POST carries at most 100 records (the contract's cap): 250 pending go
// out as 100 + 100 + 50, oldest first.
func TestSender_ChunksAtTheContractCap(t *testing.T) {
	f := &fakeAEPAPI{}
	s, _ := newTestSender(f)
	for i := range 250 {
		s.Enqueue(record(i))
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	posts := f.posts()
	if len(posts) != 3 || len(posts[0]) != 100 || len(posts[1]) != 100 || len(posts[2]) != 50 {
		t.Fatalf("batch sizes = %d batches", len(posts))
	}
	if posts[0][0].TurnID != record(0).TurnID || posts[2][49].TurnID != record(249).TurnID {
		t.Fatal("chunks are not oldest first")
	}
}

// A failed chunk stops the send: it and the chunks after it stay pending,
// the chunks before it are delivered.
func TestSender_FailedChunkKeepsItAndTheRest(t *testing.T) {
	f := &fakeAEPAPI{answers: []error{nil, errors.New("aep-api answered 503")}}
	s, _ := newTestSender(f)
	for i := range 250 {
		s.Enqueue(record(i))
	}
	if err := s.Flush(context.Background()); err == nil {
		t.Fatal("Flush hid the failure")
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	posts := f.posts()
	// 100 ok, 100 failed; then 100 + 50.
	if len(posts) != 4 || posts[2][0].TurnID != record(100).TurnID || len(posts[3]) != 50 {
		t.Fatalf("posts = %d batches, want the failed chunk resent first", len(posts))
	}
}

// More than Max pending drops the oldest, with a usage.dropped warn: the
// outbox stays bounded while aep-api is down.
func TestSender_BoundedDropsOldest(t *testing.T) {
	f := &fakeAEPAPI{}
	s, logs := newTestSender(f)
	s.Max = 3
	for i := range 5 {
		s.Enqueue(record(i))
	}
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	posts := f.posts()
	if len(posts) != 1 {
		t.Fatalf("posts = %d", len(posts))
	}
	got := turnIDs(posts[0])
	if len(got) != 3 || got[0] != record(2).TurnID || got[2] != record(4).TurnID {
		t.Fatalf("kept = %v, want the newest 3", got)
	}
	out := logs.String()
	if strings.Count(out, `"msg":"usage.dropped"`) != 2 || !strings.Contains(out, `"level":"WARN"`) {
		t.Fatalf("want two usage.dropped warns: %s", out)
	}
}

// A failed send that would push the outbox past Max keeps the newest.
func TestSender_RequeueStaysBounded(t *testing.T) {
	f := &fakeAEPAPI{answers: []error{errors.New("aep-api answered 503")}}
	s, _ := newTestSender(f)
	s.Max = 3
	for i := range 3 {
		s.Enqueue(record(i))
	}
	release := make(chan struct{})
	entered := make(chan struct{})
	s.Post = func(ctx context.Context, r []TurnRecord) error {
		close(entered)
		<-release
		return f.post(ctx, r)
	}
	errc := make(chan error, 1)
	go func() { errc <- s.Flush(context.Background()) }()
	<-entered
	// Two more arrive while the three are in flight; the POST then fails.
	s.Enqueue(record(3))
	s.Enqueue(record(4))
	close(release)
	if err := <-errc; err == nil {
		t.Fatal("Flush hid the failure")
	}
	s.Post = f.post
	if err := s.Flush(context.Background()); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	posts := f.posts()
	got := turnIDs(posts[len(posts)-1])
	if len(got) != 3 || got[0] != record(2).TurnID || got[2] != record(4).TurnID {
		t.Fatalf("kept = %v, want the newest 3", got)
	}
}
