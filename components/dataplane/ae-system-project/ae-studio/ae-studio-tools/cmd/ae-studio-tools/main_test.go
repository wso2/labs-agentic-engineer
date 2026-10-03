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

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/edge"
	"github.com/wso2/aep/ae-studio-tools/internal/usage"
)

// recordingAEPAPI is record-turn-usage: it keeps every batch it was sent.
type recordingAEPAPI struct {
	mu      sync.Mutex
	records []usage.TurnRecord
}

func (a *recordingAEPAPI) post(_ context.Context, r []usage.TurnRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, r...)
	return nil
}

func (a *recordingAEPAPI) got() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.records)
}

// serveSocket serves h on a fresh Unix socket and answers the server and a
// client dialing it.
func serveSocket(t *testing.T, name string, h http.Handler) (*http.Server, *http.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("", "aest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, name)
	ln, err := edge.ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	t.Cleanup(client.CloseIdleConnections)
	return srv, client
}

const shutdownRecord = `{"turnId":"5f0c6c1e-6c39-4f0e-9a51-7a1d0e5a6b10","project":"greeter","conversationId":"6f0c6c1e-6c39-4f0e-9a51-7a1d0e5a6b10",` +
	`"kind":"browser","flow":"design","status":"failed","reason":"shutdown","code":"shutdown","baseRef":"a","skillsRef":"b",` +
	`"startedAt":"2026-10-03T10:00:00Z","finishedAt":"2026-10-03T10:01:00Z","model":"m","modelHost":"h",` +
	`"inputTokens":1,"outputTokens":1,"cacheReadTokens":0,"cacheCreationTokens":0}`

// 07 §10 / Q-5: a record the agent hands in during the drain window (its
// shutdown turn's) is delivered by the shutdown flush, which runs beside the
// Files socket's shutdown: a Files request still in flight does not hold
// it back, and the whole step stays inside the budget.
func TestShutdown_RecordHandedInDuringTheDrainIsDelivered(t *testing.T) {
	aep := &recordingAEPAPI{}
	sender := usage.New(aep.post)
	outbox := startUsageOutbox(sender)
	defer outbox.stopRun()

	mcpSock, mcpClient := serveSocket(t, "mcp.sock", edge.MCPSocketRoutes(edge.MCPSocketDeps{Usage: sender}))
	// A Files request (ae-collab's last flush) still in flight at shutdown.
	release := make(chan struct{})
	entered := make(chan struct{})
	filesSock, filesClient := serveSocket(t, "files.sock", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	go func() {
		if resp, err := filesClient.Get("http://files/slow"); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()
	<-entered

	// SIGTERM came; the MCP socket is still served in the drain window.
	resp, err := mcpClient.Post("http://mcp/turn-usage", "application/json", strings.NewReader(shutdownRecord))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /turn-usage = %d", resp.StatusCode)
	}
	if aep.got() != 0 {
		t.Fatal("delivered before the shutdown flush; the test proves nothing")
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- shutdownSockets(serverShutdown(filesSock), serverShutdown(mcpSock), outbox.flush) }()
	deadline := time.After(usageFlushTimeout)
	for aep.got() == 0 {
		select {
		case <-deadline:
			t.Fatal("the record was not delivered inside the flush budget while the Files socket drained")
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("shutdownSockets = %v", err)
	}
	if took := time.Since(start); took >= socketShutdownTimeout {
		t.Fatalf("shutdown took %v", took)
	}
}

// A flush aep-api never answers ends at usageFlushTimeout, inside the
// socket shutdown's budget, with the failure.
func TestShutdown_FlushIsBounded(t *testing.T) {
	sender := usage.New(func(ctx context.Context, _ []usage.TurnRecord) error {
		<-ctx.Done()
		return ctx.Err()
	})
	outbox := startUsageOutbox(sender)
	defer outbox.stopRun()
	sender.Enqueue(usage.TurnRecord{Project: "greeter"})

	start := time.Now()
	err := shutdownSockets(outbox.flush)
	took := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdownSockets = %v, want the flush deadline", err)
	}
	if took < usageFlushTimeout || took > usageFlushTimeout+time.Second {
		t.Fatalf("flush took %v, want about %v", took, usageFlushTimeout)
	}
}

// The budget the pod's 30 s termination grace must hold (Q-5).
func TestShutdownBudget_InsideTheGrace(t *testing.T) {
	const grace = 30 * time.Second
	if usageFlushTimeout > socketShutdownTimeout {
		t.Fatal("the usage flush must fit the concurrent socket shutdown")
	}
	if total := socketDrainWindow + socketShutdownTimeout + reaperStopTimeout; total >= grace {
		t.Fatalf("shutdown budget %v does not fit the %v grace", total, grace)
	}
}
