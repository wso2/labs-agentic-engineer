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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// componentListPayload is one OC ComponentList page. Written as a document
// because the fixture's POINT is the labels, which the typed model drops.
func componentListPayload(items ...map[string]any) map[string]any {
	return map[string]any{
		"items":      items,
		"pagination": map[string]any{},
	}
}

func userComponent() map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"name":              "widgets-order-service",
			"creationTimestamp": "2026-08-01T10:00:00Z",
		},
		"spec": map[string]any{
			"componentType": map[string]any{"kind": "ComponentType", "name": "deployment/service"},
			"owner":         map[string]any{"projectName": "widgets"},
		},
	}
}

func agentComponent(name, cycle, created string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"name":              "widgets-" + name,
			"creationTimestamp": created,
			"labels": map[string]any{
				string(LabelKeyAepInternal): LabelValueAepInternal,
				string(LabelKeyAepCycle):    cycle,
				string(LabelKeyAepRunName):  name,
			},
		},
		"spec": map[string]any{
			"componentType": map[string]any{"kind": "ComponentType", "name": CodingAgentComponentTypeRef},
			"owner":         map[string]any{"projectName": "widgets"},
		},
	}
}

// decodeJSONBody decodes a fake server's request body, failing loudly: a silent
// decode error would turn a body assertion into a false pass.
func decodeJSONBody(t *testing.T, r *http.Request, out any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}

func TestIsInternalComponent(t *testing.T) {
	if !isInternalComponent(map[string]string{"aep.wso2.com/internal": "true"}, nil) {
		t.Fatal("annotation should mark internal")
	}
	if !isInternalComponent(nil, map[string]string{"aep.wso2.com/internal": "true"}) {
		t.Fatal("label should mark internal")
	}
	if isInternalComponent(map[string]string{"aep.wso2.com/internal": "false"}, nil) {
		t.Fatal("false must not filter")
	}
	if isInternalComponent(nil, nil) {
		t.Fatal("empty must not filter")
	}
}

func TestListComponents_FiltersInternal(t *testing.T) {
	const project = "proj"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/components") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items": []any{
				map[string]any{
					"metadata": map[string]any{
						"name": ScopedComponentName(project, "ca-hidden"),
						"annotations": map[string]string{
							annotationInternal: "true",
						},
					},
					"spec": map[string]any{
						"componentType": map[string]any{"kind": "ClusterComponentType", "name": "coding-agent"},
						"owner":         map[string]any{"projectName": project},
					},
				},
				map[string]any{
					"metadata": map[string]any{
						"name": ScopedComponentName(project, "web"),
					},
					"spec": map[string]any{
						"componentType": map[string]any{"kind": "ClusterComponentType", "name": "deployment/web-application"},
						"owner":         map[string]any{"projectName": project},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	list, err := c.ListComponents(context.Background(), "org", project, 100, "")
	if err != nil {
		t.Fatalf("ListComponents: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("Items length = %d, want 1", len(list.Items))
	}
	if list.Items[0].Name != "web" {
		t.Fatalf("Name = %q, want %q", list.Items[0].Name, "web")
	}
}

// TestListComponents_DropsInternalComponents is THE choke point: the filter
// lives in the client, so no present or future listing endpoint can leak the
// platform's ephemeral agent components into a user surface.
func TestListComponents_DropsInternalComponents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, componentListPayload(
			userComponent(),
			agentComponent("ca-11111111-2608061200", "cycle-1", "2026-08-01T11:00:00Z"),
		))
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListComponents(context.Background(), "wc-acme", "widgets", 0, "")
	if err != nil {
		t.Fatalf("ListComponents: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("ListComponents returned %d items, want 1 (the internal one must be dropped): %+v", len(got.Items), got.Items)
	}
	if got.Items[0].Name != "order-service" {
		t.Errorf("surviving item = %q, want order-service", got.Items[0].Name)
	}
}

// TestListInternalComponents_ReturnsOnlyTheProjectsMarkedOnes is the reaper's
// read: internal-marked, owned by the project, with the cycle label and the
// creation time the LRU orders on.
func TestListInternalComponents_ReturnsOnlyTheProjectsMarkedOnes(t *testing.T) {
	var gotSelector string
	other := agentComponent("ca-99999999-2608061200", "cycle-9", "2026-08-01T09:00:00Z")
	other["spec"].(map[string]any)["owner"] = map[string]any{"projectName": "gadgets"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSelector = r.URL.Query().Get("labelSelector")
		writeJSON(t, w, http.StatusOK, componentListPayload(
			userComponent(),
			other,
			agentComponent("ca-11111111-2608061200", "cycle-1", "2026-08-01T11:00:00Z"),
		))
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	got, err := c.ListInternalComponents(context.Background(), "wc-acme", "widgets")
	if err != nil {
		t.Fatalf("ListInternalComponents: %v", err)
	}
	if gotSelector != string(LabelKeyAepInternal)+"="+LabelValueAepInternal {
		t.Errorf("labelSelector = %q, want the internal marker", gotSelector)
	}
	if len(got) != 1 {
		t.Fatalf("got %d components, want 1 (another project's must not be returned): %+v", len(got), got)
	}
	if got[0].Name != "ca-11111111-2608061200" {
		t.Errorf("Name = %q, want the friendly ca-… name (DeleteComponent's argument)", got[0].Name)
	}
	if got[0].CycleID != "cycle-1" {
		t.Errorf("CycleID = %q, want cycle-1", got[0].CycleID)
	}
	if got[0].TypeName != CodingAgentComponentTypeRef {
		t.Errorf("TypeName = %q, want %q", got[0].TypeName, CodingAgentComponentTypeRef)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("CreatedAt must be populated — it is the LRU order")
	}
}

// TestCreateComponent_PaymentRequiredIsItsOwnSentinel: the org has no agent
// concurrency slot left. It is a user-actionable BLOCK, not a failure, so it
// must be distinguishable from every other create error at the client boundary.
func TestCreateComponent_PaymentRequiredIsItsOwnSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusPaymentRequired, map[string]string{
			"error": "quota exceeded for resource type coding-agent",
		})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	_, err := c.CreateComponent(context.Background(), "wc-acme", "widgets", &CreateComponentRequest{
		Name: "ca-11111111-2608061200",
		Type: CodingAgentComponentTypeRef,
	})
	if err == nil {
		t.Fatal("expected an error on 402")
	}
	if !errors.Is(err, ErrPaymentRequired) {
		t.Errorf("402 must wrap ErrPaymentRequired, got %v", err)
	}
}

// TestCreateComponent_StampsMarkerLabels proves the markers reach the CR: the
// list filter, the reaper's selector and the cancel path all key off them.
func TestCreateComponent_StampsMarkerLabels(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeJSONBody(t, r, &body)
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"metadata": map[string]any{"name": "widgets-ca-11111111-2608061200"},
			"spec": map[string]any{
				"componentType": map[string]any{"name": CodingAgentComponentTypeRef},
				"owner":         map[string]any{"projectName": "widgets"},
			},
		})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	_, err := c.CreateComponent(context.Background(), "wc-acme", "widgets", &CreateComponentRequest{
		Name: "ca-11111111-2608061200",
		Type: CodingAgentComponentTypeRef,
		Labels: map[string]string{
			string(LabelKeyAepInternal): LabelValueAepInternal,
			string(LabelKeyAepCycle):    "cycle-1",
		},
	})
	if err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
	meta, _ := body["metadata"].(map[string]any)
	labels, _ := meta["labels"].(map[string]any)
	if labels[string(LabelKeyAepInternal)] != LabelValueAepInternal {
		t.Errorf("labels = %v, want the internal marker", labels)
	}
	if labels[string(LabelKeyAepCycle)] != "cycle-1" {
		t.Errorf("labels = %v, want the cycle marker", labels)
	}
}
