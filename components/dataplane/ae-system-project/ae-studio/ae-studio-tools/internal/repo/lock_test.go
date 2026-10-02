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

package repo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// Each acquisition opens its own fd, so two goroutines contend exactly like
// two processes (flock is per open file description).

func TestFlockExclusiveSerializes(t *testing.T) {
	locker := repo.NewLocker()
	path := filepath.Join(t.TempDir(), "repo.lock")
	ctx := context.Background()

	releaseA, err := locker.Lock(ctx, path)
	if err != nil {
		t.Fatalf("Lock A: %v", err)
	}
	acquired := make(chan time.Time, 1)
	go func() {
		releaseB, err := locker.Lock(ctx, path)
		if err != nil {
			t.Errorf("Lock B: %v", err)
			acquired <- time.Time{}
			return
		}
		acquired <- time.Now()
		releaseB()
	}()

	time.Sleep(100 * time.Millisecond) // B must be blocked now
	releasedAt := time.Now()
	releaseA()
	acquiredAt := <-acquired
	if acquiredAt.Before(releasedAt) {
		t.Fatalf("B acquired the EX lock at %v, before A released at %v", acquiredAt, releasedAt)
	}
}

func TestFlockSharedCoexist(t *testing.T) {
	locker := repo.NewLocker()
	path := filepath.Join(t.TempDir(), "repo.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	releaseA, err := locker.RLock(ctx, path)
	if err != nil {
		t.Fatalf("RLock A: %v", err)
	}
	defer releaseA()
	// A second shared holder must get in while the first is still held.
	releaseB, err := locker.RLock(ctx, path)
	if err != nil {
		t.Fatalf("RLock B while A held: %v", err)
	}
	releaseB()
}

func TestFlockExclusiveBlocksShared(t *testing.T) {
	locker := repo.NewLocker()
	path := filepath.Join(t.TempDir(), "repo.lock")

	releaseEX, err := locker.Lock(context.Background(), path)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	shortCtx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := locker.RLock(shortCtx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RLock under EX = %v, want deadline exceeded (blocked)", err)
	}
	releaseEX()
	// After release the shared lock goes straight through.
	releaseSH, err := locker.RLock(context.Background(), path)
	if err != nil {
		t.Fatalf("RLock after release: %v", err)
	}
	releaseSH()
}
