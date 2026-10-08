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

package sourcecontrol

import (
	"slices"
	"strings"
	"sync"
	"time"
)

// recentIssueWindow is how long a filed issue is remembered. GitHub's issue
// list endpoint lags a creation by roughly 3-10 s; a minute is a comfortable
// margin that is still short enough for the memory never to outlive the lag.
const recentIssueWindow = time.Minute

// recentIssues remembers the issues this process filed in the last
// recentIssueWindow, per "owner/repo", so a list that races GitHub's own
// indexing can still show them (read-your-writes).
//
// In-process by design: correct for the single aep-api replica the deployment
// runs. With several replicas, a list answered by a replica that did not file
// the issue still sees GitHub's lag. The zero value is ready to use.
type recentIssues struct {
	mu     sync.Mutex
	byRepo map[string][]recentIssue
	now    func() time.Time // injectable clock; nil means time.Now
}

type recentIssue struct {
	info IssueInfo
	at   time.Time
}

func (r *recentIssues) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func recentRepoKey(owner, repo string) string { return owner + "/" + repo }

// remember records a freshly filed issue.
func (r *recentIssues) remember(owner, repo string, info IssueInfo) {
	key := recentRepoKey(owner, repo)
	now := r.clock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byRepo == nil {
		r.byRepo = make(map[string][]recentIssue)
	}
	r.byRepo[key] = append(r.pruned(key, now), recentIssue{info: info, at: now})
}

// merge adds to listed (GitHub's answer, newest first) every remembered issue
// the list does not yet carry and that satisfies the label filter, keeping the
// newest-first order. Entries that expired or that GitHub now lists are
// forgotten.
func (r *recentIssues) merge(owner, repo string, labels []string, listed []IssueInfo) []IssueInfo {
	key := recentRepoKey(owner, repo)
	now := r.clock()
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := r.pruned(key, now)
	if len(kept) == 0 {
		return listed
	}
	present := make(map[int]bool, len(listed))
	for _, iss := range listed {
		present[iss.Number] = true
	}
	live := kept[:0:0]
	merged := slices.Clone(listed)
	for _, entry := range kept {
		if present[entry.info.Number] {
			continue // GitHub caught up: the memory is spent
		}
		live = append(live, entry)
		if hasAllLabels(entry.info.Labels, labels) {
			merged = insertNewestFirst(merged, entry.info)
		}
	}
	if len(live) == 0 {
		delete(r.byRepo, key)
	} else {
		r.byRepo[key] = live
	}
	return merged
}

// pruned returns the repo's unexpired entries. Caller holds r.mu.
func (r *recentIssues) pruned(key string, now time.Time) []recentIssue {
	entries := r.byRepo[key]
	live := entries[:0:0]
	for _, e := range entries {
		if now.Sub(e.at) < recentIssueWindow {
			live = append(live, e)
		}
	}
	return live
}

// insertNewestFirst places iss before the first listed issue with a smaller
// number — issue numbers rise with creation time, which is the list's order.
func insertNewestFirst(list []IssueInfo, iss IssueInfo) []IssueInfo {
	i := 0
	for i < len(list) && list[i].Number > iss.Number {
		i++
	}
	return slices.Insert(list, i, iss)
}

// hasAllLabels reports whether have carries every wanted label, the rule
// GitHub applies to the list's labels filter (names compare case-insensitively).
func hasAllLabels(have, want []string) bool {
	for _, w := range want {
		if !slices.ContainsFunc(have, func(h string) bool { return strings.EqualFold(h, w) }) {
			return false
		}
	}
	return true
}
