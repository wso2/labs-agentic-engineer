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

// Config.ResourceLabels: every resource the clients write carries them.
//
// Driven through each client's public write against a fake OpenChoreo that
// records what it was sent, because what matters is the wire: wso2cloud's
// build workflow reads `cloud.wso2.com/product-name` off the WorkflowRun the
// API receives, and the platform API does not stamp it on an impersonated
// write.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	apigen "github.com/wso2/aep/aep-api/internal/gen"
)

const productLabel = "cloud.wso2.com/product-name"

var testResourceLabels = map[string]string{productLabel: "app-factory"}

// labelWrite is one mutating request the fake received.
type labelWrite struct {
	method   string
	resource string // the path segment after the namespace, e.g. "workflowruns"
	labels   map[string]string
}

// labelRecorder answers just enough of the OpenChoreo API for the writes under test:
// every POST and PUT is recorded and echoed back as the created or updated
// object; a GET of a component returns one with a build workflow, and a GET of
// a release binding returns one with a label of its own. conflictOn makes a
// POST to that resource answer 409, which sends a caller down its converge
// (read-modify-write) path.
type labelRecorder struct {
	mu         sync.Mutex
	writes     []labelWrite
	conflictOn string
}

func (f *labelRecorder) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource := resourceSegment(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			switch resource {
			case "components":
				_, _ = io.WriteString(w, `{"metadata":{"name":"shop-api","labels":{"openchoreo.dev/name":"shop-api"}},
				  "spec":{"owner":{"projectName":"shop"},"componentType":{"kind":"ComponentType","name":"deployment/service"},
				          "workflow":{"kind":"ClusterWorkflow","name":"dockerfile-builder",
				                      "parameters":{"repository":{"url":"https://github.com/acme/shop.git"}}}}}`)
			case "releasebindings":
				_, _ = io.WriteString(w, `{"metadata":{"name":"shop-api-development","labels":{"openchoreo.dev/component":"shop-api"}},
				  "spec":{"owner":{"projectName":"shop","componentName":"shop-api"},"environment":"development"}}`)
			default:
				t.Errorf("unexpected GET %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPost, http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var obj struct {
				Metadata struct {
					Labels map[string]string `json:"labels"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(body, &obj); err != nil {
				t.Errorf("%s %s: body is not an object: %v", r.Method, r.URL.Path, err)
			}
			f.mu.Lock()
			f.writes = append(f.writes, labelWrite{method: r.Method, resource: resource, labels: obj.Metadata.Labels})
			conflict := r.Method == http.MethodPost && resource == f.conflictOn
			f.mu.Unlock()
			switch {
			case conflict:
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":"exists","code":"CONFLICT"}`)
			case r.Method == http.MethodPost:
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write(body)
			default:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
}

// resourceSegment is the collection a namespaced API path addresses:
// /api/v1/namespaces/{ns}/{resource}[/{name}] → resource.
func resourceSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "namespaces" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	return ""
}

func (f *labelRecorder) recorded() []labelWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]labelWrite(nil), f.writes...)
}

func newLabelRecorder(t *testing.T) (*labelRecorder, *httptest.Server) {
	t.Helper()
	f := &labelRecorder{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return f, srv
}

// labelWriteCase is one client write and the resource collection it lands in.
type labelWriteCase struct {
	name     string
	method   string
	resource string
	// conflictOn sends the case down its converge path (see labelRecorder).
	conflictOn string
	do         func(t *testing.T, cfg Config) error
	// keeps is a label the write sets itself, which stamping must not drop.
	keeps map[string]string
}

var labelsCtx = context.Background()

func labelWriteCases() []labelWriteCase {
	return []labelWriteCase{
		{
			name: "build WorkflowRun", method: http.MethodPost, resource: "workflowruns",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewComponentClient(cfg).TriggerBuildAtCommit(labelsCtx, "acme", "shop", "api",
					"abc123def456", "", "shop-api-abc123def456-1")
				return err
			},
			keeps: map[string]string{string(LabelKeyComponent): "shop-api", string(LabelKeyProject): "shop"},
		},
		{
			name: "Component create", method: http.MethodPost, resource: "components",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewComponentClient(cfg).CreateComponent(labelsCtx, "acme", "shop",
					&CreateComponentRequest{Name: "api", Type: "deployment/service"})
				return err
			},
		},
		{
			name: "Component spec update", method: http.MethodPut, resource: "components",
			do: func(_ *testing.T, cfg Config) error {
				return NewComponentClient(cfg).ApplyComponentSpec(labelsCtx, "acme", "shop", "api", ComponentSpecDesired{})
			},
			keeps: map[string]string{"openchoreo.dev/name": "shop-api"},
		},
		{
			name: "ReleaseBinding create", method: http.MethodPost, resource: "releasebindings",
			do: func(_ *testing.T, cfg Config) error {
				return NewComponentClient(cfg).ApplyReleaseBinding(labelsCtx, "acme", "shop",
					ReleaseBindingDesired{ComponentName: "api", Environment: "development", ReleaseName: "shop-api-r1"})
			},
		},
		{
			name: "ReleaseBinding converge", method: http.MethodPut, resource: "releasebindings",
			conflictOn: "releasebindings",
			do: func(_ *testing.T, cfg Config) error {
				return NewComponentClient(cfg).ApplyReleaseBinding(labelsCtx, "acme", "shop",
					ReleaseBindingDesired{ComponentName: "api", Environment: "development", ReleaseName: "shop-api-r1"})
			},
			keeps: map[string]string{"openchoreo.dev/component": "shop-api"},
		},
		{
			name: "Workload create", method: http.MethodPost, resource: "workloads",
			do: func(_ *testing.T, cfg Config) error {
				return NewComponentClient(cfg).EnsureWorkload(labelsCtx, "acme", "shop",
					WorkloadInput{ComponentName: "api", Image: "registry/api:1", Labels: map[string]string{"aep.wso2.com/internal": "true"}})
			},
			keeps: map[string]string{"aep.wso2.com/internal": "true"},
		},
		{
			name: "Project create", method: http.MethodPost, resource: "projects",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewProjectClient(cfg).CreateProject(labelsCtx, "acme", &apigen.CreateProjectRequest{Name: "shop"})
				return err
			},
		},
		{
			name: "SecretReference create", method: http.MethodPost, resource: "secretreferences",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewSecretReferenceClient(cfg).CreateSecretReference(labelsCtx, "acme",
					secretmanagersvc.CreateSecretReferenceRequest{Name: "model-key"})
				return err
			},
		},
		{
			name: "SecretReference update", method: http.MethodPut, resource: "secretreferences",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewSecretReferenceClient(cfg).UpdateSecretReference(labelsCtx, "acme", "model-key",
					secretmanagersvc.CreateSecretReferenceRequest{Name: "model-key"})
				return err
			},
		},
		{
			name: "ProjectReleaseBinding create", method: http.MethodPost, resource: "projectreleasebindings",
			do: func(_ *testing.T, cfg Config) error {
				return NewProjectCellClient(cfg).EnsureProjectReleaseBinding(labelsCtx, "acme", "shop", "development")
			},
			keeps: map[string]string{"openchoreo.dev/project": "shop", "openchoreo.dev/environment": "development"},
		},
		{
			name: "ResourceType create", method: http.MethodPost, resource: "resourcetypes",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewResourceClient(cfg).EnsureResourceType(labelsCtx, "acme", &ResourceType{Metadata: OCObjectMeta{Name: "payments-v1"}})
				return err
			},
		},
		{
			name: "ResourceType update", method: http.MethodPut, resource: "resourcetypes",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewResourceClient(cfg).UpdateResourceType(labelsCtx, "acme", &ResourceType{Metadata: OCObjectMeta{Name: "payments-v1"}})
				return err
			},
		},
		{
			name: "Resource create", method: http.MethodPost, resource: "resources",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewResourceClient(cfg).ApplyResource(labelsCtx, "acme",
					&Resource{Metadata: OCObjectMeta{Name: "db", Labels: map[string]string{"openchoreo.dev/project": "shop"}}})
				return err
			},
			keeps: map[string]string{"openchoreo.dev/project": "shop"},
		},
		{
			name: "ResourceReleaseBinding create", method: http.MethodPost, resource: "resourcereleasebindings",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewResourceClient(cfg).EnsureBinding(labelsCtx, "acme", &ResourceReleaseBinding{Metadata: OCObjectMeta{Name: "db-development"}})
				return err
			},
		},
		{
			name: "ResourceReleaseBinding converge", method: http.MethodPut, resource: "resourcereleasebindings",
			conflictOn: "resourcereleasebindings",
			do: func(_ *testing.T, cfg Config) error {
				_, err := NewResourceClient(cfg).EnsureBinding(labelsCtx, "acme", &ResourceReleaseBinding{Metadata: OCObjectMeta{Name: "db-development"}})
				return err
			},
		},
		{
			name: "ComponentType create", method: http.MethodPost, resource: "componenttypes",
			do: func(_ *testing.T, cfg Config) error {
				return NewComponentClient(cfg).EnsureComponentType(labelsCtx, "acme", map[string]any{
					"metadata": map[string]any{"name": "coding-agent", "labels": map[string]any{"aep.wso2.com/internal": "true"}},
					"spec":     map[string]any{"workloadType": "job"},
				})
			},
			keeps: map[string]string{"aep.wso2.com/internal": "true"},
		},
	}
}

func TestResourceLabels_StampedOnEveryWrite(t *testing.T) {
	for _, tc := range labelWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			fake, srv := newLabelRecorder(t)
			fake.conflictOn = tc.conflictOn
			if err := tc.do(t, Config{BaseURL: srv.URL, ResourceLabels: testResourceLabels}); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, ok := findLabelWrite(fake.recorded(), tc.method, tc.resource)
			if !ok {
				t.Fatalf("no %s to %s was made; writes: %+v", tc.method, tc.resource, fake.recorded())
			}
			if got.labels[productLabel] != "app-factory" {
				t.Fatalf("%s %s must carry %s=app-factory, got labels %v", tc.method, tc.resource, productLabel, got.labels)
			}
			for k, v := range tc.keeps {
				if got.labels[k] != v {
					t.Fatalf("stamping must keep the write's own label %s=%s, got labels %v", k, v, got.labels)
				}
			}
		})
	}
}

// With nothing configured (local: no platform API, no product) the writes are
// exactly what they were.
func TestResourceLabels_NoneConfiguredAddsNothing(t *testing.T) {
	for _, tc := range labelWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			fake, srv := newLabelRecorder(t)
			fake.conflictOn = tc.conflictOn
			if err := tc.do(t, Config{BaseURL: srv.URL}); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, ok := findLabelWrite(fake.recorded(), tc.method, tc.resource)
			if !ok {
				t.Fatalf("no %s to %s was made", tc.method, tc.resource)
			}
			if _, has := got.labels[productLabel]; has {
				t.Fatalf("no resource labels configured, yet %s %s carries %v", tc.method, tc.resource, got.labels)
			}
		})
	}
}

func findLabelWrite(writes []labelWrite, method, resource string) (labelWrite, bool) {
	for _, w := range writes {
		if w.method == method && w.resource == resource {
			return w, true
		}
	}
	return labelWrite{}, false
}

func TestValidateResourceLabels(t *testing.T) {
	valid := []map[string]string{
		nil,
		{"cloud.wso2.com/product-name": "app-factory"},
		{"team": "a.b_c-1"},
		{"example.com/empty": ""},
	}
	for _, labels := range valid {
		if err := ValidateResourceLabels(labels); err != nil {
			t.Errorf("ValidateResourceLabels(%v) = %v, want nil", labels, err)
		}
	}
	invalid := []map[string]string{
		{"": "x"},
		{"cloud.wso2.com/": "x"},
		{"UPPER.example.com/name": "x"},
		{"a/b/c": "x"},
		{"name": "has space"},
		{"name": "-leading"},
		{"name": strings.Repeat("v", 64)},
		{strings.Repeat("k", 64): "x"},
	}
	for _, labels := range invalid {
		if err := ValidateResourceLabels(labels); err == nil {
			t.Errorf("ValidateResourceLabels(%v) = nil, want an error", labels)
		}
	}
}
