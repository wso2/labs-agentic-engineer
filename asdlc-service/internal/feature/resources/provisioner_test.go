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
	"encoding/json"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/resources"
)

func TestBuildPlatformResource_ReferencesClusterResourceType(t *testing.T) {
	r := resources.BuildPlatformResource("shop", "maindb", "postgres-cnpg", map[string]string{"version": "16"})
	if r.Spec.Type.Kind != "ClusterResourceType" || r.Spec.Type.Name != "postgres-cnpg" {
		t.Fatalf("type ref: %+v", r.Spec.Type)
	}
	if r.Metadata.Name != "shop-maindb" {
		t.Fatalf("name: %s", r.Metadata.Name)
	}
	if r.Spec.Owner.ProjectName != "shop" {
		t.Fatalf("owner: %+v", r.Spec.Owner)
	}
	var got map[string]string
	_ = json.Unmarshal(r.Spec.Parameters, &got)
	if got["version"] != "16" {
		t.Fatalf("params: %s", r.Spec.Parameters)
	}
}

func TestBuildPlatformBinding_PinsRelease(t *testing.T) {
	b, err := resources.BuildPlatformBinding("shop", "maindb", "development", "shop-maindb-r1", map[string]string{"size": "small"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Spec.ResourceRelease != "shop-maindb-r1" || b.Spec.Environment != "development" {
		t.Fatalf("binding: %+v", b.Spec)
	}
	if b.Metadata.Name != "shop-maindb-development" {
		t.Fatalf("name: %s", b.Metadata.Name)
	}
}

// recordingRC records calls and returns a Resource that already has a
// latestRelease but a binding that is NOT ready — proving Provision returns
// success without waiting on the binding Ready condition. (catalog_test.go owns
// the minimal fakeResourceClient; this one records the author flow.)
type recordingRC struct {
	ensureRTCalls int
	appliedRes    *openchoreo.Resource
	bindings      []*openchoreo.ResourceReleaseBinding
	latest        string
	getResource   int
}

func (f *recordingRC) EnsureResourceType(_ context.Context, _ string, rt *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
	f.ensureRTCalls++
	return rt, nil
}
func (f *recordingRC) ApplyResource(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
	f.appliedRes = r
	return r, nil
}
func (f *recordingRC) GetResource(_ context.Context, _, name string) (*openchoreo.Resource, error) {
	f.getResource++
	return &openchoreo.Resource{
		Metadata: openchoreo.OCObjectMeta{Name: name},
		Status:   &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: f.latest}},
	}, nil
}
func (f *recordingRC) EnsureBinding(_ context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (*openchoreo.ResourceReleaseBinding, error) {
	f.bindings = append(f.bindings, b)
	return b, nil
}
func (f *recordingRC) GetBinding(_ context.Context, _, name string) (*openchoreo.ResourceReleaseBinding, error) {
	// NOT ready — Provision must not block on this.
	return &openchoreo.ResourceReleaseBinding{
		Metadata: openchoreo.OCObjectMeta{Name: name},
		Status:   &openchoreo.ResourceReleaseBindingStatus{Conditions: []openchoreo.OCCondition{{Type: "Ready", Status: "False"}}},
	}, nil
}
func (f *recordingRC) DeleteBinding(_ context.Context, _, _ string) error  { return nil }
func (f *recordingRC) DeleteResource(_ context.Context, _, _ string) error { return nil }
func (f *recordingRC) ListClusterResourceTypes(_ context.Context) ([]openchoreo.ResourceType, error) {
	return nil, nil
}
func (f *recordingRC) PatchWorkloadResourceDeps(_ context.Context, _, _ string, _ []openchoreo.WorkloadResourceDep) error {
	return nil
}
func (f *recordingRC) PatchWorkloadEndpointDeps(_ context.Context, _, _ string, _ []openchoreo.WorkloadEndpointDep) error {
	return nil
}
func (f *recordingRC) ListWorkloadEndpoints(_ context.Context, _ string) ([]openchoreo.WorkloadEndpointInfo, error) {
	return nil, nil
}

func TestProvision_AuthorsResourceModel_Async(t *testing.T) {
	rc := &recordingRC{latest: "shop-maindb-r1"}
	p := resources.NewOCNativeProvisioner(rc)

	res, err := p.Provision(
		context.Background(),
		"default", "shop", "maindb", "postgres-cnpg",
		map[string]string{"version": "16"},
		[]string{"development", "production"},
	)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Never authors the type — it's discovered, not authored.
	if rc.ensureRTCalls != 0 {
		t.Fatalf("EnsureResourceType must NEVER be called, got %d", rc.ensureRTCalls)
	}

	// Authored exactly one Resource against the discovered ClusterResourceType.
	if rc.appliedRes == nil {
		t.Fatal("Resource not applied")
	}
	if rc.appliedRes.Spec.Type.Kind != "ClusterResourceType" || rc.appliedRes.Spec.Type.Name != "postgres-cnpg" {
		t.Fatalf("type ref: %+v", rc.appliedRes.Spec.Type)
	}
	if rc.appliedRes.Metadata.Name != "shop-maindb" {
		t.Fatalf("resource name: %s", rc.appliedRes.Metadata.Name)
	}
	if rc.getResource == 0 {
		t.Fatal("GetResource (poll for latestRelease) never called")
	}

	// One binding per env, pinned to the polled release.
	if len(rc.bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d", len(rc.bindings))
	}
	for _, b := range rc.bindings {
		if b.Spec.ResourceRelease != "shop-maindb-r1" {
			t.Errorf("binding not pinned: %q", b.Spec.ResourceRelease)
		}
	}

	// Result reports the resource + per-env binding names — returned success even
	// though the binding is NOT Ready (a real DB takes minutes; the watcher
	// observes readiness later).
	if res.ResourceName != "shop-maindb" {
		t.Errorf("result resource name: %s", res.ResourceName)
	}
	if res.BindingByEnv["development"] != "shop-maindb-development" || res.BindingByEnv["production"] != "shop-maindb-production" {
		t.Errorf("result bindings: %+v", res.BindingByEnv)
	}
}

func TestProvision_RequiresArgs(t *testing.T) {
	rc := &recordingRC{latest: "r1"}
	p := resources.NewOCNativeProvisioner(rc)
	if _, err := p.Provision(context.Background(), "", "shop", "maindb", "postgres-cnpg", nil, []string{"development"}); err == nil {
		t.Fatal("expected error for empty orgHandle")
	}
	_ = time.Second
}
