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

package credentials

import (
	"sync"
	"time"
)

// appTokenCache caches App-installation tokens per installationID, keyed
// by deadline with a 5-minute safety margin. Process-local; restart drops
// it. Multi-replica deployments populate each replica's cache
// independently — the small redundancy is accepted (1 extra mint per
// replica per token-window) over a distributed cache.
//
// Shape mirrors the doc's evolution-doc §9.10 + phase2.md §7.4 contract.
type appTokenCache struct {
	mu      sync.Mutex
	entries map[int64]appTokenEntry
}

type appTokenEntry struct {
	token     string
	expiresAt time.Time
}

// safetyMargin is the headroom subtracted from the GitHub-supplied
// expires_at before the cache treats a token as expired. 5 minutes is
// large enough to absorb in-flight long-running git operations and
// covers normal clock skew.
const safetyMargin = 5 * time.Minute

func newAppTokenCache() *appTokenCache {
	return &appTokenCache{entries: make(map[int64]appTokenEntry)}
}

// get returns the cached entry if it still has > safetyMargin until expiry.
func (c *appTokenCache) get(installationID int64) (appTokenEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[installationID]
	if !ok {
		return appTokenEntry{}, false
	}
	if time.Until(entry.expiresAt) <= safetyMargin {
		return appTokenEntry{}, false
	}
	return entry, true
}

func (c *appTokenCache) put(installationID int64, entry appTokenEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[installationID] = entry
}

func (c *appTokenCache) evict(installationID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, installationID)
}
