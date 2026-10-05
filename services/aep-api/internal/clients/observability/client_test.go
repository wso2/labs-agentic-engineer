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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
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
	// RELATIVE to now, deliberately. This was a fixed calendar date, and it
	// worked until real time drifted more than defaultLookback past it — after
	// which the clamp below took over, startTime stopped being `since`, and the
	// test failed for a reason that had nothing to do with the code. A date
	// literal in a test about a rolling window is a time bomb with a fuse the
	// length of that window.
	since := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
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

	// A since older than the lookback does not widen the window: the start is
	// clamped to now-defaultLookback rather than honoured.
	tooOld := since.Add(-2 * defaultLookback)
	if _, err := NewClient(srv.URL).GetBuildLogs(context.Background(), "acme", "shop", "web", "run-1", tooOld); err != nil {
		t.Fatalf("GetBuildLogs: %v", err)
	}
	start, err := time.Parse(time.RFC3339, got.StartTime)
	if err != nil {
		t.Fatalf("startTime %q is not RFC3339: %v", got.StartTime, err)
	}
	if start.Before(tooOld.Add(defaultLookback)) || time.Since(start) > defaultLookback+time.Minute {
		t.Fatalf("startTime = %q: an over-old since must be clamped to now-%s", got.StartTime, defaultLookback)
	}
}

// The other half of the same rule, and the half nothing asserted: a `since`
// OLDER than the lookback does NOT widen the window. Reading it as given would
// let one caller ask the observability plane for an unbounded scan.
func TestGetBuildLogs_SinceOlderThanTheLookbackIsClamped(t *testing.T) {
	since := time.Now().UTC().Add(-2 * defaultLookback)
	var got capturedQuery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = decodeQuery(t, r)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"logs": []interface{}{}, "total": 0})
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).GetBuildLogs(context.Background(), "acme", "shop", "web", "run-1", since); err != nil {
		t.Fatalf("GetBuildLogs: %v", err)
	}
	start, err := time.Parse(time.RFC3339, got.StartTime)
	if err != nil {
		t.Fatalf("startTime %q does not parse: %v", got.StartTime, err)
	}
	if !start.After(since) {
		t.Fatalf("startTime = %q, want the lookback floor — a since older than %s must not widen the window", got.StartTime, defaultLookback)
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

const (
	cycleComponent = "shop-ca-abc"
	cycleUID       = "11111111-2222-3333-4444-555555555555"
)

var t0 = time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)

// at is second `sec` after t0, `ms` milliseconds in. ms starts at 1: a line
// stamped exactly on a whole second is invisible to a window that starts on
// that second (`gt`), which is the observer's rule, not the client's.
func at(sec, ms int) time.Time {
	return t0.Add(time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond)
}

// inSecond returns n of the cycle's lines spread over second `sec`, labelled
// from `first`.
func inSecond(sec, n, first int) []fakeLine {
	out := make([]fakeLine, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fakeLine{
			At:            at(sec, 1+i*998/max(n, 1)),
			Log:           fmt.Sprintf(`{"v":2,"seq":%d}`, first+i),
			ComponentName: cycleComponent,
			ComponentUID:  cycleUID,
			PodName:       "shop-ca-abc-pod-1",
		})
	}
	return out
}

func concat(parts ...[]fakeLine) []fakeLine {
	var out []fakeLine
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func componentQuery() CycleLogQuery {
	return CycleLogQuery{
		Namespace: "acme", Project: "shop", Environment: "development",
		Component: cycleComponent, ComponentUID: cycleUID,
		From: t0, To: t0.Add(time.Hour),
	}
}

// assertExactly checks the read returned every indexed line once, in order.
func assertExactly(t *testing.T, got []LogLine, want []fakeLine) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d", len(got), len(want))
	}
	seen := make(map[string]bool, len(got))
	for i := range got {
		if seen[got[i].Log] {
			t.Fatalf("line %q returned twice", got[i].Log)
		}
		seen[got[i].Log] = true
		if got[i].Log != want[i].Log {
			t.Fatalf("line %d = %q, want %q (order must follow the index)", i, got[i].Log, want[i].Log)
		}
	}
}

// assertSameLines checks the read returned every indexed line once. Order is
// checked across seconds only: the observer stamps lines to the second, and a
// second read from both ends cannot be put back in index order within itself.
func assertSameLines(t *testing.T, got []LogLine, want []fakeLine) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w.Log] = true
	}
	seen := make(map[string]bool, len(got))
	for i, g := range got {
		if seen[g.Log] {
			t.Fatalf("line %q returned twice", g.Log)
		}
		if !wantSet[g.Log] {
			t.Fatalf("line %q was not indexed", g.Log)
		}
		seen[g.Log] = true
		if i > 0 && g.Timestamp.Before(got[i-1].Timestamp) {
			t.Fatalf("line %d (%v) is older than line %d (%v)", i, g.Timestamp, i-1, got[i-1].Timestamp)
		}
	}
	if len(seen) != len(wantSet) {
		t.Fatalf("lines = %d, want %d (some indexed lines were not returned)", len(seen), len(wantSet))
	}
}

// The observer has no cursor and stamps every line to the second, so a page
// that ends inside second S cannot say where in S it stopped. The read drops
// S, re-asks from S-1s, and keeps only S onwards: S arrives whole, once.
func TestQueryCycleLogs_BoundarySecondIsReadWhole(t *testing.T) {
	t.Run("page ends five lines into second 11", func(t *testing.T) {
		// The brief's shape: a 1000-line page that is all second 10 but for its
		// last 5 lines (second 11), then 7 more lines in second 11.
		lines := concat(inSecond(10, 995, 0), inSecond(11, 12, 995))
		obs, srv := newFakeObserver(t, lines)

		got, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
		if err != nil {
			t.Fatalf("QueryCycleLogs: %v", err)
		}
		assertExactly(t, got, lines)
		if n := len(obs.requestLog()); n > 4 {
			t.Fatalf("requests = %d: the read must make progress, not re-ask the same window", n)
		}
	})
	t.Run("page holds second 10 alone", func(t *testing.T) {
		// 1000 lines in second 10, then 12 in second 11: 1012 lines.
		lines := concat(inSecond(10, 1000, 0), inSecond(11, 12, 1000))
		_, srv := newFakeObserver(t, lines)

		got, stats, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
		if err != nil {
			t.Fatalf("QueryCycleLogs: %v", err)
		}
		assertSameLines(t, got, lines)
		if stats.LinesMissing {
			t.Fatal("a complete read must not report lines missing")
		}
	})
	t.Run("previous second is light", func(t *testing.T) {
		// The common case: the re-read of S-1s..S is small, so one extra page.
		lines := concat(inSecond(9, 600, 0), inSecond(10, 395, 600), inSecond(11, 12, 995))
		obs, srv := newFakeObserver(t, lines)

		got, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
		if err != nil {
			t.Fatalf("QueryCycleLogs: %v", err)
		}
		assertExactly(t, got, lines)
		reqs := obs.requestLog()
		if len(reqs) != 2 {
			t.Fatalf("requests = %d, want 2", len(reqs))
		}
		if got, want := reqs[1]["startTime"], at(10, 0).Format(time.RFC3339); got != want {
			t.Fatalf("second page startTime = %v, want %v (the boundary second minus one)", got, want)
		}
	})
}

// A second with more lines than a page cannot be paged forward through at all.
// It is read once from each end; up to two pages' worth it is exact.
func TestQueryCycleLogs_SaturatedSecondIsReadFromBothEnds(t *testing.T) {
	lines := concat(inSecond(9, 3, 0), inSecond(10, 1500, 3), inSecond(11, 4, 1503))
	_, srv := newFakeObserver(t, lines)

	got, stats, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	assertSameLines(t, got, lines)
	if stats.LinesMissing {
		t.Fatal("a second of two pages or fewer is read exactly")
	}
}

// OpenSearch breaks timestamp ties by document order ascending in BOTH sort
// directions, so a same-millisecond group cut by the ascending page is not the
// mirror image of the same group in the descending read. Joining the two by
// position duplicates part of such a group and loses the rest.
func TestQueryCycleLogs_SaturatedSecondTiesStraddlingTheCut(t *testing.T) {
	// 1500 lines in second 10, three per millisecond: the asc page (1000) ends
	// one line into a group, and the desc page (1000) ends one line into one.
	var sec10 []fakeLine
	for i := 0; i < 1500; i++ {
		sec10 = append(sec10, fakeLine{
			At: at(10, 1+i/3), Log: fmt.Sprintf(`{"v":2,"seq":%d}`, i),
			ComponentName: cycleComponent, ComponentUID: cycleUID, PodName: "shop-ca-abc-pod-1",
		})
	}
	lines := concat(sec10, inSecond(11, 4, 1500))
	_, srv := newFakeObserver(t, lines)

	got, stats, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	assertSameLines(t, got, lines)
	if stats.LinesMissing {
		t.Fatal("every line was returned; none may be reported missing")
	}
}

// Identical lines in one second cannot be told apart by anything the observer
// returns. The join never duplicates them and says what it could not read.
func TestQueryCycleLogs_IdenticalLinesInASaturatedSecondAreReportedMissing(t *testing.T) {
	var same []fakeLine
	for i := 0; i < 1200; i++ {
		same = append(same, fakeLine{At: at(10, 1+i%999), Log: "retrying", ComponentName: cycleComponent, ComponentUID: cycleUID, PodName: "shop-ca-abc-pod-1"})
	}
	_, srv := newFakeObserver(t, same)
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	got, stats, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	if len(got) != queryPageLimit {
		t.Fatalf("lines = %d, want %d (each read saw %d; more would be a guess)", len(got), queryPageLimit, queryPageLimit)
	}
	if !stats.LinesMissing {
		t.Fatal("200 lines were not returned; the read must say so")
	}
	if !strings.Contains(logged.String(), `"msg":"observer.read_incomplete"`) || !strings.Contains(logged.String(), `"missing":"200"`) {
		t.Fatalf("the missing lines must be logged, got %s", logged.String())
	}
}

// The stats carry the request count the caller logs.
func TestQueryCycleLogs_StatsCountThePages(t *testing.T) {
	lines := concat(inSecond(9, 600, 0), inSecond(10, 395, 600), inSecond(11, 12, 995))
	_, srv := newFakeObserver(t, lines)

	_, stats, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	if stats.Pages != 2 || stats.LinesMissing {
		t.Fatalf("stats = %+v, want 2 pages, nothing missing", stats)
	}
}

// Beyond two pages in one second the middle is unreadable on this API. The read
// returns both ends without duplicates and moves on rather than looping.
func TestQueryCycleLogs_OverSaturatedSecondMovesOn(t *testing.T) {
	lines := concat(inSecond(10, 2500, 0), inSecond(11, 4, 2500))
	obs, srv := newFakeObserver(t, lines)
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	got, stats, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	if !stats.LinesMissing {
		t.Fatal("500 lines of second 10 were not returned; the read must say so")
	}
	if len(got) != 2*queryPageLimit+4 {
		t.Fatalf("lines = %d, want %d (both ends of second 10, then second 11)", len(got), 2*queryPageLimit+4)
	}
	seen := map[string]bool{}
	for _, l := range got {
		if seen[l.Log] {
			t.Fatalf("line %q returned twice", l.Log)
		}
		seen[l.Log] = true
	}
	if got[len(got)-1].Log != lines[len(lines)-1].Log {
		t.Fatalf("last line = %q, want %q", got[len(got)-1].Log, lines[len(lines)-1].Log)
	}
	if n := len(obs.requestLog()); n > 4 {
		t.Fatalf("requests = %d, want the read to move past the saturated second", n)
	}
	if !strings.Contains(logged.String(), `"msg":"observer.read_incomplete"`) || !strings.Contains(logged.String(), `"missing":"500"`) {
		t.Fatalf("the unreadable middle must be logged, got %s", logged.String())
	}
}

// `total` is the match count of the request's window: a full page that holds
// all of it is the last page, without an empty follow-up request.
func TestQueryCycleLogs_StopsWhenThePageHoldsTheTotal(t *testing.T) {
	lines := concat(inSecond(10, 500, 0), inSecond(11, 500, 500))
	obs, srv := newFakeObserver(t, lines)

	got, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	assertExactly(t, got, lines)
	if n := len(obs.requestLog()); n != 1 {
		t.Fatalf("requests = %d, want 1", n)
	}
}

func TestQueryCycleLogs_ComponentScopeRequestShape(t *testing.T) {
	obs, srv := newFakeObserver(t, inSecond(10, 2, 0))

	got, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), componentQuery())
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	if len(got) != 2 || got[0].ComponentUID != cycleUID || got[0].PodName != "shop-ca-abc-pod-1" {
		t.Fatalf("unexpected lines: %+v", got)
	}
	if !got[0].Timestamp.Equal(at(10, 0)) {
		t.Fatalf("timestamp = %v, want %v (the observer's second)", got[0].Timestamp, at(10, 0))
	}
	req := obs.requestLog()[0]
	scope := req["searchScope"].(map[string]any)
	if scope["namespace"] != "acme" || scope["project"] != "shop" || scope["environment"] != "development" || scope["component"] != cycleComponent {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if _, ok := scope["workflowRunName"]; ok {
		t.Fatalf("a component scope must not carry workflowRunName: %+v", scope)
	}
	if _, ok := req["searchPhrase"]; ok {
		t.Fatalf("a component-scope read is already one Component's lines; got searchPhrase %v", req["searchPhrase"])
	}
	if req["sortOrder"] != "asc" || req["limit"] != float64(queryPageLimit) {
		t.Fatalf("unexpected paging: sortOrder=%v limit=%v", req["sortOrder"], req["limit"])
	}
}

// After the Component is deleted its name no longer resolves, so the read asks
// for the whole project and keeps only the lines its stored UID produced —
// including against a recreated Component of the same name, which has a new UID.
func TestQueryCycleLogs_ProjectScopeFiltersOnUIDAndSendsSearchPhrase(t *testing.T) {
	mine := inSecond(10, 3, 0)
	sameNameNewUID := fakeLine{At: at(10, 500), Log: `{"v":2,"seq":1}`, ComponentName: cycleComponent, ComponentUID: "99999999-0000-0000-0000-000000000000", PodName: "shop-ca-abc-pod-9"}
	otherComponent := fakeLine{At: at(10, 600), Log: `{"v":2,"seq":7}`, ComponentName: "shop-web", ComponentUID: "77777777-0000-0000-0000-000000000000", PodName: "shop-web-1"}
	notRunner := fakeLine{At: at(10, 700), Log: "GET /health 200", ComponentName: "shop-web", ComponentUID: "77777777-0000-0000-0000-000000000000", PodName: "shop-web-1"}
	obs, srv := newFakeObserver(t, concat(mine, []fakeLine{sameNameNewUID, otherComponent, notRunner}))

	q := componentQuery()
	q.Component = ""
	got, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), q)
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	assertExactly(t, got, mine)
	req := obs.requestLog()[0]
	scope := req["searchScope"].(map[string]any)
	if _, ok := scope["component"]; ok {
		t.Fatalf("a project-scope read must not name a component: %+v", scope)
	}
	if scope["project"] != "shop" || scope["environment"] != "development" {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if req["searchPhrase"] != `"v":2` {
		t.Fatalf("searchPhrase = %v, want %q", req["searchPhrase"], `"v":2`)
	}
}

// Paging and the UID filter are separate: a page full of OTHER Components'
// lines still means "there is more", even though none of it is kept.
func TestQueryCycleLogs_ProjectScopePagesPastOtherComponents(t *testing.T) {
	var other []fakeLine
	for i := 0; i < 1200; i++ {
		other = append(other, fakeLine{At: at(i/100, 1+i%100), Log: fmt.Sprintf(`{"v":2,"seq":%d,"other":1}`, i), ComponentName: "shop-web", ComponentUID: "77777777-0000-0000-0000-000000000000", PodName: "shop-web-1"})
	}
	mine := inSecond(20, 2, 0)
	_, srv := newFakeObserver(t, concat(other, mine))

	q := componentQuery()
	q.Component = ""
	got, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), q)
	if err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	assertExactly(t, got, mine)
}

func TestQueryCycleLogs_ForwardsTheCallerBearerOnly(t *testing.T) {
	obs, srv := newFakeObserver(t, inSecond(10, 1, 0))
	c := NewClient(srv.URL)

	if _, _, err := c.QueryCycleLogs(auth.WithAuthToken(context.Background(), "user-jwt"), componentQuery()); err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	if _, _, err := c.QueryCycleLogs(context.Background(), componentQuery()); err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	got := obs.authHeaders()
	if len(got) != 2 || got[0] != "Bearer user-jwt" || got[1] != "" {
		t.Fatalf("Authorization headers = %q, want [\"Bearer user-jwt\" \"\"]", got)
	}
}

// The window is the cycle's, chosen by the caller. The old read fell back to a
// 30-day lookback; a cycle read never asks for more than it ran.
func TestQueryCycleLogs_WindowIsTheCallersNotThirtyDays(t *testing.T) {
	obs, srv := newFakeObserver(t, nil)
	q := componentQuery()
	q.From, q.To = at(-1, 0), at(3600+60, 0)

	if _, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), q); err != nil {
		t.Fatalf("QueryCycleLogs: %v", err)
	}
	req := obs.requestLog()[0]
	if req["startTime"] != q.From.Format(time.RFC3339) || req["endTime"] != q.To.Format(time.RFC3339) {
		t.Fatalf("window = %v..%v, want %s..%s", req["startTime"], req["endTime"], q.From.Format(time.RFC3339), q.To.Format(time.RFC3339))
	}
}

func TestQueryCycleLogs_RejectsAnIncompleteQueryWithoutCalling(t *testing.T) {
	obs, srv := newFakeObserver(t, nil)
	cases := map[string]func(*CycleLogQuery){
		"no component UID": func(q *CycleLogQuery) { q.ComponentUID = "" },
		"no window start":  func(q *CycleLogQuery) { q.From = time.Time{} },
		"no window end":    func(q *CycleLogQuery) { q.To = time.Time{} },
		"no namespace":     func(q *CycleLogQuery) { q.Namespace = "" },
		"no project":       func(q *CycleLogQuery) { q.Project = "" },
		"no environment":   func(q *CycleLogQuery) { q.Environment = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			q := componentQuery()
			mutate(&q)
			if _, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), q); err == nil {
				t.Fatal("want an error")
			}
		})
	}
	if n := len(obs.requestLog()); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

func TestQueryCycleLogs_NonOKIsAnError(t *testing.T) {
	_, srv := newFakeObserver(t, nil)
	q := componentQuery()
	q.To = q.From.Add(31 * 24 * time.Hour) // the observer refuses > 30 days with a 400

	if _, _, err := NewClient(srv.URL).QueryCycleLogs(context.Background(), q); err == nil {
		t.Fatal("a 400 must surface as an error")
	}
}
