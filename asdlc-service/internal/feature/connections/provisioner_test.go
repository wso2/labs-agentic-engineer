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

package connections

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakeRC records calls and returns a Resource that already has a latestRelease.
type fakeRC struct {
	ensuredRT       *openchoreo.ResourceType
	appliedRes      *openchoreo.Resource
	bindings        []*openchoreo.ResourceReleaseBinding
	latest          string
	patchedWorkload string
	patchedDeps     []openchoreo.WorkloadResourceDep
}

func (f *fakeRC) EnsureResourceType(_ context.Context, _ string, rt *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
	f.ensuredRT = rt
	return rt, nil
}
func (f *fakeRC) ApplyResource(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
	f.appliedRes = r
	return r, nil
}
func (f *fakeRC) GetResource(_ context.Context, _, name string) (*openchoreo.Resource, error) {
	return &openchoreo.Resource{
		Metadata: openchoreo.OCObjectMeta{Name: name},
		Status:   &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: f.latest}},
	}, nil
}
func (f *fakeRC) EnsureBinding(_ context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (*openchoreo.ResourceReleaseBinding, error) {
	f.bindings = append(f.bindings, b)
	return b, nil
}
func (f *fakeRC) GetBinding(_ context.Context, _, _ string) (*openchoreo.ResourceReleaseBinding, error) {
	return &openchoreo.ResourceReleaseBinding{}, nil
}
func (f *fakeRC) DeleteBinding(_ context.Context, _, _ string) error  { return nil }
func (f *fakeRC) DeleteResource(_ context.Context, _, _ string) error { return nil }
func (f *fakeRC) ListClusterResourceTypes(_ context.Context) ([]openchoreo.ResourceType, error) {
	return nil, nil
}
func (f *fakeRC) PatchWorkloadResourceDeps(_ context.Context, _, workloadName string, resources []openchoreo.WorkloadResourceDep) error {
	f.patchedWorkload = workloadName
	f.patchedDeps = resources
	return nil
}

type fakeSW struct{ wrote map[string]map[string]string }

func (s *fakeSW) Enabled() bool { return true }
func (s *fakeSW) WriteConnectionSecret(_ context.Context, _, _, entity string, data map[string]string) (string, string, error) {
	if s.wrote == nil {
		s.wrote = map[string]map[string]string{}
	}
	s.wrote[entity] = data
	return "user-app-secrets/wc-org/cred-" + entity, "cred-" + entity, nil
}

func TestProvisionConnection_OrchestratesResourceModel(t *testing.T) {
	rc := &fakeRC{latest: "openweather-proj-abc123"}
	sw := &fakeSW{}
	p := &Provisioner{rc: rc, sm: sw, pollInterval: time.Millisecond, pollTimeout: time.Second}

	conn := &models.Connection{
		Name:             "openweather",
		ResourceTypeName: "openweather",
		ConfigSchema: []models.ConfigKey{
			{Key: "OPENWEATHER_BASE_URL", Secret: false},
			{Key: "OPENWEATHER_API_KEY", Secret: true},
		},
	}
	byEnv := map[string]EnvConnectionValues{
		"development": {
			Plain:  map[string]string{"OPENWEATHER_BASE_URL": "https://api.openweathermap.org"},
			Secret: map[string]string{"OPENWEATHER_API_KEY": "k123"},
		},
	}

	res, err := p.ProvisionConnection(context.Background(), "default", "default", "weatherproj", conn, byEnv)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// ResourceType built from the schema + ensured, under the version-pinned name.
	wantRT := openchoreo.ConnectionRTName("openweather")
	if rc.ensuredRT == nil || rc.ensuredRT.Metadata.Name != wantRT {
		t.Fatalf("resourcetype not ensured under %q: %+v", wantRT, rc.ensuredRT)
	}
	// Resource: project-prefixed name, owner, type ref points at the versioned RT.
	if rc.appliedRes.Metadata.Name != "weatherproj-openweather" {
		t.Errorf("resource name = %q", rc.appliedRes.Metadata.Name)
	}
	if rc.appliedRes.Spec.Owner.ProjectName != "weatherproj" || rc.appliedRes.Spec.Type.Name != wantRT {
		t.Errorf("resource spec wrong: %+v", rc.appliedRes.Spec)
	}
	if res.LatestRelease != "openweather-proj-abc123" {
		t.Errorf("latestRelease = %q", res.LatestRelease)
	}

	// Secret written under the per-env entity.
	if _, ok := sw.wrote["conn-openweather-development"]; !ok {
		t.Fatalf("secret not written per-env: %v", sw.wrote)
	}

	// One binding, pinned, with plain value + secretStorePath in env configs.
	if len(rc.bindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(rc.bindings))
	}
	b := rc.bindings[0]
	if b.Metadata.Name != "weatherproj-openweather-development" {
		t.Errorf("binding name = %q", b.Metadata.Name)
	}
	if b.Spec.ResourceRelease != "openweather-proj-abc123" {
		t.Errorf("binding not pinned: %q", b.Spec.ResourceRelease)
	}
	if b.Spec.Owner.ResourceName != "weatherproj-openweather" || b.Spec.Environment != "development" {
		t.Errorf("binding owner/env wrong: %+v", b.Spec)
	}
	var cfg map[string]string
	if err := json.Unmarshal(b.Spec.ResourceTypeEnvironmentConfigs, &cfg); err != nil {
		t.Fatalf("env configs not json: %v", err)
	}
	if cfg["OPENWEATHER_BASE_URL"] != "https://api.openweathermap.org" {
		t.Errorf("plain value missing from env configs: %v", cfg)
	}
	if cfg[openchoreo.SecretStorePathField] == "" {
		t.Errorf("secretStorePath missing from env configs: %v", cfg)
	}
}

func TestProvisionConnection_AllPlain_NoSecretWrite(t *testing.T) {
	rc := &fakeRC{latest: "rel-1"}
	sw := &fakeSW{}
	p := &Provisioner{rc: rc, sm: sw, pollInterval: time.Millisecond, pollTimeout: time.Second}
	conn := &models.Connection{Name: "plainsvc", ResourceTypeName: "plainsvc", ConfigSchema: []models.ConfigKey{{Key: "BASE_URL"}}}
	byEnv := map[string]EnvConnectionValues{"development": {Plain: map[string]string{"BASE_URL": "https://x"}}}
	if _, err := p.ProvisionConnection(context.Background(), "default", "default", "proj", conn, byEnv); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if len(sw.wrote) != 0 {
		t.Errorf("no secret should be written for an all-plain connection: %v", sw.wrote)
	}
}
