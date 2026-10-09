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

// bindingListPayload is one OC ReleaseBindingList page. A document rather than
// the typed model for the same reason componentListPayload is: the fixture's
// POINT is metadata.labels, which ReleaseBindingSummary drops.
func bindingListPayload(items ...map[string]any) map[string]any {
	return map[string]any{"items": items, "pagination": map[string]any{}}
}

// readyBinding is a binding whose Ready condition is True — the only state that
// folds to "deployed", and the state an agent Job's binding sits in
// unconditionally.
func readyBinding(project, component string, labels map[string]any) map[string]any {
	meta := map[string]any{"name": ReleaseBindingName(project, component, "development")}
	if labels != nil {
		meta["labels"] = labels
	}
	return map[string]any{
		"metadata": meta,
		"spec": map[string]any{
			"environment": "development",
			"owner": map[string]any{
				"projectName":   project,
				"componentName": ScopedComponentName(project, component),
			},
		},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":               "Ready",
					"status":             "True",
					"reason":             "ResourcesReady",
					"lastTransitionTime": "2026-08-01T10:00:00Z",
				},
			},
		},
	}
}

func serveBindings(t *testing.T, payload map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/releasebindings") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, payload)
	}))
}

// A coding-agent binding is owned by the USER'S project and lives in the dev
// environment, so project ownership alone cannot tell it apart from a real
// deployment. It also reports Ready=True whatever its Job is doing, because
// OpenChoreo registers no health check for batch/v1 Job. Counting it reported a
// project "deployed" before a single component had been deployed.
func TestListProjectReleaseBindings_ExcludesMarkedAgentBindings(t *testing.T) {
	const project = "widgets"
	srv := serveBindings(t, bindingListPayload(
		readyBinding(project, "order-service", nil),
		readyBinding(project, "ca-run-7", map[string]any{
			string(LabelKeyAepInternal): LabelValueAepInternal,
			string(LabelKeyAepCycle):    "cyc-7",
		}),
		readyBinding("other-project", "billing", nil),
	))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListProjectReleaseBindings(context.Background(), "org", project)
	if err != nil {
		t.Fatalf("ListProjectReleaseBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 binding, got %d: %+v", len(got), got)
	}
	if got[0].ComponentName != "order-service" {
		t.Errorf("unexpected component: %q", got[0].ComponentName)
	}
	if got[0].ReadyStatus != "True" {
		t.Errorf("user binding lost its Ready condition: %q", got[0].ReadyStatus)
	}
}

// The honest "nothing deployed" state has to be REACHABLE. Before the marker,
// one finished coding cycle left a permanently-Ready binding behind and the
// deploy stage could never report `none` again for the life of the project.
func TestListProjectReleaseBindings_AgentOnlyProjectIsEmpty(t *testing.T) {
	const project = "widgets"
	srv := serveBindings(t, bindingListPayload(
		readyBinding(project, "ca-run-1", map[string]any{
			string(LabelKeyAepInternal): LabelValueAepInternal,
		}),
	))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListProjectReleaseBindings(context.Background(), "org", project)
	if err != nil {
		t.Fatalf("ListProjectReleaseBindings: %v", err)
	}
	// Empty is what deployStageStatus folds to deployNone — "Nothing deployed".
	if len(got) != 0 {
		t.Fatalf("want no bindings, got %d: %+v", len(got), got)
	}
}

// Bindings created before the marker existed carry no label, so they are still
// counted. Deliberate, not an oversight: they go when the settler deletes them,
// and the alternative (matching the agent's run-name convention) would infer
// identity from a naming scheme. Pinned so the limitation is a decision on
// record rather than a surprise in the field.
func TestListProjectReleaseBindings_UnmarkedAgentBindingStillCounts(t *testing.T) {
	const project = "widgets"
	srv := serveBindings(t, bindingListPayload(
		readyBinding(project, "ca-run-legacy", nil),
	))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListProjectReleaseBindings(context.Background(), "org", project)
	if err != nil {
		t.Fatalf("ListProjectReleaseBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the unmarked binding returned, got %d: %+v", len(got), got)
	}
}

// A marker on a binding whose value is not "true" is not a marker.
func TestListProjectReleaseBindings_NonTrueMarkerDoesNotFilter(t *testing.T) {
	const project = "widgets"
	srv := serveBindings(t, bindingListPayload(
		readyBinding(project, "order-service", map[string]any{
			string(LabelKeyAepInternal): "false",
		}),
	))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListProjectReleaseBindings(context.Background(), "org", project)
	if err != nil {
		t.Fatalf("ListProjectReleaseBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 binding, got %d: %+v", len(got), got)
	}
}
