/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package repo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// gateClones holds every `git clone` until release is called, and counts them.
func gateClones(t *testing.T, e *repo.Engine) (clones *atomic.Int32, started <-chan struct{}, release func()) {
	t.Helper()
	clones = &atomic.Int32{}
	gate := make(chan struct{})
	first := make(chan struct{})
	var once, releaseOnce sync.Once
	repo.SetExecHook(e, func(args, _ []string) {
		if subcommand(args) != "clone" {
			return
		}
		clones.Add(1)
		once.Do(func() { close(first) })
		<-gate
	})
	release = func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(func() {
		release()
		repo.SetExecHook(e, nil)
	})
	return clones, first, release
}

// A cold clone outlives the request that started it: the cancelled caller
// stops waiting at once, the clone lands anyway, and the next call reuses it.
func TestColdCloneOutlivesACancelledRequest(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	clones, started, release := gateClones(t, fx.Engine)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := fx.Engine.Head(ctx, fx.Ref, "")
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled Head err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled request kept waiting for the clone")
	}

	release()
	gitDir := mirrorGitDir(t, fx)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the abandoned clone never landed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, want := mustHead(t, fx, ""), fx.Origin.HeadSHA(t); got != want {
		t.Fatalf("Head = %s, want %s", got, want)
	}
	if n := clones.Load(); n != 1 {
		t.Fatalf("clones = %d, want 1 (the next call reuses the landed clone)", n)
	}
}

// Concurrent first readers of one repository share one clone.
func TestConcurrentColdReadersShareOneClone(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	clones, started, release := gateClones(t, fx.Engine)

	const readers = 5
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := fx.Engine.Head(context.Background(), fx.Ref, "")
			errs <- err
		}()
	}
	<-started
	time.Sleep(100 * time.Millisecond) // the others reach the clone too
	release()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
	}
	if n := clones.Load(); n != 1 {
		t.Fatalf("clones = %d, want 1", n)
	}
}
