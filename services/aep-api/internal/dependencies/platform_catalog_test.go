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

package dependencies

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
)

func TestResourceTypeCatalog_List(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			return []openchoreo.ResourceType{
				{Metadata: openchoreo.OCObjectMeta{Name: "redis-cnpg"}},
				{
					Metadata: openchoreo.OCObjectMeta{Name: "postgres-cnpg"},
					Spec: openchoreo.ResourceTypeSpec{
						Parameters: &openchoreo.SchemaSection{OpenAPIV3Schema: map[string]any{
							"properties": map[string]any{"version": map[string]any{"type": "string"}},
						}},
						Outputs: []openchoreo.ResourceTypeOutput{{Name: "host"}, {Name: "port"}, {Name: "password"}},
					},
				},
			}, nil
		},
	}

	got, err := NewResourceTypeCatalog(rc, true).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by name: postgres-cnpg first.
	if len(got) != 2 || got[0].Name != "postgres-cnpg" {
		t.Fatalf("want postgres-cnpg first, got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Outputs, []string{"host", "port", "password"}) {
		t.Fatalf("outputs: %+v", got[0].Outputs)
	}
	if _, ok := got[0].Parameters["version"]; !ok {
		t.Fatalf("parameters not projected from openAPIV3Schema.properties: %+v", got[0].Parameters)
	}
	// redis-cnpg carries no Parameters spec and no Outputs (zero-value path).
	if got[1].Name != "redis-cnpg" {
		t.Fatalf("want redis-cnpg at index 1, got %q", got[1].Name)
	}
	if got[1].Parameters != nil {
		t.Fatalf("want nil Parameters for redis-cnpg, got %+v", got[1].Parameters)
	}
	if len(got[1].Outputs) != 0 {
		t.Fatalf("want empty Outputs for redis-cnpg, got %+v", got[1].Outputs)
	}
}

func TestResourceTypeCatalog_ListError(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			return nil, errors.New("OC down")
		},
	}
	if _, err := NewResourceTypeCatalog(rc, true).List(context.Background()); err == nil {
		t.Fatal("want the client error surfaced")
	}
}

func TestResourceTypeCatalog_List_ProjectsMarkers(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			return []openchoreo.ResourceType{
				{
					// Carries every marker.
					Metadata: openchoreo.OCObjectMeta{
						Name:   "thunder-app",
						Labels: map[string]string{LabelRole: RoleEndUserAuth},
						Annotations: map[string]string{
							AnnotationConsumerURLEnvConfig: "redirectUris",
							AnnotationConsumerURLPath:      "/oauth2/callback",
							AnnotationSkill:                "thunder-authentication",
							AnnotationDescription:          "End-user sign-in for this project's apps.",
						},
					},
				},
				{
					// Carries no markers at all.
					Metadata: openchoreo.OCObjectMeta{Name: "postgres-cnpg"},
				},
			}, nil
		},
	}

	got, err := NewResourceTypeCatalog(rc, true).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 types, got %d", len(got))
	}
	// Sorted by name: postgres-cnpg first, thunder-app second.
	if got[0].Name != "postgres-cnpg" || got[0].Markers != (TypeMarkers{}) {
		t.Fatalf("postgres-cnpg: want zero-value Markers, got %+v", got[0].Markers)
	}
	// A type without the description annotation projects an empty (and thus
	// omitted-from-JSON) Description.
	if got[0].Description != "" {
		t.Fatalf("postgres-cnpg: Description = %q, want empty", got[0].Description)
	}
	wantMarkers := TypeMarkers{
		EndUserAuth:          true,
		ConsumerURLEnvConfig: "redirectUris",
		ConsumerURLPath:      "/oauth2/callback",
		Skill:                "thunder-authentication",
		Description:          "End-user sign-in for this project's apps.",
	}
	if got[1].Name != "thunder-app" || got[1].Markers != wantMarkers {
		t.Fatalf("thunder-app: Markers = %+v, want %+v", got[1].Markers, wantMarkers)
	}
	// Description is ALSO projected onto the serialized top-level field —
	// the architect-facing MCP payload carries it (unlike Markers, json:"-").
	if got[1].Description != wantMarkers.Description {
		t.Fatalf("thunder-app: Description = %q, want %q", got[1].Description, wantMarkers.Description)
	}
}

func TestResourceTypeCatalog_MarkersByName(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			return []openchoreo.ResourceType{
				{
					Metadata: openchoreo.OCObjectMeta{
						Name:   "thunder-app",
						Labels: map[string]string{LabelRole: RoleEndUserAuth},
						Annotations: map[string]string{
							AnnotationConsumerURLEnvConfig: "redirectUris",
						},
					},
				},
				{Metadata: openchoreo.OCObjectMeta{Name: "postgres-cnpg"}},
			}, nil
		},
	}

	got, err := NewResourceTypeCatalog(rc, true).MarkersByName(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if m := got["thunder-app"]; !m.EndUserAuth || m.ConsumerURLEnvConfig != "redirectUris" || m.ConsumerURLPath != DefaultConsumerURLPath {
		t.Fatalf("thunder-app markers = %+v, want role+env-config+default path", m)
	}
	if m := got["postgres-cnpg"]; m != (TypeMarkers{}) {
		t.Fatalf("postgres-cnpg markers = %+v, want zero value", m)
	}
}

func TestResourceTypeCatalog_MarkersByNameError(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			return nil, errors.New("OC down")
		},
	}
	if _, err := NewResourceTypeCatalog(rc, true).MarkersByName(context.Background()); err == nil {
		t.Fatal("want the client error surfaced")
	}
}

func TestResourceTypeCatalog_List_FlagOff_NoOCCall(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			t.Fatal("ListClusterResourceTypes must not be called when platform resources are disabled")
			return nil, nil
		},
	}

	got, err := NewResourceTypeCatalog(rc, false).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty catalog when disabled, got %+v", got)
	}
	if n := len(rc.ListClusterResourceTypesCalls()); n != 0 {
		t.Fatalf("want 0 OC calls, got %d", n)
	}
}

func TestResourceTypeCatalog_List_FlagOn_Unchanged(t *testing.T) {
	t.Parallel()

	rc := &ocmocks.ResourceClientMock{
		ListClusterResourceTypesFunc: func(_ context.Context) ([]openchoreo.ResourceType, error) {
			return []openchoreo.ResourceType{
				{Metadata: openchoreo.OCObjectMeta{Name: "postgres-cnpg"}},
			}, nil
		},
	}

	got, err := NewResourceTypeCatalog(rc, true).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "postgres-cnpg" {
		t.Fatalf("want postgres-cnpg, got %+v", got)
	}
	if n := len(rc.ListClusterResourceTypesCalls()); n != 1 {
		t.Fatalf("want 1 OC call, got %d", n)
	}
}
