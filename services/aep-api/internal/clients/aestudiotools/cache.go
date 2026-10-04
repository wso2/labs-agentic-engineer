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

package aestudiotools

// cache.go — the one bounded LRU of immutable reads (05 §4): an answer read
// at a 40-hex commit sha never changes, so it is kept, keyed by the repo, the
// sha, the op and its canonical arguments, and served without the hop.
// Mutable reads (a branch tip, a tag's first resolution) always make the hop;
// their answer is stored only under the sha it returned.

import (
	"container/list"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// commitSHA is a full lowercase commit id, the only `at` (or reply) that
// addresses immutable content.
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isCommitSHA(s string) bool { return commitSHA.MatchString(s) }

// readKey is the cache key of op at sha in ref's repository with args. The
// org is part of it: the pod's owner guard runs per org, and a hit must not
// serve one org what only another's pod would answer. args is any value
// whose JSON form is canonical for the call (sorted lists).
func readKey(ref RepoRef, sha, op string, args any) string {
	b, _ := json.Marshal(args) // args are plain structs of strings: never fails
	return ref.Org + "|" + strings.ToLower(ref.Owner+"/"+ref.Repo) + "|" + sha + "|" + op + "|" + string(b)
}

// bundleArgs is read-bundle's canonical argument set: exts and paths sorted.
func bundleArgs(f sourcecontrol.BundleFilter) any {
	exts, paths := slices.Clone(f.Exts), slices.Clone(f.Paths)
	slices.Sort(exts)
	slices.Sort(paths)
	return struct {
		Prefix string   `json:"prefix"`
		Exts   []string `json:"exts"`
		Paths  []string `json:"paths"`
	}{f.Prefix, exts, paths}
}

// readCache is a byte-bounded LRU. A value is never handed out or kept by
// reference: callers store and receive copies.
type readCache struct {
	max int64

	mu    sync.Mutex
	used  int64
	order *list.List // front = most recently used
	items map[string]*list.Element
}

type cacheEntry struct {
	key   string
	value any
	size  int64
}

func newReadCache(maxBytes int64) *readCache {
	return &readCache{max: maxBytes, order: list.New(), items: map[string]*list.Element{}}
}

// get answers the value under key and marks it most recently used.
func (c *readCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).value, true
}

// put stores value under key, sized size bytes of content (the key's own
// length is added), evicting the least recently used entries to fit. A value
// larger than the whole cache is not stored.
func (c *readCache) put(key string, value any, size int64) {
	size += int64(len(key))
	if size > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.used -= el.Value.(*cacheEntry).size
		c.order.Remove(el)
		delete(c.items, key)
	}
	for c.used+size > c.max {
		oldest := c.order.Back()
		e := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.items, e.key)
		c.used -= e.size
	}
	c.items[key] = c.order.PushFront(&cacheEntry{key: key, value: value, size: size})
	c.used += size
}
