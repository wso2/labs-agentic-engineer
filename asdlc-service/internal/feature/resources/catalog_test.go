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

package resources_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/resources"
)

// fakeResourceClient implements just enough of openchoreo.ResourceClient to
// drive the catalog under test.
type fakeResourceClient struct {
	cts []openchoreo.ResourceType
}

func (f *fakeResourceClient) EnsureResourceType(_ context.Context, _ string, rt *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
	return rt, nil
}
func (f *fakeResourceClient) ApplyResource(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
	return r, nil
}
func (f *fakeResourceClient) GetResource(_ context.Context, _, _ string) (*openchoreo.Resource, error) {
	return nil, nil
}
func (f *fakeResourceClient) EnsureBinding(_ context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (*openchoreo.ResourceReleaseBinding, error) {
	return b, nil
}
func (f *fakeResourceClient) GetBinding(_ context.Context, _, _ string) (*openchoreo.ResourceReleaseBinding, error) {
	return nil, nil
}
func (f *fakeResourceClient) DeleteBinding(_ context.Context, _, _ string) error  { return nil }
func (f *fakeResourceClient) DeleteResource(_ context.Context, _, _ string) error { return nil }
func (f *fakeResourceClient) ListClusterResourceTypes(_ context.Context) ([]openchoreo.ResourceType, error) {
	return f.cts, nil
}
func (f *fakeResourceClient) PatchWorkloadResourceDeps(_ context.Context, _, _ string, _ []openchoreo.WorkloadResourceDep) error {
	return nil
}
func (f *fakeResourceClient) PatchWorkloadEndpointDeps(_ context.Context, _, _ string, _ []openchoreo.WorkloadEndpointDep) error {
	return nil
}
func (f *fakeResourceClient) ListWorkloadEndpoints(_ context.Context, _ string) ([]openchoreo.WorkloadEndpointInfo, error) {
	return nil, nil
}

func TestResourceTypeCatalog_List(t *testing.T) {
	rc := &fakeResourceClient{cts: []openchoreo.ResourceType{
		{Metadata: openchoreo.OCObjectMeta{Name: "redis-cnpg"}},
		{Metadata: openchoreo.OCObjectMeta{Name: "postgres-cnpg"},
			Spec: openchoreo.ResourceTypeSpec{
				Parameters: &openchoreo.SchemaSection{OpenAPIV3Schema: map[string]any{
					"properties": map[string]any{"version": map[string]any{"type": "string"}}}},
				Outputs: []openchoreo.ResourceTypeOutput{{Name: "host"}, {Name: "port"}, {Name: "password"}},
			}},
	}}
	got, err := resources.NewResourceTypeCatalog(rc).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "postgres-cnpg" {
		t.Fatalf("want postgres-cnpg first, got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Outputs, []string{"host", "port", "password"}) {
		t.Fatalf("outputs: %+v", got[0].Outputs)
	}
}
