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

package reaper

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestRunStartupSweepThenForcedSweep: Run sweeps once at start (young trash
// survives the age gate), and a forced sweep requested through the engine's
// ENOSPC hook purges trash regardless of age.
func TestRunStartupSweepThenForcedSweep(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, Interval: time.Hour, TrashMaxAge: time.Hour})
	old := trashEntry(t, root, trashName(time.Now().Add(-2*time.Hour), "old"), 4)
	young := trashEntry(t, root, trashName(time.Now(), "young"), 4)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()
	waitGone(t, old, "startup sweep did not reclaim aged trash")
	mustExist(t, young)

	r.requestForceSweep()
	waitGone(t, young, "forced sweep did not purge young trash")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// TestRunTicksOnInterval: the ticker drives later sweeps.
func TestRunTicksOnInterval(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, Interval: 10 * time.Millisecond, TrashMaxAge: 50 * time.Millisecond})
	entry := trashEntry(t, root, trashName(time.Now(), "soon"), 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)
	waitGone(t, entry, "Run never swept after the entry aged")
}

// requestForceSweep never blocks, even with no Run loop draining it.
func TestRequestForceSweepNeverBlocks(t *testing.T) {
	r := newReaperForTest(t, t.TempDir(), Config{Budget: 1 << 30})
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 3; i++ {
			r.requestForceSweep()
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("requestForceSweep blocked")
	}
}

func TestSweep_LogLineIsValueFree(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1000})
	writeMirror(t, root, "acme/secret-project/r", 900, time.Now().Add(-time.Hour))
	r.sweep(context.Background())

	var line map[string]any
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, `"msg":"reaper.sweep"`) {
			if err := json.Unmarshal([]byte(l), &line); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if strings.Contains(l, "secret-project") {
				t.Fatalf("reaper.sweep names a repo: %s", l)
			}
		}
	}
	if line == nil {
		t.Fatalf("no reaper.sweep line in:\n%s", buf.String())
	}
	for _, k := range []string{"usedBytes", "budgetBytes", "pct", "evicted"} {
		if _, ok := line[k]; !ok {
			t.Errorf("reaper.sweep missing %q: %v", k, line)
		}
	}
	if line["evicted"] != float64(1) || line["budgetBytes"] != float64(1000) {
		t.Errorf("reaper.sweep = %v, want evicted 1, budgetBytes 1000", line)
	}
}

func waitGone(t *testing.T, path, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !gone(path) {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
