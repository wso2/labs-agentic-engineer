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

package openchoreo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ListBuildRuns is the read behind "which release should this component be
// serving": the caller picks the newest SUCCEEDED run and derives the release
// name from its commit. That makes PAGINATION a correctness property rather
// than a nicety — the list is paged with Kubernetes continuation tokens, so a
// newer green run can sit on a later page, and a reader that stops at the first
// page names an older release as the desired one and promotes the component
// backwards onto it.

// buildRun is one WorkflowRun document. Authored as a document rather than the
// typed model because the fixture's POINT is where the commit lives —
// `spec.workflow.parameters.repository.revision.commit`, the exact path the
// trigger writes — which no typed field on the summary carries.
func buildRun(name, commit, reason string, created string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"name":              name,
			"creationTimestamp": created,
			"labels":            map[string]any{string(LabelKeyComponent): "widgets-order-service"},
		},
		"spec": map[string]any{
			"workflow": map[string]any{
				"name": "dockerfile-builder",
				"parameters": map[string]any{
					"repository": map[string]any{
						"revision": map[string]any{"commit": commit},
					},
				},
			},
		},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":               WorkflowConditionCompleted,
					"status":             "True",
					"reason":             reason,
					"lastTransitionTime": created,
				},
			},
		},
	}
}

// serveBuildRunPages answers each successive workflowruns request with the next
// page, handing out a cursor until the pages run out. It also records the
// cursor it was given, so a test can assert the client actually followed it
// rather than re-requesting the first page.
func serveBuildRunPages(t *testing.T, pages ...[]map[string]any) (*httptest.Server, *[]string) {
	t.Helper()
	cursors := make([]string, 0, len(pages))
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/workflowruns") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		cursors = append(cursors, r.URL.Query().Get("cursor"))
		if page >= len(pages) {
			t.Fatalf("client asked for page %d of %d", page+1, len(pages))
		}
		body := map[string]any{"items": pages[page], "pagination": map[string]any{}}
		if page < len(pages)-1 {
			body["pagination"] = map[string]any{"nextCursor": "cursor-" + string(rune('a'+page))}
		}
		page++
		writeJSON(t, w, http.StatusOK, body)
	}))
	return srv, &cursors
}

// The failure this closes: the newest green build is on page two, so a
// single-page read would call the OLDER release the desired one — and the
// deploy stage would promote the component backwards onto it.
func TestListBuildRuns_FollowsPaginationToTheNewestRun(t *testing.T) {
	srv, cursors := serveBuildRunPages(t,
		[]map[string]any{
			buildRun("widgets-order-service-aaa1-1", "aaa1", ReasonWorkflowSucceeded, "2026-09-01T10:00:00Z"),
		},
		[]map[string]any{
			buildRun("widgets-order-service-bbb2-1", "bbb2", ReasonWorkflowSucceeded, "2026-09-01T11:00:00Z"),
			buildRun("widgets-order-service-ccc3-1", "ccc3", "WorkflowFailed", "2026-09-01T12:00:00Z"),
		},
	)
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListBuildRuns(context.Background(), "org", "widgets", "order-service")
	if err != nil {
		t.Fatalf("ListBuildRuns: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want every run across both pages, got %d: %+v", len(got), got)
	}
	if len(*cursors) != 2 || (*cursors)[0] != "" || (*cursors)[1] == "" {
		t.Fatalf("the second request did not carry the first page's cursor: %v", *cursors)
	}
	// The commit is read from the run's own spec, and the terminal verdict from
	// its Completed condition's reason — both of which decide which release the
	// caller names as desired.
	newest := got[1]
	if newest.CommitSHA != "bbb2" || !newest.Succeeded || !newest.Completed {
		t.Errorf("page-two run lost its facts: %+v", newest)
	}
	if got[2].Succeeded {
		t.Errorf("a failed run must not read as succeeded: %+v", got[2])
	}
	if newest.StartedAt.IsZero() {
		t.Error("StartedAt is empty, so two green builds cannot be ordered")
	}
}

// A cursor that does not advance would spin the loop forever. Failing is the
// only safe answer: this read is on the path of every deploy decision, and a
// hung activity is indistinguishable from a slow one.
func TestListBuildRuns_RefusesANonAdvancingCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items":      []any{buildRun("widgets-order-service-aaa1-1", "aaa1", ReasonWorkflowSucceeded, "2026-09-01T10:00:00Z")},
			"pagination": map[string]any{"nextCursor": "stuck"},
		})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	_, err := c.ListBuildRuns(context.Background(), "org", "widgets", "order-service")
	if err == nil {
		t.Fatal("a cursor that repeats itself was accepted; the loop would never end")
	}
	if !strings.Contains(err.Error(), "pagination did not advance") {
		t.Errorf("the error does not name the cause: %v", err)
	}
}

// A build triggered without a pinned commit (a manual build of the branch tip)
// names no release the platform could pin, and the caller skips it. It must not
// arrive as a parse error or as somebody else's commit.
func TestListBuildRuns_AnUnpinnedBuildHasNoCommit(t *testing.T) {
	run := buildRun("widgets-order-service-manual-1", "", ReasonWorkflowSucceeded, "2026-09-01T10:00:00Z")
	spec := run["spec"].(map[string]any)
	spec["workflow"] = map[string]any{"name": "dockerfile-builder"}
	srv, _ := serveBuildRunPages(t, []map[string]any{run})
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListBuildRuns(context.Background(), "org", "widgets", "order-service")
	if err != nil {
		t.Fatalf("ListBuildRuns: %v", err)
	}
	if len(got) != 1 || got[0].CommitSHA != "" {
		t.Fatalf("want one run with no commit, got %+v", got)
	}
}
