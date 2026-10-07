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

package listreports

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// fakeRepo records the (org, cursor, limit) the slice actually passed through,
// so the tests can pin the default/cap logic that is this slice's whole job.
type fakeRepo struct {
	gotOrg    string
	gotCursor string
	gotLimit  int
	reports   []ops.RcaAgentReport
	next      string
}

func (f *fakeRepo) Get(context.Context, string, string) (*ops.RcaAgentReport, error) { return nil, nil }
func (f *fakeRepo) List(_ context.Context, orgID, cursor string, limit int) ([]ops.RcaAgentReport, string, error) {
	f.gotOrg, f.gotCursor, f.gotLimit = orgID, cursor, limit
	return f.reports, f.next, nil
}

func ctxWithOrg(org string) context.Context {
	return tenant.WithBoundOrg(context.Background(), org)
}

func list(t *testing.T, h *Handler, params gen.ListRcaAgentReportsParams) gen.RcaAgentReportList {
	t.Helper()
	resp, err := h.ListRcaAgentReports(ctxWithOrg("acme"), gen.ListRcaAgentReportsRequestObject{Params: params})
	if err != nil {
		t.Fatalf("ListRcaAgentReports: %v", err)
	}
	out, ok := resp.(gen.ListRcaAgentReports200JSONResponse)
	if !ok {
		t.Fatalf("response = %T, want 200", resp)
	}
	return gen.RcaAgentReportList(out)
}

func TestListReports_LimitDefaultsAndCaps(t *testing.T) {
	cases := []struct {
		name     string
		in, want int
	}{
		{"absent/zero defaults to 50", 0, defaultListLimit},
		{"negative defaults to 50", -5, defaultListLimit},
		{"excessive is capped at 200", 5000, maxListLimit},
		{"reasonable passes through", 10, 10},
		{"exactly the cap passes through", maxListLimit, maxListLimit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeRepo{}
			list(t, New(repo), gen.ListRcaAgentReportsParams{Limit: c.in})
			if repo.gotLimit != c.want {
				t.Errorf("limit %d reached the repository as %d, want %d", c.in, repo.gotLimit, c.want)
			}
		})
	}
}

func TestListReports_PassesOrgAndCursor(t *testing.T) {
	repo := &fakeRepo{}
	list(t, New(repo), gen.ListRcaAgentReportsParams{Cursor: "abc"})

	// org comes from the gate-bound context, never from a request param.
	if repo.gotOrg != "acme" {
		t.Errorf("repository saw org %q, want the gate-bound %q", repo.gotOrg, "acme")
	}
	if repo.gotCursor != "abc" {
		t.Errorf("cursor = %q, want %q", repo.gotCursor, "abc")
	}
}

func TestListReports_ProjectsPageAndCursor(t *testing.T) {
	repo := &fakeRepo{
		reports: []ops.RcaAgentReport{{ID: "r1", Project: "p"}, {ID: "r2", Project: "p"}},
		next:    "next-cursor",
	}
	got := list(t, New(repo), gen.ListRcaAgentReportsParams{})

	if len(got.Items) != 2 || got.Items[0].ID != "r1" {
		t.Fatalf("items = %+v, want the repository's page projected in order", got.Items)
	}
	if got.NextCursor != "next-cursor" {
		t.Errorf("nextCursor = %q, want it passed through", got.NextCursor)
	}
}

// TestListReports_EmptyPageStaysNull pins the WIRE shape of an empty page.
//
// gorm's Find leaves an empty result nil, so an empty page has always
// marshalled to `"items":null` and the contract marks items nullable to say so.
// The P1 refactor introduced a mapping step between the domain and the wire —
// exactly where a well-meaning `make([]T, 0)` would silently turn null into [].
// That is a behaviour change, so it is pinned here rather than left to chance.
func TestListReports_EmptyPageStaysNull(t *testing.T) {
	got := list(t, New(&fakeRepo{reports: nil}), gen.ListRcaAgentReportsParams{})

	if got.Items != nil {
		t.Errorf("Items = %#v, want nil (an empty page has always been `\"items\":null`)", got.Items)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(body) != `{"items":null}` {
		t.Errorf("empty page marshals to %s, want {\"items\":null}", body)
	}
}
