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

package observability

// observer_fake_test.go — an httptest observer that answers
// POST /api/v1/logs/query the way OpenChoreo 1.2.5 and its OpenSearch logs
// adapter do, not the way a client would like it to:
//
//   - validation (OC internal/observer/api/handlers/validations.go): RFC3339
//     startTime/endTime, end not before start, window ≤ 30 days, limit 0 → 100
//     and > 1000 → 400, sortOrder asc|desc (default desc), namespace required,
//     component without project → 400, workflowRunName mixed with component
//     fields → 400;
//   - query (community-modules observability-logs-opensearch queries.go): the
//     window is re-formatted to whole seconds and matched `gt start, lt end`
//     (both exclusive), sorted on the timestamp only, `size: limit`;
//     searchPhrase is a `*phrase*` wildcard on the log text; equal timestamps
//     keep document order ascending whichever way the sort runs (Lucene);
//   - response (OC service/logs.go): timestamps at SECOND precision, the
//     metadata block, and `total` = the match count of THAT request's window.
//
// Lines are stored with millisecond timestamps, like the index, so a test can
// put many lines in one second the client can only see as one second.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLine is one indexed line. ComponentName is the SCOPED component name the
// control plane resolves to ComponentUID.
type fakeLine struct {
	At            time.Time
	Log           string
	ComponentName string
	ComponentUID  string
	PodName       string
}

type fakeObserver struct {
	t         *testing.T
	namespace string
	project   string
	env       string

	mu       sync.Mutex
	lines    []fakeLine
	requests []map[string]any
	auth     []string
}

func newFakeObserver(t *testing.T, lines []fakeLine) (*fakeObserver, *httptest.Server) {
	t.Helper()
	f := &fakeObserver{t: t, namespace: "acme", project: "shop", env: "development", lines: lines}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func badRequest(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"title": "badRequest", "errorCode": "", "message": msg})
}

func (f *fakeObserver) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/api/v1/logs/query" {
		http.NotFound(w, r)
		return
	}
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		badRequest(w, "Invalid request format")
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, raw)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()

	scope, _ := raw["searchScope"].(map[string]any)
	if scope == nil {
		badRequest(w, "searchScope is required")
		return
	}
	str := func(m map[string]any, k string) string { s, _ := m[k].(string); return strings.TrimSpace(s) }
	_, hasRun := scope["workflowRunName"]
	_, hasComp := scope["component"]
	_, hasProj := scope["project"]
	_, hasEnv := scope["environment"]
	if hasRun && (hasComp || hasProj || hasEnv) {
		badRequest(w, "searchScope cannot mix workflowRunName with project/component/environment")
		return
	}
	if str(scope, "namespace") == "" {
		badRequest(w, "searchScope.namespace is required")
		return
	}
	if str(scope, "component") != "" && str(scope, "project") == "" {
		badRequest(w, "searchScope.project is required when searchScope.component is provided")
		return
	}
	start, err1 := time.Parse(time.RFC3339, str(raw, "startTime"))
	end, err2 := time.Parse(time.RFC3339, str(raw, "endTime"))
	if err1 != nil || err2 != nil {
		badRequest(w, "startTime/endTime must be in RFC3339 format")
		return
	}
	if end.Before(start) {
		badRequest(w, "endTime must be after startTime")
		return
	}
	if end.Sub(start) > 30*24*time.Hour {
		badRequest(w, "query time range cannot exceed 30 days")
		return
	}
	limit := 100
	if l, ok := raw["limit"].(float64); ok && l != 0 {
		if l < 0 || l > 1000 {
			badRequest(w, "limit cannot exceed 1000")
			return
		}
		limit = int(l)
	}
	order := "desc"
	if o := str(raw, "sortOrder"); o != "" {
		if o != "asc" && o != "desc" {
			badRequest(w, "sortOrder must be either 'asc' or 'desc'")
			return
		}
		order = o
	}
	// The adapter formats the window with time.RFC3339: sub-second precision
	// is gone before OpenSearch sees it.
	start, end = start.Truncate(time.Second), end.Truncate(time.Second)

	var match []fakeLine
	f.mu.Lock()
	for _, l := range f.lines {
		if !l.At.After(start) || !l.At.Before(end) {
			continue
		}
		if str(scope, "namespace") != f.namespace {
			continue
		}
		if p := str(scope, "project"); p != "" && p != f.project {
			continue
		}
		if e := str(scope, "environment"); e != "" && e != f.env {
			continue
		}
		if c := str(scope, "component"); c != "" && c != l.ComponentName {
			continue
		}
		if p := str(raw, "searchPhrase"); p != "" && !strings.Contains(l.Log, p) {
			continue
		}
		match = append(match, l)
	}
	f.mu.Unlock()
	// Lucene's rule: sort on the timestamp only, ties broken by document
	// order ASCENDING in both directions — desc is NOT the reverse of asc.
	// f.lines is in document (insertion) order, so a stable sort is exact.
	sort.SliceStable(match, func(i, j int) bool {
		if order == "desc" {
			return match[i].At.After(match[j].At)
		}
		return match[i].At.Before(match[j].At)
	})
	total := len(match)
	if len(match) > limit {
		match = match[:limit]
	}
	logs := make([]map[string]any, 0, len(match))
	for _, l := range match {
		logs = append(logs, map[string]any{
			"timestamp": l.At.UTC().Format(time.RFC3339),
			"log":       l.Log,
			"level":     "INFO",
			"metadata": map[string]any{
				"componentName":   l.ComponentName,
				"projectName":     f.project,
				"environmentName": f.env,
				"namespaceName":   f.namespace,
				"componentUid":    l.ComponentUID,
				"projectUid":      "puid-shop",
				"environmentUid":  "euid-dev",
				"containerName":   "main",
				"podName":         l.PodName,
				"podNamespace":    "dp-acme-shop-development",
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"logs": logs, "total": total, "tookMs": 1})
}

func (f *fakeObserver) requestLog() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.requests...)
}

func (f *fakeObserver) authHeaders() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auth...)
}
