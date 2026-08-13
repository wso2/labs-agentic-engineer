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

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type capturedQuery struct {
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	Limit       int    `json:"limit"`
	SortOrder   string `json:"sortOrder"`
	SearchScope struct {
		Namespace       string `json:"namespace"`
		Project         string `json:"project"`
		Component       string `json:"component"`
		Environment     string `json:"environment"`
		WorkflowRunName string `json:"workflowRunName"`
	} `json:"searchScope"`
}

func decodeQuery(t *testing.T, r *http.Request) capturedQuery {
	t.Helper()
	var q capturedQuery
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return q
}

// The old client POSTed /api/logs/build/{name}, an endpoint no observer has
// ever served, and read `totalCount` from a body that says `total`. Both are
// fixed here: a build IS a workflow run, so it queries the workflow scope.
func TestGetBuildLogs_UsesTheWorkflowScopeOnTheQueryEndpoint(t *testing.T) {
	var gotPath string
	var got capturedQuery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, got = r.URL.Path, decodeQuery(t, r)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []interface{}{
				map[string]interface{}{"timestamp": "2026-08-06T10:00:01Z", "log": "step 1", "level": "INFO"},
			},
			"total":  1,
			"tookMs": 3,
		})
	}))
	defer srv.Close()

	logs, err := NewClient(srv.URL).GetBuildLogs(context.Background(), "acme", "shop", "web", "shop-web-1754476800000", time.Time{})
	if err != nil {
		t.Fatalf("GetBuildLogs: %v", err)
	}
	if gotPath != "/api/v1/logs/query" {
		t.Fatalf("path = %q, want /api/v1/logs/query", gotPath)
	}
	if got.SearchScope.WorkflowRunName != "shop-web-1754476800000" || got.SearchScope.Namespace != "acme" {
		t.Fatalf("unexpected scope: %+v", got.SearchScope)
	}
	if got.SearchScope.Component != "" || got.SearchScope.Project != "" {
		t.Fatalf("a workflow scope must not carry component fields: %+v", got.SearchScope)
	}
	if got.SortOrder != "asc" || got.Limit != queryPageLimit {
		t.Fatalf("unexpected paging: sortOrder=%q limit=%d", got.SortOrder, got.Limit)
	}
	if len(logs.Logs) != 1 || logs.Logs[0].Log != "step 1" {
		t.Fatalf("unexpected logs: %+v", logs.Logs)
	}
	if logs.TotalCount != 1 {
		t.Fatalf("TotalCount = %d, want 1 (read from `total`)", logs.TotalCount)
	}
}

func TestGetBuildLogs_SinceNarrowsTheWindow(t *testing.T) {
	since := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	var got capturedQuery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decodeQuery(t, r)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"logs": []interface{}{}, "total": 0})
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).GetBuildLogs(context.Background(), "acme", "shop", "web", "run-1", since); err != nil {
		t.Fatalf("GetBuildLogs: %v", err)
	}
	if got.StartTime != since.Format(time.RFC3339) {
		t.Fatalf("startTime = %q, want %q", got.StartTime, since.Format(time.RFC3339))
	}
}

func TestGetBuildLogs_NonOKIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).GetBuildLogs(context.Background(), "a", "p", "c", "b", time.Time{}); err == nil {
		t.Fatal("a 403 must surface as an error")
	}
}

func TestQueryComponentLogs_UsesTheComponentScope(t *testing.T) {
	var got capturedQuery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decodeQuery(t, r)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []interface{}{
				map[string]interface{}{"timestamp": "2026-08-06T10:00:01Z", "log": "agent up"},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	from := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	lines, err := NewClient(srv.URL).QueryComponentLogs(context.Background(), ComponentLogQuery{
		Namespace: "acme", Project: "shop", Component: "shop-ca-abc", Environment: "development",
		From: from, To: from.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("QueryComponentLogs: %v", err)
	}
	if got.SearchScope.Component != "shop-ca-abc" || got.SearchScope.Environment != "development" {
		t.Fatalf("unexpected scope: %+v", got.SearchScope)
	}
	if got.SearchScope.WorkflowRunName != "" {
		t.Fatalf("a component scope must not carry workflowRunName: %+v", got.SearchScope)
	}
	if len(lines) != 1 || lines[0].Log != "agent up" {
		t.Fatalf("unexpected lines: %+v", lines)
	}
}

// The observer has no cursor and no offset, so a full page means "there is
// more" and the only way forward is to move the window past the last entry.
func TestQueryComponentLogs_PagesByAdvancingTheWindow(t *testing.T) {
	var starts []string
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := decodeQuery(t, r)
		starts = append(starts, q.StartTime)
		page++
		logs := make([]interface{}, 0, queryPageLimit)
		if page == 1 {
			for i := 0; i < queryPageLimit; i++ {
				logs = append(logs, map[string]interface{}{
					"timestamp": time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Second).Format(time.RFC3339),
					"log":       "line",
				})
			}
		} else {
			logs = append(logs, map[string]interface{}{"timestamp": "2026-08-06T11:00:00Z", "log": "last"})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"logs": logs, "total": len(logs)})
	}))
	defer srv.Close()

	from := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	lines, err := NewClient(srv.URL).QueryComponentLogs(context.Background(), ComponentLogQuery{
		Namespace: "acme", Project: "shop", Component: "shop-ca-abc", Environment: "development",
		From: from, To: from.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("QueryComponentLogs: %v", err)
	}
	if page != 2 {
		t.Fatalf("pages = %d, want 2", page)
	}
	if len(lines) != queryPageLimit+1 {
		t.Fatalf("lines = %d, want %d", len(lines), queryPageLimit+1)
	}
	if len(starts) != 2 || starts[1] == starts[0] {
		t.Fatalf("the second window must start later: %+v", starts)
	}
	if lines[len(lines)-1].Log != "last" {
		t.Fatalf("pages must be concatenated in order, got %q last", lines[len(lines)-1].Log)
	}
}
