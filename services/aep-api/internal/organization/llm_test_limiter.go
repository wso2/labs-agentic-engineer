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

package organization

import (
	"sync"
	"time"
)

// Test connection makes aep-api call a public host of the caller's choosing
// with a key of the caller's choosing, so it is rationed: this many calls per
// org per window. A real Settings session tests a handful of times.
const (
	llmTestsPerWindow = 10
	llmTestWindow     = time.Minute
)

// llmTestLimiter is a per-org sliding-window counter. It is per replica: with
// N replicas an org gets up to N×10 a minute, which still bounds the abuse the
// limit exists for (turning aep-api into a free prober of public hosts).
type llmTestLimiter struct {
	mu    sync.Mutex
	now   func() time.Time
	calls map[string][]time.Time // org → call times inside the window, oldest first
}

func newLLMTestLimiter(now func() time.Time) *llmTestLimiter {
	return &llmTestLimiter{now: now, calls: map[string][]time.Time{}}
}

// allow records a call for org and reports whether it is within the limit. A
// refused call is not recorded, so a client that backs off regains its quota
// as the window slides.
func (l *llmTestLimiter) allow(org string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-llmTestWindow)
	kept := l.calls[org][:0]
	for _, t := range l.calls[org] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= llmTestsPerWindow {
		l.calls[org] = kept
		return false
	}
	l.calls[org] = append(kept, now)
	return true
}
