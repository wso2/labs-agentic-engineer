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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestRuntimeClient(t *testing.T, srv *httptest.Server) RuntimeClient {
	t.Helper()
	return NewRuntimeClient(Config{BaseURL: srv.URL})
}

func TestReleaseBindingName_PicksTheEnvironmentsBinding(t *testing.T) {
	var gotComponentQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotComponentQuery = r.URL.Query().Get("component")
		writeJSON(t, w, http.StatusOK, map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"metadata": map[string]interface{}{"name": "rb-staging"},
					"spec": map[string]interface{}{
						"environment": "staging",
						"owner":       map[string]interface{}{"projectName": "shop", "componentName": "shop-ca-abc"},
						"releaseName": "rel-1",
					},
				},
				map[string]interface{}{
					"metadata": map[string]interface{}{"name": "rb-dev"},
					"spec": map[string]interface{}{
						"environment": "development",
						"owner":       map[string]interface{}{"projectName": "shop", "componentName": "shop-ca-abc"},
						"releaseName": "rel-2",
					},
				},
			},
			"pagination": map[string]interface{}{},
		})
	}))
	defer srv.Close()

	got, err := newTestRuntimeClient(t, srv).
		ReleaseBindingName(context.Background(), "acme", "shop", "ca-abc", DevEnvironmentName)
	if err != nil {
		t.Fatalf("ReleaseBindingName: %v", err)
	}
	if got != "rb-dev" {
		t.Fatalf("ReleaseBindingName = %q, want rb-dev", got)
	}
	if gotComponentQuery != ScopedComponentName("shop", "ca-abc") {
		t.Fatalf("component query = %q, want the scoped component name", gotComponentQuery)
	}
}

// A cycle whose Component has been deleted (retention, or a cancel) has no
// binding. That is ErrNotFound and not a transport failure: it is exactly the
// signal the watcher counts toward its sustained-404 rule and the progress
// reader turns into "logs unavailable".
func TestReleaseBindingName_NoBindingIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]interface{}{
			"items":      []interface{}{},
			"pagination": map[string]interface{}{},
		})
	}))
	defer srv.Close()

	_, err := newTestRuntimeClient(t, srv).
		ReleaseBindingName(context.Background(), "acme", "shop", "ca-abc", DevEnvironmentName)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPodSnapshot_FindsTheJobsPod(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, http.StatusOK, map[string]interface{}{
			"renderedReleases": []interface{}{
				map[string]interface{}{
					"name":        "rel-2",
					"targetPlane": "dataplane",
					"nodes": []interface{}{
						map[string]interface{}{
							"kind": "Job", "name": "ca-abc", "object": map[string]interface{}{},
						},
						map[string]interface{}{
							"kind": "Pod", "name": "ca-abc-9x2",
							"createdAt": "2026-08-06T10:00:00Z",
							"object": map[string]interface{}{
								"status": map[string]interface{}{"phase": "Running"},
							},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	pod, err := newTestRuntimeClient(t, srv).PodSnapshot(context.Background(), "acme", "rb-dev")
	if err != nil {
		t.Fatalf("PodSnapshot: %v", err)
	}
	if !pod.Found || pod.Name != "ca-abc-9x2" || pod.Phase != "Running" {
		t.Fatalf("unexpected pod: %+v", pod)
	}
	if gotPath != "/api/v1/namespaces/acme/releasebindings/rb-dev/k8sresources/tree" {
		t.Fatalf("unexpected path %q", gotPath)
	}
}

// A tree with no Pod node is the normal pre-scheduling state, not an error: the
// watcher's startup grace, not an exception, is what decides it has waited long
// enough.
func TestPodSnapshot_NoPodNodeIsNotFoundPodNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]interface{}{
			"renderedReleases": []interface{}{
				map[string]interface{}{
					"name": "rel-2", "targetPlane": "dataplane",
					"nodes": []interface{}{
						map[string]interface{}{"kind": "Job", "name": "ca-abc", "object": map[string]interface{}{}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	pod, err := newTestRuntimeClient(t, srv).PodSnapshot(context.Background(), "acme", "rb-dev")
	if err != nil {
		t.Fatalf("PodSnapshot: %v", err)
	}
	if pod.Found {
		t.Fatalf("a tree with no Pod node must report Found=false, got %+v", pod)
	}
}

func TestPodSnapshot_BindingGoneIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]interface{}{"error": "release binding not found"})
	}))
	defer srv.Close()

	_, err := newTestRuntimeClient(t, srv).PodSnapshot(context.Background(), "acme", "rb-dev")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPodLogs_ReadsLinesForThePod(t *testing.T) {
	var gotPod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPod = r.URL.Query().Get("podName")
		writeJSON(t, w, http.StatusOK, map[string]interface{}{
			"logEntries": []interface{}{
				map[string]interface{}{"timestamp": "2026-08-06T10:00:01Z", "log": "hello"},
				map[string]interface{}{"timestamp": "2026-08-06T10:00:02Z", "log": "world"},
			},
		})
	}))
	defer srv.Close()

	lines, err := newTestRuntimeClient(t, srv).PodLogs(context.Background(), "acme", "rb-dev", "ca-abc-9x2", 0)
	if err != nil {
		t.Fatalf("PodLogs: %v", err)
	}
	if len(lines) != 2 || lines[1].Log != "world" {
		t.Fatalf("unexpected lines: %+v", lines)
	}
	if !lines[0].Timestamp.Equal(time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC)) {
		t.Fatalf("unexpected timestamp %v", lines[0].Timestamp)
	}
	if gotPod != "ca-abc-9x2" {
		t.Fatalf("podName = %q", gotPod)
	}
}

func TestPodEvents_ReadsWarningEvents(t *testing.T) {
	var gotKind, gotName, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotKind, gotName, gotVersion = q.Get("kind"), q.Get("name"), q.Get("version")
		writeJSON(t, w, http.StatusOK, map[string]interface{}{
			"events": []interface{}{
				map[string]interface{}{
					"type": "Warning", "reason": "Failed",
					"message":       `Error: secret "ca-abc-anthropic" not found`,
					"lastTimestamp": "2026-08-06T10:05:00Z",
				},
			},
		})
	}))
	defer srv.Close()

	events, err := newTestRuntimeClient(t, srv).PodEvents(context.Background(), "acme", "rb-dev", "ca-abc-9x2")
	if err != nil {
		t.Fatalf("PodEvents: %v", err)
	}
	if len(events) != 1 || events[0].Reason != "Failed" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if gotKind != "Pod" || gotName != "ca-abc-9x2" || gotVersion != "v1" {
		t.Fatalf("unexpected query kind=%q name=%q version=%q", gotKind, gotName, gotVersion)
	}
}
