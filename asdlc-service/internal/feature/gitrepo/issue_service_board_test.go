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

package gitrepo

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLockProjectBoard_SerializesPerProject is the regression guard for the
// two-board race: lazy board creation is guarded by a per-project mutex so
// concurrent CreateIssue calls cannot each create their own board. This asserts
// the primitive grants at most one holder per project at a time.
func TestLockProjectBoard_SerializesPerProject(t *testing.T) {
	s := &issueService{}
	var active, maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := s.lockProjectBoard("proj-A")
			defer unlock()
			cur := atomic.AddInt32(&active, 1)
			for {
				old := atomic.LoadInt32(&maxActive)
				if cur <= old || atomic.CompareAndSwapInt32(&maxActive, old, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("same-project lock allowed %d concurrent holders, want 1", maxActive)
	}
}

// TestLockProjectBoard_DifferentProjectsDoNotBlock asserts the lock is
// per-project — holding project A's lock must not block project B.
func TestLockProjectBoard_DifferentProjectsDoNotBlock(t *testing.T) {
	s := &issueService{}
	unlockA := s.lockProjectBoard("A")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := s.lockProjectBoard("B")
		unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lock for project B blocked on project A's lock — not per-project")
	}
}
