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

package thundersvc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// -- harness ------------------------------------------------------------------

// directoryCall is one request the stub saw, kept in arrival order. The
// directory surface has ordering rules that a set of requests cannot express
// (read-modify-write, delete-then-recreate), so the harness records the
// sequence rather than counting hits.
type directoryCall struct {
	Method  string
	Path    string // decoded, for routing and ordinary assertions
	RawPath string // as it went on the wire, for the path-escaping assertions
	Query   string
	Auth    string
	Body    []byte
}

func (c directoryCall) String() string { return c.Method + " " + c.Path }

// directoryStub is the thunderMock analogue for directory.go. It answers the
// two preflight calls every directory method makes — the system token, and the
// default-OU lookup for the methods that need one — and hands the rest to the
// test's own handler.
//
// The OU lookup IS recorded (CreateGroup must resolve the OU before it POSTs);
// the token call is not, because it is cached on the client and would only add
// noise to a sequence assertion.
type directoryStub struct {
	ouID   string // served at /organization-units/tree/default ("" → "default-ou")
	handle func(w http.ResponseWriter, r *http.Request, body []byte)

	mu    sync.Mutex
	calls []directoryCall
}

func (s *directoryStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	if s.ouID == "" {
		s.ouID = "default-ou"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		if r.Method == http.MethodPost && r.URL.Path == "/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
			return
		}

		s.record(directoryCall{
			Method: r.Method, Path: r.URL.Path, RawPath: r.URL.EscapedPath(),
			Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), Body: body,
		})

		if r.Method == http.MethodGet && r.URL.Path == "/organization-units/tree/default" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": s.ouID})
			return
		}
		if s.handle == nil {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.handle(w, r, body)
	}))
}

func (s *directoryStub) record(c directoryCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, c)
}

// seq is the recorded "METHOD /path" sequence, for an exact ordering assertion.
func (s *directoryStub) seq() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.String())
	}
	return out
}

func (s *directoryStub) snapshot() []directoryCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]directoryCall(nil), s.calls...)
}

// only returns the single recorded call matching method+path, failing when
// there is not exactly one — an assertion on "the POST body" is meaningless if
// the code issued two POSTs.
func (s *directoryStub) only(t *testing.T, method, path string) directoryCall {
	t.Helper()
	var hits []directoryCall
	for _, c := range s.snapshot() {
		if c.Method == method && c.Path == path {
			hits = append(hits, c)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly one %s %s, got %d (sequence: %v)", method, path, len(hits), s.seq())
	}
	return hits[0]
}

func wantSeq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("request sequence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("request sequence = %v, want %v", got, want)
		}
	}
}

func decodeCallBody(t *testing.T, c directoryCall, out any) {
	t.Helper()
	if err := json.Unmarshal(c.Body, out); err != nil {
		t.Fatalf("decode %s body %q: %v", c, c.Body, err)
	}
}

// intQuery reads a positive integer query parameter, failing the test when the
// client omitted it — a paging test must not silently pass on offset="".
func intQuery(t *testing.T, r *http.Request, key string) int {
	t.Helper()
	raw := r.URL.Query().Get(key)
	n, err := strconv.Atoi(raw)
	if err != nil {
		t.Errorf("%s %s: %s query param = %q, want an integer", r.Method, r.URL.Path, key, raw)
		return 0
	}
	return n
}

// pageWindow clamps [offset, offset+limit) to n.
func pageWindow(n, offset, limit int) (int, int) {
	start := min(offset, n)
	return start, min(start+limit, n)
}

// wantPagingQueries asserts the exact limit/offset of every listing request.
//
// The offsets are spelled out per case rather than derived from the page index,
// because the property under test is that the cursor advances by the number of
// records RECEIVED. A helper that recomputed page x requested-limit would agree
// with the bug it exists to catch.
func wantPagingQueries(t *testing.T, calls []directoryCall, wantOffsets ...int) {
	t.Helper()
	if len(calls) != len(wantOffsets) {
		var got []string
		for _, c := range calls {
			got = append(got, c.Query)
		}
		t.Fatalf("issued %d requests %v, want %d at offsets %v", len(calls), got, len(wantOffsets), wantOffsets)
	}
	for i, c := range calls {
		want := fmt.Sprintf("limit=%d&offset=%d", directoryPageSize, wantOffsets[i])
		if c.Query != want {
			t.Errorf("call %d query = %q, want %q", i, c.Query, want)
		}
	}
}

func makeGroups(n int) []Group {
	out := make([]Group, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Group{ID: fmt.Sprintf("g-%d", i), Name: fmt.Sprintf("role-%d", i), OUID: "default-ou"})
	}
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// -- morePages ----------------------------------------------------------------

func TestMorePages(t *testing.T) {
	// The stop rule is the whole of finding #2: a server is free to cap `limit`,
	// so "this page looked short" must never end a listing. Only "we have them
	// all" (totalResults) or "the server sent nothing" may.
	tests := []struct {
		name                 string
		got, total, thisPage int
		want                 bool
	}{
		{"empty first page", 0, 0, 0, false},
		{"empty page ends it even when totalResults claims more", 3, 99, 0, false},
		{"unknown total, non-empty page, keep draining", 5, 0, 5, true},
		{"a short page is NOT the end when the total is unknown", 3, 0, 3, true},
		{"a short page is NOT the end when the total says more", 3, 7, 3, true},
		{"exactly the total", 7, 7, 3, false},
		{"more than the total (server over-reported)", 8, 7, 3, false},
		{"one short of the total", 6, 7, 3, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := morePages(tc.got, tc.total, tc.thisPage); got != tc.want {
				t.Errorf("morePages(%d, %d, %d) = %v, want %v", tc.got, tc.total, tc.thisPage, got, tc.want)
			}
		})
	}
}

// -- ListGroups ---------------------------------------------------------------

// groupPager serves GET /groups as a window over `all`.
//
//   - reportTotal models a server that returns `totalResults`; without it the
//     client can only stop on an empty page.
//   - serverLimit > 0 models a server that CAPS the requested limit.
func groupPager(t *testing.T, all []Group, reportTotal bool, serverLimit int) func(http.ResponseWriter, *http.Request, []byte) {
	return func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodGet || r.URL.Path != "/groups" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		limit := intQuery(t, r, "limit")
		if serverLimit > 0 && limit > serverLimit {
			limit = serverLimit
		}
		lo, hi := pageWindow(len(all), intQuery(t, r, "offset"), limit)
		body := map[string]any{"groups": all[lo:hi]}
		if reportTotal {
			body["totalResults"] = len(all)
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}

func TestListGroups_PaginatesAndConcatenates(t *testing.T) {
	tests := []struct {
		name        string
		total       int
		reportTotal bool
		wantOffsets []int
	}{
		// With totalResults the client stops the moment it has them all.
		{"empty directory", 0, true, []int{0}},
		{"one partial page", 7, true, []int{0}},
		{"exactly one full page", directoryPageSize, true, []int{0}},
		{"two pages", directoryPageSize + 13, true, []int{0, 100}},
		{"three pages", 2*directoryPageSize + 1, true, []int{0, 100, 200}},
		// Without totalResults the only safe stop is an empty page, so it costs
		// one extra round trip rather than risking a partial answer. The second
		// offset is the count RECEIVED so far, not the page index times the
		// requested limit: the server decides how much it hands back, and the
		// cursor has to follow what arrived or it skips the gap. Here 7 records
		// came back, so the next window starts at 7 — asking for offset=100
		// would jump the cursor past records the server never sent.
		{"no totalResults, empty directory", 0, false, []int{0}},
		{"no totalResults, one partial page", 7, false, []int{0, 7}},
		{"no totalResults, exactly one full page", directoryPageSize, false, []int{0, 100}},
		{"no totalResults, two pages", directoryPageSize + 13, false, []int{0, 100, 113}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			all := makeGroups(tc.total)
			stub := &directoryStub{handle: groupPager(t, all, tc.reportTotal, 0)}
			srv := stub.server(t)
			defer srv.Close()

			got, err := newTestClient(srv.URL).ListGroups(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.total {
				t.Fatalf("got %d groups, want %d", len(got), tc.total)
			}
			for i := range got {
				if got[i].ID != all[i].ID {
					t.Fatalf("group %d = %q, want %q (pages concatenated out of order)", i, got[i].ID, all[i].ID)
				}
			}
			wantPagingQueries(t, stub.snapshot(), tc.wantOffsets...)
		})
	}
}

func TestListGroups_ServerCappedPageSize_ReturnsEverything(t *testing.T) {
	// The driver for finding #2: nothing obliges Thunder to honour limit=100.
	// A server that hands back 3 at a time while reporting totalResults: 7 must
	// still yield all 7, or FindGroupByName reports an existing role as absent
	// and the ensure 409s trying to create it.
	all := makeGroups(7)
	stub := &directoryStub{handle: groupPager(t, all, true, 3)}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).ListGroups(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(all) {
		t.Fatalf("got %d groups, want all %d — the listing truncated silently", len(got), len(all))
	}
	for i := range got {
		if got[i].ID != all[i].ID {
			t.Fatalf("group %d = %q, want %q", i, got[i].ID, all[i].ID)
		}
	}
	// Three windows of three, each starting where the last one actually ended.
	wantPagingQueries(t, stub.snapshot(), 0, 3, 6)
}

func TestListGroups_ErrorsPastMaxPages(t *testing.T) {
	// A directory that never settles must fail loudly rather than loop forever
	// or answer with a silent prefix of the truth.
	full := makeGroups(directoryPageSize)
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": full})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).ListGroups(context.Background())
	if err == nil {
		t.Fatalf("want an error past %d pages, got %d groups", maxDirectoryPages, len(got))
	}
	if got != nil {
		t.Errorf("want no partial result alongside the error, got %d groups", len(got))
	}
	if !strings.Contains(err.Error(), strconv.Itoa(maxDirectoryPages)) {
		t.Errorf("error should name the page cap, got %q", err)
	}
	if n := len(stub.snapshot()); n != maxDirectoryPages {
		t.Errorf("issued %d requests, want the cap %d", n, maxDirectoryPages)
	}
}

// -- FindGroupByName ----------------------------------------------------------

func TestFindGroupByName(t *testing.T) {
	// The platform treats two role names differing only in case as one role, so
	// the match folds case. Absent is (nil, false, nil), not an error.
	catalog := []Group{
		{ID: "g-1", Name: "Admin", Description: "admins"},
		{ID: "g-2", Name: "viewer"},
	}
	tests := []struct {
		name      string
		query     string
		wantFound bool
		wantID    string
	}{
		{"exact", "Admin", true, "g-1"},
		{"lowercased", "admin", true, "g-1"},
		{"uppercased", "VIEWER", true, "g-2"},
		{"absent", "editor", false, ""},
		{"substring is not a match", "Adm", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: groupPager(t, catalog, true, 0)}
			srv := stub.server(t)
			defer srv.Close()

			got, found, err := newTestClient(srv.URL).FindGroupByName(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if !tc.wantFound {
				if got != nil {
					t.Errorf("want a nil group when absent, got %+v", got)
				}
				return
			}
			if got == nil || got.ID != tc.wantID {
				t.Fatalf("got %+v, want id %q", got, tc.wantID)
			}
			// The returned group carries the catalog record, not just the id.
			if got.Name != catalog[0].Name && got.Name != catalog[1].Name {
				t.Errorf("group name = %q, want a catalog name", got.Name)
			}
		})
	}
}

func TestFindGroupByName_PropagatesListError(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"GRP-5000"}`))
	}}
	srv := stub.server(t)
	defer srv.Close()

	_, found, err := newTestClient(srv.URL).FindGroupByName(context.Background(), "admin")
	if err == nil {
		t.Fatal("want the listing error to surface, got nil")
	}
	if found {
		t.Error("found must be false when the lookup failed")
	}
}

// -- GroupMembers -------------------------------------------------------------

func memberPage(members []map[string]string, total int) map[string]any {
	return map[string]any{"totalResults": total, "members": members}
}

func TestGroupMembers_ReturnsOnlyUserMembers(t *testing.T) {
	// A nested group member is not an account that can sign in, so it is
	// dropped. The type comparison folds case because Thunder is not consistent
	// about it.
	members := []map[string]string{
		{"id": "u-1", "type": "user"},
		{"id": "grp-9", "type": "group"},
		{"id": "u-2", "type": "USER"},
		{"id": "role-4", "type": "role"},
		{"id": "u-3", "type": "User"},
	}
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path != "/groups/g-1/members" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(memberPage(members, len(members)))
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).GroupMembers(context.Background(), "g-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"u-1", "u-2", "u-3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("members = %v, want %v", got, want)
	}
}

func TestGroupMembers_PagesOnTheRawMemberCount(t *testing.T) {
	// Page 0 is a full page of mostly nested-group members. The paging window
	// advances by what the SERVER returned, not by the user-typed subset that
	// survived the filter — counting the filtered subset against totalResults
	// would keep asking for pages that are already behind the cursor.
	page0 := make([]map[string]string, 0, directoryPageSize)
	for i := 0; i < directoryPageSize; i++ {
		typ := "group"
		if i < 3 {
			typ = "user"
		}
		page0 = append(page0, map[string]string{"id": fmt.Sprintf("m-%d", i), "type": typ})
	}
	page1 := []map[string]string{{"id": "u-late", "type": "user"}}
	const total = directoryPageSize + 1

	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		page := page1
		if intQuery(t, r, "offset") == 0 {
			page = page0
		}
		_ = json.NewEncoder(w).Encode(memberPage(page, total))
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).GroupMembers(context.Background(), "g-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"m-0", "m-1", "m-2", "u-late"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("members = %v, want %v", got, want)
	}
	// Two requests, the second starting at the RAW member count of the first.
	wantPagingQueries(t, stub.snapshot(), 0, directoryPageSize)
}

func TestGroupMembers_ServerCappedPageSize_ReturnsEverything(t *testing.T) {
	// The same pin on the members paginator. A truncated membership read is
	// worse here than in the groups listing: AddGroupMembers writes the union of
	// what it read and what it was asked to add, so members it never saw are
	// silently dropped by the recreate.
	const serverLimit = 3
	all := make([]map[string]string, 0, 7)
	for i := 0; i < 7; i++ {
		all = append(all, map[string]string{"id": fmt.Sprintf("u-%d", i), "type": "user"})
	}
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		lo, hi := pageWindow(len(all), intQuery(t, r, "offset"), min(intQuery(t, r, "limit"), serverLimit))
		_ = json.NewEncoder(w).Encode(memberPage(all[lo:hi], len(all)))
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).GroupMembers(context.Background(), "g-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"u-0", "u-1", "u-2", "u-3", "u-4", "u-5", "u-6"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("members = %v, want all %v — the membership read truncated", got, want)
	}
	wantPagingQueries(t, stub.snapshot(), 0, 3, 6)
}

func TestGroupMembers_EscapesTheGroupIDInThePath(t *testing.T) {
	// An unescaped id is path injection: "a/b" would address a different
	// resource entirely. Ids come from Thunder today, which is exactly the kind
	// of assumption that stops holding.
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(memberPage(nil, 0))
	}}
	srv := stub.server(t)
	defer srv.Close()

	if _, err := newTestClient(srv.URL).GroupMembers(context.Background(), "a/b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := stub.snapshot()
	if len(got) != 1 {
		t.Fatalf("issued %d requests, want 1", len(got))
	}
	if got[0].RawPath != "/groups/a%2Fb/members" {
		t.Errorf("wire path = %q, want /groups/a%%2Fb/members", got[0].RawPath)
	}
}

// -- CreateGroup --------------------------------------------------------------

func TestCreateGroup_PostsMembersUnderTheDefaultOU(t *testing.T) {
	stub := &directoryStub{ouID: "ou-default", handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodPost || r.URL.Path != "/groups" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(Group{ID: "g-new", Name: "admin", Description: "the admins", OUID: "ou-default"})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).CreateGroup(context.Background(), "admin", "the admins", []string{"u-1", "u-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The OU must be resolved BEFORE the POST — the group has to land in the
	// same OU the generated app's OAuth client is registered under.
	wantSeq(t, stub.seq(), "GET /organization-units/tree/default", "POST /groups")

	post := stub.only(t, http.MethodPost, "/groups")
	if post.Auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want the system token", post.Auth)
	}
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		OuID        string `json:"ouId"`
		Members     []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"members"`
	}
	decodeCallBody(t, post, &body)
	if body.Name != "admin" || body.Description != "the admins" || body.OuID != "ou-default" {
		t.Errorf("posted %+v, want name=admin description=%q ouId=ou-default", body, "the admins")
	}
	if len(body.Members) != 2 {
		t.Fatalf("posted %d members, want 2", len(body.Members))
	}
	for i, want := range []string{"u-1", "u-2"} {
		if body.Members[i].ID != want || body.Members[i].Type != "user" {
			t.Errorf("member %d = %+v, want {id:%s type:user}", i, body.Members[i], want)
		}
	}
	if got.ID != "g-new" || got.Name != "admin" || got.OUID != "ou-default" {
		t.Errorf("returned %+v, want the created group", got)
	}
}

func TestCreateGroup_SendsAnEmptyMemberArrayNotNull(t *testing.T) {
	// Thunder validates `members` as an array; a JSON null is a 400. The
	// make(...) in createGroupLocked is what keeps this an empty array, and a
	// switch to `var members []map[string]string` would silently break it.
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(Group{ID: "g-new"})
	}}
	srv := stub.server(t)
	defer srv.Close()

	if _, err := newTestClient(srv.URL).CreateGroup(context.Background(), "empty", "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw := string(stub.only(t, http.MethodPost, "/groups").Body)
	if !strings.Contains(raw, `"members":[]`) {
		t.Errorf("body %s, want an empty members array", raw)
	}
}

// -- AddGroupMembers ----------------------------------------------------------

// addMembersStub answers the read-modify-write AddGroupMembers performs against
// one group: the members read, the delete, and the recreate.
func addMembersStub(t *testing.T, groupID string, existing []string, recreated Group, recreateStatus int) func(http.ResponseWriter, *http.Request, []byte) {
	return func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/groups/"+groupID+"/members":
			members := make([]map[string]string, 0, len(existing))
			for _, id := range existing {
				members = append(members, map[string]string{"id": id, "type": "user"})
			}
			_ = json.NewEncoder(w).Encode(memberPage(members, len(members)))
		case r.Method == http.MethodDelete && r.URL.Path == "/groups/"+groupID:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/groups":
			if recreateStatus != 0 {
				w.WriteHeader(recreateStatus)
				_, _ = w.Write([]byte(`{"code":"GRP-5001"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(recreated)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func TestAddGroupMembers_ReadsThenRecreatesWithTheUnion(t *testing.T) {
	// Thunder ignores `members` on update, so the only way to change membership
	// is delete-then-create with the merged list. The ORDER is the contract: a
	// create before the delete is a 409 on the unique name, and a recreate that
	// forgot to read first would silently drop everyone already in the group.
	recreated := Group{ID: "g-new", Name: "admin", Description: "the admins", OUID: "ou-1"}
	stub := &directoryStub{handle: addMembersStub(t, "g-old", []string{"u-1"}, recreated, 0)}
	srv := stub.server(t)
	defer srv.Close()

	old := Group{ID: "g-old", Name: "admin", Description: "the admins", OUID: "ou-1"}
	got, err := newTestClient(srv.URL).AddGroupMembers(context.Background(), old, []string{"u-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The group already carries its OU, so no default-OU lookup is needed.
	wantSeq(t, stub.seq(), "GET /groups/g-old/members", "DELETE /groups/g-old", "POST /groups")

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		OuID        string `json:"ouId"`
		Members     []struct {
			ID string `json:"id"`
		} `json:"members"`
	}
	decodeCallBody(t, stub.only(t, http.MethodPost, "/groups"), &body)
	if body.Name != "admin" || body.Description != "the admins" || body.OuID != "ou-1" {
		t.Errorf("recreated as %+v, want the original name/description/OU", body)
	}
	var ids []string
	for _, m := range body.Members {
		ids = append(ids, m.ID)
	}
	if strings.Join(sortedCopy(ids), ",") != "u-1,u-2" {
		t.Errorf("recreated with members %v, want the union {u-1,u-2}", ids)
	}

	// The id changes; a caller holding the old one must take this.
	if got.ID != "g-new" {
		t.Errorf("returned id %q, want the id of the recreated group", got.ID)
	}
}

func TestAddGroupMembers_NoOpWhenEveryMemberIsAlreadyPresent(t *testing.T) {
	// The only membership write Thunder offers is a delete-and-recreate, which
	// changes the group id. Doing that for nothing would churn the id on every
	// build — and briefly leave the role absent for no reason at all.
	stub := &directoryStub{handle: addMembersStub(t, "g-old", []string{"u-1", "u-2"}, Group{ID: "g-new"}, 0)}
	srv := stub.server(t)
	defer srv.Close()

	old := Group{ID: "g-old", Name: "admin", Description: "the admins", OUID: "ou-1"}
	got, err := newTestClient(srv.URL).AddGroupMembers(context.Background(), old, []string{"u-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSeq(t, stub.seq(), "GET /groups/g-old/members")
	if got != old {
		t.Errorf("returned %+v, want the group unchanged (%+v)", got, old)
	}
}

func TestAddGroupMembers_ResolvesDefaultOUWhenTheGroupHasNone(t *testing.T) {
	stub := &directoryStub{
		ouID:   "ou-default",
		handle: addMembersStub(t, "g-old", []string{"u-1"}, Group{ID: "g-new"}, 0),
	}
	srv := stub.server(t)
	defer srv.Close()

	old := Group{ID: "g-old", Name: "admin"}
	if _, err := newTestClient(srv.URL).AddGroupMembers(context.Background(), old, []string{"u-2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSeq(t, stub.seq(),
		"GET /organization-units/tree/default",
		"GET /groups/g-old/members",
		"DELETE /groups/g-old",
		"POST /groups")

	var body struct {
		OuID string `json:"ouId"`
	}
	decodeCallBody(t, stub.only(t, http.MethodPost, "/groups"), &body)
	if body.OuID != "ou-default" {
		t.Errorf("ouId = %q, want the resolved default OU", body.OuID)
	}
}

func TestAddGroupMembers_DoesNotDeleteWhenTheMembershipReadFails(t *testing.T) {
	// Without the current membership there is no union to write, and the delete
	// is not undoable. A failed read has to abort before anything destructive.
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodGet {
			t.Errorf("must not write after a failed read, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}}
	srv := stub.server(t)
	defer srv.Close()

	old := Group{ID: "g-old", Name: "project-admin", OUID: "ou-1"}
	got, err := newTestClient(srv.URL).AddGroupMembers(context.Background(), old, []string{"u-2"})
	if err == nil {
		t.Fatalf("want an error when the membership read fails, got %+v", got)
	}
	if !strings.Contains(err.Error(), "project-admin") {
		t.Errorf("error should name the group, got %q", err)
	}
	if got != (Group{}) {
		t.Errorf("want a zero Group alongside the error, got %+v", got)
	}
	wantSeq(t, stub.seq(), "GET /groups/g-old/members")
}

func TestAddGroupMembers_RecreateFailureSaysTheGroupIsGone(t *testing.T) {
	// The operation is not atomic: a failed recreate leaves the group deleted.
	// The error has to say which group and that the delete already happened,
	// because that is the difference between "nothing changed" and "the role
	// no longer exists until the next build".
	stub := &directoryStub{handle: addMembersStub(t, "g-old", []string{"u-1"}, Group{}, http.StatusInternalServerError)}
	srv := stub.server(t)
	defer srv.Close()

	old := Group{ID: "g-old", Name: "project-admin", OUID: "ou-1"}
	got, err := newTestClient(srv.URL).AddGroupMembers(context.Background(), old, []string{"u-2"})
	if err == nil {
		t.Fatalf("want an error when the recreate fails, got group %+v", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "project-admin") {
		t.Errorf("error should name the group, got %q", msg)
	}
	if !strings.Contains(msg, "after delete") {
		t.Errorf("error should say the delete already happened, got %q", msg)
	}
	if got != (Group{}) {
		t.Errorf("want a zero Group alongside the error, got %+v", got)
	}
	// Prove the delete really did land — otherwise the message above is a lie.
	wantSeq(t, stub.seq(), "GET /groups/g-old/members", "DELETE /groups/g-old", "POST /groups")
}

// -- the lost update ----------------------------------------------------------

// storedGroup is one group in groupStore.
type storedGroup struct {
	id, name, desc, ou string
	members            []string
	present            bool
}

// groupStore is a stateful Thunder group store for the read-modify-write race
// test. It is keyed by NAME, and every id the group has ever had resolves to
// the live record, so a caller holding a pre-recreate id still reads current
// membership — that keeps the test about the lost update rather than about id
// staleness. Recreating a name that is still present is a 409, as Thunder does.
type groupStore struct {
	mu        sync.Mutex
	byName    map[string]*storedGroup
	byID      map[string]*storedGroup
	next      int
	readDelay time.Duration
}

func newGroupStore(name, id string, members []string, readDelay time.Duration) *groupStore {
	g := &storedGroup{id: id, name: name, ou: "ou-1", members: members, present: true}
	return &groupStore{
		byName:    map[string]*storedGroup{name: g},
		byID:      map[string]*storedGroup{id: g},
		readDelay: readDelay,
	}
}

func (s *groupStore) membersOf(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.byName[name]
	if !ok {
		return nil
	}
	return append([]string(nil), g.members...)
}

func (s *groupStore) handle(t *testing.T) func(http.ResponseWriter, *http.Request, []byte) {
	return func(w http.ResponseWriter, r *http.Request, body []byte) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/members"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/groups/"), "/members")
			s.mu.Lock()
			g, ok := s.byID[id]
			live := ok && g.present
			var members []string
			if live {
				members = append([]string(nil), g.members...)
			}
			s.mu.Unlock()
			if !live {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// Hold the read open so an unserialised read-modify-write overlaps
			// observably instead of racing past by luck.
			time.Sleep(s.readDelay)
			out := make([]map[string]string, 0, len(members))
			for _, m := range members {
				out = append(out, map[string]string{"id": m, "type": "user"})
			}
			_ = json.NewEncoder(w).Encode(memberPage(out, len(out)))

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/groups/"):
			id := strings.TrimPrefix(r.URL.Path, "/groups/")
			s.mu.Lock()
			g, ok := s.byID[id]
			if !ok || !g.present {
				s.mu.Unlock()
				w.WriteHeader(http.StatusNotFound)
				return
			}
			g.present = false
			s.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/groups":
			var req struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				OuID        string `json:"ouId"`
				Members     []struct {
					ID string `json:"id"`
				} `json:"members"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("undecodable create body %q: %v", body, err)
			}
			s.mu.Lock()
			g, ok := s.byName[req.Name]
			if !ok {
				g = &storedGroup{name: req.Name}
				s.byName[req.Name] = g
			}
			if g.present {
				s.mu.Unlock()
				w.WriteHeader(http.StatusConflict) // GRP-1004, unique name per OU
				return
			}
			s.next++
			g.id = fmt.Sprintf("g-%d", s.next)
			g.desc, g.ou, g.present = req.Description, req.OuID, true
			g.members = nil
			for _, m := range req.Members {
				g.members = append(g.members, m.ID)
			}
			s.byID[g.id] = g
			out := Group{ID: g.id, Name: g.name, Description: g.desc, OUID: g.ou}
			s.mu.Unlock()
			_ = json.NewEncoder(w).Encode(out)

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func TestAddGroupMembers_ConcurrentAddsDoNotLoseAMember(t *testing.T) {
	// This is the test finding #1 was missing. Serialising only the WRITE is not
	// enough: N goroutines can each read {u-seed}, then take the write lock in
	// turn and each recreate the group with {u-seed, u-N} — every add but the
	// last is lost. The read has to be inside the same lock hold as the write,
	// so moving it out must turn this red.
	const goroutines = 6
	store := newGroupStore("admin", "g-0", []string{"u-seed"}, 2*time.Millisecond)
	stub := &directoryStub{handle: store.handle(t)}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	errs := make([]error, goroutines)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			group := Group{ID: "g-0", Name: "admin", Description: "the admins", OUID: "ou-1"}
			_, errs[i] = c.AddGroupMembers(context.Background(), group, []string{fmt.Sprintf("u-%d", i)})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	want := []string{"u-seed"}
	for i := 0; i < goroutines; i++ {
		want = append(want, fmt.Sprintf("u-%d", i))
	}
	got := sortedCopy(store.membersOf("admin"))
	if strings.Join(got, ",") != strings.Join(sortedCopy(want), ",") {
		t.Errorf("final membership = %v, want %v — a concurrent add was lost", got, sortedCopy(want))
	}
}

func TestCreateGroup_SerialisesWritesToOneGroupName(t *testing.T) {
	// Group membership has no atomic add, so two goroutines creating the same
	// group name concurrently would race. lockGroup keys on the lowercased
	// name, so a fan-out that spells the role differently must serialise too.
	tests := []struct {
		name  string
		names []string
	}{
		{"same name", []string{"admin", "admin", "admin", "admin", "admin", "admin", "admin", "admin"}},
		{"names differing only in case", []string{"admin", "Admin", "ADMIN", "aDmIn", "admin", "ADMIN", "Admin", "admin"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			probe := &concurrencyProbe{}
			stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if r.Method != http.MethodPost || r.URL.Path != "/groups" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				probe.enter()
				// Hold the request open long enough that an unserialised
				// fan-out overlaps observably, but short enough to stay cheap.
				time.Sleep(2 * time.Millisecond)
				probe.leave()
				_ = json.NewEncoder(w).Encode(Group{ID: "g-new", Name: "admin"})
			}}
			srv := stub.server(t)
			defer srv.Close()
			c := newTestClient(srv.URL)

			errs := make([]error, len(tc.names))
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i, name := range tc.names {
				wg.Add(1)
				go func(i int, name string) {
					defer wg.Done()
					<-start
					_, errs[i] = c.CreateGroup(context.Background(), name, "", []string{"u-1"})
				}(i, name)
			}
			close(start)
			wg.Wait()

			for i, err := range errs {
				if err != nil {
					t.Fatalf("goroutine %d: unexpected error: %v", i, err)
				}
			}
			if got := probe.max(); got != 1 {
				t.Errorf("%d concurrent create-group requests overlapped, want strictly serialised (1)", got)
			}
			var posts int
			for _, call := range stub.snapshot() {
				if call.Method == http.MethodPost {
					posts++
				}
			}
			if posts != len(tc.names) {
				t.Errorf("issued %d POSTs, want %d — every caller must get its request through", posts, len(tc.names))
			}
		})
	}
}

// concurrencyProbe records the peak number of overlapping requests.
type concurrencyProbe struct {
	mu       sync.Mutex
	inFlight int
	peak     int
}

func (p *concurrencyProbe) enter() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inFlight++
	if p.inFlight > p.peak {
		p.peak = p.inFlight
	}
}

func (p *concurrencyProbe) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inFlight--
}

func (p *concurrencyProbe) max() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peak
}

// -- idempotent deletes -------------------------------------------------------

func TestDeleteIsIdempotent(t *testing.T) {
	// "Already gone" is the state the caller asked for, so a 404 is success —
	// a retried console delete, or a re-run after a partial failure, must not
	// surface a failure the user cannot act on. Every other non-2xx still does.
	tests := []struct {
		name     string
		status   int
		wantErr  bool
		wantCode string
	}{
		{"deleted", http.StatusNoContent, false, ""},
		{"already gone", http.StatusNotFound, false, ""},
		{"server failure still errors", http.StatusInternalServerError, true, "500"},
		{"forbidden still errors", http.StatusForbidden, true, "403"},
	}
	targets := []struct {
		kind string
		path string
		call func(c *client, ctx context.Context) error
	}{
		{"user", "/users/x-1", func(c *client, ctx context.Context) error {
			return c.DeleteUser(ctx, "x-1")
		}},
	}
	for _, target := range targets {
		for _, tc := range tests {
			t.Run(target.kind+"/"+tc.name, func(t *testing.T) {
				stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
					if r.Method != http.MethodDelete || r.URL.Path != target.path {
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					}
					w.WriteHeader(tc.status)
				}}
				srv := stub.server(t)
				defer srv.Close()

				err := target.call(newTestClient(srv.URL), context.Background())
				if tc.wantErr {
					if err == nil {
						t.Fatalf("status %d must surface as an error", tc.status)
					}
					if !strings.Contains(err.Error(), tc.wantCode) {
						t.Errorf("error should name the status, got %q", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("status %d must be success, got %v", tc.status, err)
				}
			})
		}
	}
}

func TestDeleteUser_EscapesTheUserIDInThePath(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).DeleteUser(context.Background(), "a/b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := stub.snapshot()
	if len(got) != 1 {
		t.Fatalf("issued %d requests, want 1", len(got))
	}
	if got[0].RawPath != "/users/a%2Fb" {
		t.Errorf("wire path = %q, want /users/a%%2Fb", got[0].RawPath)
	}
}

// -- FindUserByUsername -------------------------------------------------------

func TestFindUserByUsername(t *testing.T) {
	// Thunder has no server-side filter, so this is a bounded client-side scan.
	// Unlike group names the username match is EXACT: usernames are credentials,
	// and folding case here would hand one account's login to another.
	page0 := make([]map[string]any, 0, directoryPageSize)
	for i := 0; i < directoryPageSize; i++ {
		page0 = append(page0, map[string]any{
			"id":         fmt.Sprintf("u-%d", i),
			"attributes": map[string]string{"username": fmt.Sprintf("filler%d", i)},
		})
	}
	page1 := []map[string]any{{
		"id":   "u-alice",
		"ouId": "ou-default",
		"type": "Person",
		"attributes": map[string]string{
			"username": "alice", "email": "alice@example.com",
		},
	}}
	const total = directoryPageSize + 1

	tests := []struct {
		name        string
		query       string
		wantFound   bool
		wantOffsets []int
	}{
		{"found on the first page", "filler7", true, []int{0}},
		{"found on a later page", "alice", true, []int{0, directoryPageSize}},
		{"absent", "nobody", false, []int{0, directoryPageSize}},
		{"case differs, so no match", "Alice", false, []int{0, directoryPageSize}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if r.URL.Path != "/users" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				page := page1
				if intQuery(t, r, "offset") == 0 {
					page = page0
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": total, "users": page})
			}}
			srv := stub.server(t)
			defer srv.Close()

			got, found, err := newTestClient(srv.URL).FindUserByUsername(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v (user %+v)", found, tc.wantFound, got)
			}
			if !tc.wantFound && got != nil {
				t.Errorf("want a nil user when absent, got %+v", got)
			}
			if tc.query == "alice" {
				if got.ID != "u-alice" || got.Username != "alice" || got.Email != "alice@example.com" {
					t.Errorf("got %+v, want the attributes mapped onto DirectoryUser", *got)
				}
			}
			wantPagingQueries(t, stub.snapshot(), tc.wantOffsets...)
		})
	}
}

func TestFindUserByUsername_ServerCappedPageSize_ScansEverything(t *testing.T) {
	// And on the user scan. There is no server-side filter to fall back on, so a
	// truncated drain answers "no such user" for an account that exists — and
	// the ensure then tries to create it, 409 on the unique username.
	const serverLimit = 3
	all := make([]map[string]any, 0, 7)
	for i := 0; i < 7; i++ {
		all = append(all, map[string]any{
			"id":         fmt.Sprintf("u-%d", i),
			"attributes": map[string]string{"username": fmt.Sprintf("user%d", i)},
		})
	}
	tests := []struct {
		name      string
		query     string
		wantFound bool
	}{
		{"the last record is still reachable", "user6", true},
		{"absent only after the whole scan", "nobody", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
				lo, hi := pageWindow(len(all), intQuery(t, r, "offset"), min(intQuery(t, r, "limit"), serverLimit))
				_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": len(all), "users": all[lo:hi]})
			}}
			srv := stub.server(t)
			defer srv.Close()

			got, found, err := newTestClient(srv.URL).FindUserByUsername(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v — the scan stopped early", found, tc.wantFound)
			}
			if tc.wantFound && got.ID != "u-6" {
				t.Errorf("got %+v, want the last record", *got)
			}
			wantPagingQueries(t, stub.snapshot(), 0, 3, 6)
		})
	}
}

// -- CreateUser ---------------------------------------------------------------

func TestCreateUser_PostsAPersonUnderTheDefaultOU(t *testing.T) {
	stub := &directoryStub{ouID: "ou-default", handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodPost || r.URL.Path != "/users" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Thunder echoes the submitted password back on success too; the
		// mapping must not carry it anywhere.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "u-1", "ouId": "ou-default", "type": "Person",
			"attributes": map[string]string{
				"username": "alice", "email": "alice@example.com", "password": "pw",
			},
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).CreateUser(context.Background(), "alice", "alice@example.com", "pw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSeq(t, stub.seq(), "GET /organization-units/tree/default", "POST /users")

	var body struct {
		OuID       string            `json:"ouId"`
		Type       string            `json:"type"`
		Attributes map[string]string `json:"attributes"`
	}
	decodeCallBody(t, stub.only(t, http.MethodPost, "/users"), &body)
	if body.OuID != "ou-default" {
		t.Errorf("ouId = %q, want the default OU", body.OuID)
	}
	if body.Type != "Person" {
		t.Errorf("type = %q, want Person", body.Type)
	}
	want := map[string]string{"username": "alice", "email": "alice@example.com", "password": "pw"}
	for k, v := range want {
		if body.Attributes[k] != v {
			t.Errorf("attributes[%s] = %q, want %q", k, body.Attributes[k], v)
		}
	}
	if len(body.Attributes) != len(want) {
		t.Errorf("attributes = %v, want exactly %v (Thunder 400s on unknown keys)", body.Attributes, want)
	}
	if got != (DirectoryUser{ID: "u-1", Username: "alice", Email: "alice@example.com"}) {
		t.Errorf("returned %+v, want the created account", got)
	}
}

// -- SetUserPassword ----------------------------------------------------------

func TestSetUserPassword_ReadsThenSendsEveryExistingAttribute(t *testing.T) {
	// `PUT /users/{id}` is a FULL REPLACE of the attributes map. Sending only
	// the password erases the account's email and username, which is a silent
	// data loss — hence the read-first, and hence this assertion on every
	// attribute the read returned.
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users/u-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "u-1", "ouId": "ou-7", "type": "Person",
				"attributes": map[string]string{
					"username": "alice", "email": "alice@example.com", "firstName": "Alice",
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/users/u-1":
			// Thunder echoes the password here; the client must not read it.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attributes": map[string]string{"password": "new-pw"},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).SetUserPassword(context.Background(), "u-1", "new-pw"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSeq(t, stub.seq(), "GET /users/u-1", "PUT /users/u-1")

	var body struct {
		OuID       string            `json:"ouId"`
		Type       string            `json:"type"`
		Attributes map[string]string `json:"attributes"`
	}
	decodeCallBody(t, stub.only(t, http.MethodPut, "/users/u-1"), &body)
	want := map[string]string{
		"username": "alice", "email": "alice@example.com", "firstName": "Alice", "password": "new-pw",
	}
	if len(body.Attributes) != len(want) {
		t.Fatalf("PUT attributes = %v, want %v", body.Attributes, want)
	}
	for k, v := range want {
		if body.Attributes[k] != v {
			t.Errorf("attributes[%s] = %q, want %q", k, body.Attributes[k], v)
		}
	}
	if body.OuID != "ou-7" || body.Type != "Person" {
		t.Errorf("PUT ouId/type = %q/%q, want the values read back", body.OuID, body.Type)
	}
}

func TestSetUserPassword_DoesNotPutWhenTheReadFails(t *testing.T) {
	// Without the read there is nothing to merge, so a blind PUT would wipe the
	// account's attributes.
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodGet {
			t.Errorf("must not write after a failed read, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).SetUserPassword(context.Background(), "u-1", "new-pw"); err == nil {
		t.Fatal("want an error when the user read fails, got nil")
	}
	wantSeq(t, stub.seq(), "GET /users/u-1")
}

func TestSetUserPassword_EscapesTheUserIDInThePath(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "a/b", "type": "Person"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).SetUserPassword(context.Background(), "a/b", "pw"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range stub.snapshot() {
		if c.RawPath != "/users/a%2Fb" {
			t.Errorf("%s wire path = %q, want /users/a%%2Fb", c.Method, c.RawPath)
		}
	}
}

// -- password never reaches an error string -----------------------------------
//
// Thunder's user endpoints echo the submitted password back in plaintext, in
// the SUCCESS body and in the failure body alike. The rest of this client
// reports a Thunder failure by interpolating the response body into the error,
// and those errors travel into build output that a project's members read. The
// three tests below are the regression guard on doJSONNoEcho: if any of these
// calls goes back through plain doJSON, a generated test-user password is
// published. The GET is included because the attributes map it reads is the one
// map in this API known to be able to carry a password.

func TestSetUserPassword_ErrorNeverContainsThePassword(t *testing.T) {
	const password = "Zx9-generated-secret-pw"
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "u-1", "type": "Person",
				"attributes": map[string]string{"username": "alice"},
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		// Verbatim shape of a Thunder rejection: it hands the password back.
		_, _ = w.Write([]byte(`{"code":"USR-1019","attributes":{"password":"` + password + `"}}`))
	}}
	srv := stub.server(t)
	defer srv.Close()

	err := newTestClient(srv.URL).SetUserPassword(context.Background(), "u-1", password)
	if err == nil {
		t.Fatal("want an error on a 400 PUT, got nil")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("password leaked into the error string: %q", err)
	}
	// Still diagnosable: the error must name the request and the status.
	if !strings.Contains(err.Error(), "PUT") || !strings.Contains(err.Error(), "400") {
		t.Errorf("error should identify the failed request, got %q", err)
	}
}

func TestSetUserPassword_ReadErrorNeverContainsThePassword(t *testing.T) {
	const password = "Rt2-generated-secret-pw"
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"USR-5000","attributes":{"password":"` + password + `"}}`))
	}}
	srv := stub.server(t)
	defer srv.Close()

	err := newTestClient(srv.URL).SetUserPassword(context.Background(), "u-1", password)
	if err == nil {
		t.Fatal("want an error on a 500 GET, got nil")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("password leaked into the error string: %q", err)
	}
	if !strings.Contains(err.Error(), "GET") || !strings.Contains(err.Error(), "500") {
		t.Errorf("error should identify the failed request, got %q", err)
	}
}

func TestCreateUser_ErrorNeverContainsThePassword(t *testing.T) {
	const password = "Qw4-generated-secret-pw"
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"USR-1003","attributes":{"password":"` + password + `"}}`))
	}}
	srv := stub.server(t)
	defer srv.Close()

	_, err := newTestClient(srv.URL).CreateUser(context.Background(), "alice", "alice@example.com", password)
	if err == nil {
		t.Fatal("want an error on a 400 POST, got nil")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("password leaked into the error string: %q", err)
	}
	if !strings.Contains(err.Error(), "POST") || !strings.Contains(err.Error(), "400") {
		t.Errorf("error should identify the failed request, got %q", err)
	}
}

// -- redactPath ---------------------------------------------------------------

func TestRedactPath(t *testing.T) {
	// A directory id is not a secret, but it is an identifier, and errors from
	// this path reach build output. The query is ALWAYS kept: it is the paging
	// window, it is diagnostic, and it carries nothing sensitive.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"uuid segment", "/users/3f2504e0-4f89-11d3-9a0c-0305e82c3301", "/users/{id}"},
		{"uuid mid-path keeps the suffix", "/groups/3f2504e0-4f89-11d3-9a0c-0305e82c3301/members", "/groups/{id}/members"},
		{
			"uuid and query together",
			"/groups/3f2504e0-4f89-11d3-9a0c-0305e82c3301/members?limit=100&offset=0",
			"/groups/{id}/members?limit=100&offset=0",
		},
		{"short segment survives", "/users/u-1", "/users/u-1"},
		{"escaped segment survives", "/users/a%2Fb", "/users/a%2Fb"},
		{"collection with paging", "/groups?limit=100&offset=200", "/groups?limit=100&offset=200"},
		// An all-numeric query used to be dropped by a branch whose comment said
		// it was keeping it. Nothing here produces one, which is exactly why the
		// contradiction went unnoticed; pin the query as always kept.
		{"all-numeric query is kept", "/groups?100", "/groups?100"},
		{"no id anywhere", "/organization-units/tree/default", "/organization-units/tree/default"},
		{
			"two uuids",
			"/groups/3f2504e0-4f89-11d3-9a0c-0305e82c3301/members/0f8fad5b-d9cb-469f-a165-70867728950e",
			"/groups/{id}/members/{id}",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactPath(tc.in); got != tc.want {
				t.Errorf("redactPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Thunder's attribute values are NOT all strings — the default `admin` account
// ships with `email_verified: true` and `phone_number_verified: true`. A
// string-typed attributes map fails to decode the WHOLE listing the moment one
// such account exists, which is every deployment, so FindUserByUsername would
// error on every build. This was found by running the ensure against a live
// Thunder; every fixture here had used string-only attributes and missed it.
func TestFindUserByUsername_ToleratesNonStringAttributes(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path != "/users" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		// Shaped exactly like the real default admin account.
		_, _ = w.Write([]byte(`{"totalResults":2,"count":2,"users":[
			{"id":"u-admin","ouId":"ou-1","type":"Person","attributes":{
				"username":"admin","email":"admin@thunder.dev","email_verified":true,
				"phone_number_verified":true,"name":"Administrator","sub":"admin"}},
			{"id":"u-2","ouId":"ou-1","type":"Person","attributes":{
				"username":"test-viewer","email":"test-viewer@test-users.invalid"}}
		]}`))
	}}
	srv := stub.server(t)
	defer srv.Close()

	// The account we want sits AFTER the one with boolean attributes, so a decode
	// failure on the first record hides the second entirely.
	got, found, err := newTestClient(srv.URL).FindUserByUsername(context.Background(), "test-viewer")
	if err != nil {
		t.Fatalf("FindUserByUsername: %v", err)
	}
	if !found || got.Username != "test-viewer" || got.ID != "u-2" {
		t.Fatalf("got %+v found=%v, want the test-viewer account", got, found)
	}

	// And the boolean-bearing account is still readable in its own right.
	admin, found, err := newTestClient(srv.URL).FindUserByUsername(context.Background(), "admin")
	if err != nil || !found || admin.Email != "admin@thunder.dev" {
		t.Fatalf("admin = %+v found=%v err=%v", admin, found, err)
	}
}

// PUT is a full replace, so a non-string attribute has to survive the
// read-merge-write as its own type rather than being coerced or dropped.
func TestSetUserPassword_PreservesNonStringAttributes(t *testing.T) {
	var put map[string]any
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, body []byte) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"u-1","ouId":"ou-1","type":"Person","attributes":{
				"username":"test-viewer","email":"v@test-users.invalid","email_verified":true}}`))
		case http.MethodPut:
			if err := json.Unmarshal(body, &put); err != nil {
				t.Errorf("decode PUT body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).SetUserPassword(context.Background(), "u-1", "Aep1!new"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	attrs, ok := put["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("PUT carried no attributes map: %+v", put)
	}
	if attrs["email_verified"] != true {
		t.Errorf("email_verified = %#v, want the boolean true to survive verbatim", attrs["email_verified"])
	}
	if attrs["username"] != "test-viewer" || attrs["email"] != "v@test-users.invalid" {
		t.Errorf("string attributes lost: %+v", attrs)
	}
	if attrs["password"] != "Aep1!new" {
		t.Errorf("password not set: %+v", attrs)
	}
}
