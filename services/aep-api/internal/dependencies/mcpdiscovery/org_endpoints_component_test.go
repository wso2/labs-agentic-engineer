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

// COMPONENT tier: GET /dependencies/org-endpoints through the REAL production
// handler chain — faked auth → contract validation → the deny-by-default
// tenant gate in ENFORCE → the strict handler — with only the org-endpoint
// lister faked. The BFF lists namespace-visible org-service APIs as a bare
// JSON array (not the MCP {endpoints:[…]} wrapper).
//
// External test package: the harness imports edge, which imports the
// dependencies domain — an in-package test file would be an import cycle.
package mcpdiscovery_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	dephttpapi "github.com/wso2/aep/aep-api/internal/dependencies/httpapi"
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
)

type stubOrgEndpointLister struct {
	items   []openchoreo.WorkloadEndpointInfo
	err     error
	lastOrg string
}

func (s *stubOrgEndpointLister) List(_ context.Context, orgHandle string) ([]openchoreo.WorkloadEndpointInfo, error) {
	s.lastOrg = orgHandle
	return s.items, s.err
}

func (s *stubOrgEndpointLister) ListResolved(context.Context, string) ([]dependencies.OrgComponentEndpoint, error) {
	return nil, nil
}

func newOrgEndpointHarness(t *testing.T, lister mcpdiscovery.OrgEndpointLister) *componenttest.Harness {
	t.Helper()
	deps, err := dephttpapi.New(dephttpapi.Deps{OrgEndpoints: lister})
	if err != nil {
		t.Fatalf("assemble dependencies domain: %v", err)
	}
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{Dependencies: deps}})
}

const orgEndpointsPath = "/api/v1/dependencies/org-endpoints"

// TestOrgEndpoints_FiltersToNamespaceVisible keeps only the namespace-visible
// row and maps MCP list_org_endpoints fields onto the bare JSON array item
// (name=component, project, endpoint=name, type, namespaceVisible=true).
func TestOrgEndpoints_FiltersToNamespaceVisible(t *testing.T) {
	t.Parallel()
	lister := &stubOrgEndpointLister{items: []openchoreo.WorkloadEndpointInfo{
		{Project: "billing", Component: "invoice-api", Name: "rest", Type: "HTTP", Visibility: []string{"namespace"}},
		{Project: "crm", Component: "leads-api", Name: "grpc", Type: "gRPC"},
		{Project: "edge", Component: "public-api", Name: "http", Type: "HTTP", Visibility: []string{"external"}},
	}}
	h := newOrgEndpointHarness(t, lister)

	resp := h.AsOrg("acme").Get(orgEndpointsPath)
	if resp.Code != 200 {
		t.Fatalf("list: got %d body=%s", resp.Code, resp.Body.String())
	}
	raw := resp.Body.Bytes()
	if len(raw) > 0 && raw[0] == '{' {
		t.Fatalf("want a JSON array, not an object wrapper: %s", resp.Body.String())
	}
	var got []gen.OrgEndpointDTO
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "invoice-api" || got[0].Project != "billing" || got[0].Endpoint != "rest" {
		t.Errorf("name/project/endpoint not mapped: %+v", got[0])
	}
	if string(got[0].Type) != "HTTP" {
		t.Errorf("type = %q, want HTTP", got[0].Type)
	}
	if !got[0].NamespaceVisible {
		t.Errorf("namespaceVisible = false, want true")
	}
	if lister.lastOrg != "acme" {
		t.Errorf("lister org = %q, want acme", lister.lastOrg)
	}
}

// TestOrgEndpoints_TwoNamespaceVisibleSameProject returns both namespace-visible
// rows that share a project.
func TestOrgEndpoints_TwoNamespaceVisibleSameProject(t *testing.T) {
	t.Parallel()
	h := newOrgEndpointHarness(t, &stubOrgEndpointLister{items: []openchoreo.WorkloadEndpointInfo{
		{Project: "billing", Component: "invoice-api", Name: "rest", Type: "HTTP", Visibility: []string{"namespace"}},
		{Project: "billing", Component: "payments-api", Name: "grpc", Type: "gRPC", Visibility: []string{"namespace"}},
	}})

	resp := h.AsOrg("acme").Get(orgEndpointsPath)
	if resp.Code != 200 {
		t.Fatalf("list: got %d body=%s", resp.Code, resp.Body.String())
	}
	var got []gen.OrgEndpointDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for i, row := range got {
		if !row.NamespaceVisible {
			t.Errorf("row %d namespaceVisible = false, want true", i)
		}
	}
}

// TestOrgEndpoints_EmptyOrgReturnsEmptyArray pins the empty-org wire shape:
// a JSON array `[]`, never JSON null.
func TestOrgEndpoints_EmptyOrgReturnsEmptyArray(t *testing.T) {
	t.Parallel()
	h := newOrgEndpointHarness(t, &stubOrgEndpointLister{})

	resp := h.AsOrg("acme").Get(orgEndpointsPath)
	if resp.Code != 200 {
		t.Fatalf("list: got %d body=%s", resp.Code, resp.Body.String())
	}
	if strings.TrimSpace(resp.Body.String()) != "[]" {
		t.Fatalf("empty body = %q, want []", resp.Body.String())
	}
}

// A catalog read failure is an upstream (data-plane) fault — 502, opaque.
func TestOrgEndpoints_ListerFailure502(t *testing.T) {
	t.Parallel()
	h := newOrgEndpointHarness(t, &stubOrgEndpointLister{err: errors.New("boom")})

	resp := h.AsOrg("acme").Get(orgEndpointsPath)
	if resp.Code != 502 {
		t.Fatalf("want 502, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "bad_gateway" || e.Message != "failed to list org endpoints" {
		t.Fatalf("502 envelope = %+v", e)
	}
}

// The endpoint requires an authenticated caller (the org-scoped auth fence):
// claimless is the gate's ENFORCE 401.
func TestOrgEndpoints_NoClaims401(t *testing.T) {
	t.Parallel()
	h := newOrgEndpointHarness(t, &stubOrgEndpointLister{})

	if resp := h.NoAuth().Get(orgEndpointsPath); resp.Code != 401 {
		t.Fatalf("claimless: want 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// A nil catalog keeps the surface present but unwired — 503, mirroring
// ListPlatformResourceTypes.
func TestOrgEndpoints_Unconfigured503(t *testing.T) {
	t.Parallel()
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{}})

	resp := h.AsOrg("acme").Get(orgEndpointsPath)
	if resp.Code != 503 {
		t.Fatalf("want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "service_unavailable" || e.Message != "org-endpoint catalog is not configured" {
		t.Fatalf("503 envelope = %+v", e)
	}
}
