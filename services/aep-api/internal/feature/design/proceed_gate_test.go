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

package design

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts/artifactstest"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// --- proceed-gate (dependency-management Phase 5) ----------------------------
//
// SaveAndProceed (the tag-cut = approve) blocks on exactly two dependency
// conditions and no others: an `org-service` that is not namespace-visible
// (unresolved/blocked/ambiguous against the live catalog) and an `external`
// that declares needsSpec but has no collected spec yet. external-values
// (config-only external) and platform-resource deps are NOT proceed-gated —
// they are dispatch-gated in Phase 6.

// gateOrgResolver is a static artifacts.OrgServiceResolver for the gate tests:
// `visible` names publish a namespace-visible endpoint (→ resolved), `exists`
// names publish only project-only (→ blocked/access-required); anything else is
// absent (→ unresolved/not-found).
type gateOrgResolver struct {
	visible map[string]bool
	exists  map[string]bool
}

func (r gateOrgResolver) IsNamespaceVisible(_ context.Context, _, name string) (bool, error) {
	return r.visible[name], nil
}

func (r gateOrgResolver) ExistsAnyVisibility(_ context.Context, _, name string) (bool, error) {
	return r.exists[name] || r.visible[name], nil
}

// recordingRegistrar records the external-resource Upsert calls made on save.
type recordingRegistrar struct{ names []string }

func (r *recordingRegistrar) Upsert(_ context.Context, _, name, _ string, _ []models.ConfigKey) (*models.ExternalResource, error) {
	r.names = append(r.names, name)
	return &models.ExternalResource{Name: name}, nil
}

// designFilesWithDeps is a well-formed design tree whose single `consumer`
// component carries the given dependencies JSON array.
func designFilesWithDeps(depsJSON string) map[string]string {
	return map[string]string{
		artifacts.DesignRootFile: "---\nsourceSpec: v1\n---\n\nOverview.\n",
		"components/consumer/design.json": `{
  "name": "consumer",
  "type": "service",
  "language": "Go",
  "description": "Consumes deps.",
  "dependencies": ` + depsJSON + `
}
`,
	}
}

// readsFor wires a fake artifact service for the SaveAndProceed pre-gate read
// (HEAD resolution) with the given design tree. SaveDesign is set to fail the
// test — a proceed-gate that blocks must never reach the tag-cut.
func readsFor(t *testing.T, files map[string]string) *artifactstest.FakeArtifactService {
	t.Helper()
	return &artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
		SaveDesignFunc: func(context.Context, string, string, artifacts.SaveRequest) (*artifacts.DesignSaveResult, error) {
			t.Error("proceed-gate should have blocked before SaveDesign (tag-cut) was reached")
			return nil, errors.New("SaveDesign must not be called")
		},
	}
}

func TestSaveAndProceed_GateBlocksUnreachableOrgService(t *testing.T) {
	t.Parallel()
	fake := readsFor(t, designFilesWithDeps(`[{"kind":"org-service","name":"billing"}]`))
	svc := newService(fake)
	// billing is neither namespace-visible nor present → unresolved/not-found.
	svc.store.SetOrgServiceResolver(gateOrgResolver{})

	_, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if !errors.Is(err, ErrUnresolvedDependency) {
		t.Fatalf("unreachable org-service: want ErrUnresolvedDependency, got %v", err)
	}
}

func TestSaveAndProceed_GateBlocksProjectOnlyOrgService(t *testing.T) {
	t.Parallel()
	fake := readsFor(t, designFilesWithDeps(`[{"kind":"org-service","name":"payroll"}]`))
	svc := newService(fake)
	// payroll exists but is project-only → blocked/access-required (still gated).
	svc.store.SetOrgServiceResolver(gateOrgResolver{exists: map[string]bool{"payroll": true}})

	_, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if !errors.Is(err, ErrUnresolvedDependency) {
		t.Fatalf("project-only org-service: want ErrUnresolvedDependency, got %v", err)
	}
}

func TestSaveAndProceed_GateBlocksExternalNeedsSpec(t *testing.T) {
	t.Parallel()
	// No resolver needed — an external needsSpec dep with no specPath is
	// unresolved purely from the authored design.json.
	fake := readsFor(t, designFilesWithDeps(`[{"kind":"external","name":"stripe","needsSpec":true}]`))

	_, err := newService(fake).SaveAndProceed(context.Background(), "acme", "web", "")
	if !errors.Is(err, ErrUnresolvedDependency) {
		t.Fatalf("external needs-spec: want ErrUnresolvedDependency, got %v", err)
	}
}

// happySave wires a fake that lets the tag-cut through: a resolved read, a
// successful SaveDesign, and a version list.
func happySave(files map[string]string) *artifactstest.FakeArtifactService {
	return &artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
		SaveDesignFunc: func(context.Context, string, string, artifacts.SaveRequest) (*artifacts.DesignSaveResult, error) {
			return &artifacts.DesignSaveResult{Status: "approved", Tag: "v1-1", RequirementsVersion: 1, DesignRevision: 1}, nil
		},
		ListDesignVersionsFunc: func(context.Context, string, string) ([]artifacts.DesignVersionInfo, error) {
			return []artifacts.DesignVersionInfo{{Tag: "v1-1", RequirementsVersion: 1, DesignRevision: 1}}, nil
		},
	}
}

func TestSaveAndProceed_GateAllowsResolvedOrgService(t *testing.T) {
	t.Parallel()
	fake := happySave(designFilesWithDeps(`[{"kind":"org-service","name":"billing"}]`))
	svc := newService(fake)
	svc.store.SetOrgServiceResolver(gateOrgResolver{visible: map[string]bool{"billing": true}})

	got, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if err != nil {
		t.Fatalf("resolved org-service: want success, got %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("resolved org-service: status = %q, want approved", got.Status)
	}
}

// TestSaveAndProceed_GateIgnoresValuesAndPlatformResource asserts the gate does
// NOT block config-only external deps or platform-resource deps (they are
// dispatch-gated in Phase 6), and that external deps are registered into the
// org catalog on a successful save.
func TestSaveAndProceed_GateIgnoresValuesAndPlatformResource(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"external","name":"sendgrid"},{"kind":"platform-resource","name":"orders-db","resourceType":"postgres"}]`
	fake := happySave(designFilesWithDeps(deps))
	svc := newService(fake)
	reg := &recordingRegistrar{}
	svc.SetExternalResourceRegistry(reg)
	// A platform-resource dep is present, so the save fetches CRT markers; the
	// postgres type carries no end-user-auth role, so nothing is stamped.
	svc.resourceCatalog = &fakeMarkerCatalog{markers: map[string]resources.TypeMarkers{}}

	got, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if err != nil {
		t.Fatalf("values + platform-resource: want success, got %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	// The config-only external dep is registered on save; the platform-resource
	// is not an external and must not be.
	if len(reg.names) != 1 || reg.names[0] != "sendgrid" {
		t.Fatalf("external-resource registration: got %v, want [sendgrid]", reg.names)
	}
}
