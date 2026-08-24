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

package run

import (
	"context"
	"log/slog"
	"time"

	"go.temporal.io/sdk/worker"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// dialRetryInterval paces the worker watcher's connection attempts while the
// Temporal server is unreachable.
const dialRetryInterval = 15 * time.Second

// WorkerWatcher implements the app.Watcher contract: it blocks on its context,
// dialing Temporal in a retry loop and then running the run-supervisor worker
// until shutdown. aep-api boots normally with Temporal down — the watcher just
// keeps retrying, and a build click that lands meanwhile settles its run row
// with a plan-failed reason rather than wedging the project's build mutex.
//
// There is ONE worker on the task queue and it belongs to this package now: a
// task queue must be served by one worker that knows every workflow on it, and
// the run supervisor is the only workflow left.
type WorkerWatcher struct {
	rt   *delivery.Runtime
	acts *Activities
}

// NewWorkerWatcher builds the watcher; nothing connects until Run.
func NewWorkerWatcher(rt *delivery.Runtime, acts *Activities) *WorkerWatcher {
	return &WorkerWatcher{rt: rt, acts: acts}
}

// Run dials until connected, then runs the worker until ctx is cancelled.
// A worker fatal error tears the client down and re-enters the dial loop.
func (w *WorkerWatcher) Run(ctx context.Context) {
	for {
		if err := w.rt.Dial(); err != nil {
			slog.Warn("run: temporal dial failed, retrying",
				"hostPort", w.rt.HostPort(), "interval", dialRetryInterval, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(dialRetryInterval):
				continue
			}
		}

		c, err := w.rt.Client()
		if err != nil {
			continue // raced with close; re-dial
		}
		// worker.Start returns as soon as the worker is running, so a later
		// fatal error would otherwise go unnoticed. OnFatalError surfaces it on
		// fatalCh; the select below then tears the client down and re-dials.
		fatalCh := make(chan error, 1)
		wk := worker.New(c, w.rt.TaskQueue(), worker.Options{
			OnFatalError: func(err error) {
				select {
				case fatalCh <- err:
				default:
				}
			},
		})
		Register(wk, w.acts)

		if err := wk.Start(); err != nil {
			slog.Error("run: worker start failed, re-dialing", "error", err)
			w.rt.Close()
			continue
		}
		slog.Info("run: temporal worker started",
			"hostPort", w.rt.HostPort(), "namespace", w.rt.Namespace(), "taskQueue", w.rt.TaskQueue())

		select {
		case <-ctx.Done():
			wk.Stop()
			w.rt.Close()
			return
		case err := <-fatalCh:
			slog.Error("run: temporal worker fatal error, re-dialing", "error", err)
			wk.Stop()
			w.rt.Close()
			// loop re-enters the dial retry
		}
	}
}
