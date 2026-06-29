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
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// emptyBody is passed to humatest.Post for operations that take no request body
// (the save-and-proceed handler has no body).
var emptyBody = struct{}{}

// stubDesignService is a minimal DesignService stub for handler-level tests.
// Only SaveAndProceed has interesting behaviour; all other methods return
// sensible zero values.
type stubDesignService struct {
	saveAndProceedErr error
}

func (s *stubDesignService) GetDesign(_ context.Context, _, _ string) (*models.Design, error) {
	return &models.Design{}, nil
}
func (s *stubDesignService) GetDesignBundle(_ context.Context, _, _ string) (*DesignBundle, error) {
	return &DesignBundle{Files: map[string]string{}, Design: &models.Design{}}, nil
}
func (s *stubDesignService) GetDesignAtTag(_ context.Context, _, _, _ string) (*models.Design, error) {
	return &models.Design{}, nil
}
func (s *stubDesignService) GetDesignBundleAtTag(_ context.Context, _, _, _ string) (*DesignBundle, error) {
	return &DesignBundle{Files: map[string]string{}, Design: &models.Design{}}, nil
}
func (s *stubDesignService) StreamGenerateDesign(_ context.Context, _, _ string, _ io.Writer, _ func()) error {
	return nil
}
func (s *stubDesignService) UpdateDesignFile(_ context.Context, _, _, _, _ string) (*DesignBundle, error) {
	return &DesignBundle{Files: map[string]string{}, Design: &models.Design{}}, nil
}
func (s *stubDesignService) DeleteDesignFile(_ context.Context, _, _, _ string) (*DesignBundle, error) {
	return &DesignBundle{Files: map[string]string{}, Design: &models.Design{}}, nil
}
func (s *stubDesignService) DeleteComponent(_ context.Context, _, _, _ string) (*DesignBundle, error) {
	return &DesignBundle{Files: map[string]string{}, Design: &models.Design{}}, nil
}
func (s *stubDesignService) SaveAndProceed(_ context.Context, _, _ string) (*models.Design, error) {
	if s.saveAndProceedErr != nil {
		return nil, s.saveAndProceedErr
	}
	return &models.Design{}, nil
}
func (s *stubDesignService) DiscardChanges(_ context.Context, _, _ string) (*models.Design, error) {
	return &models.Design{}, nil
}
func (s *stubDesignService) ListDesignVersions(_ context.Context, _, _ string) ([]models.ArtifactVersion, error) {
	return nil, nil
}
func (s *stubDesignService) CollectSpec(_ context.Context, _, _, _, _, _, _ string) (string, int, error) {
	return "", 0, nil
}

// TestSaveAndProceedHandler_UnresolvedDependency_Returns409 asserts that when
// the service returns ErrUnresolvedDependency, the HTTP handler maps it to 409
// Conflict (not 500) and the response body carries the gate message.
func TestSaveAndProceedHandler_UnresolvedDependency_Returns409(t *testing.T) {
	humakit.SetGateMode(tenant.GateModeLog)
	t.Cleanup(func() { humakit.SetGateMode(tenant.GateModeEnforce) })

	svcErr := fmt.Errorf("%w: component %q dep %q (status: %s)",
		ErrUnresolvedDependency, "my-component", "openweather", "unresolved")
	svc := &stubDesignService{saveAndProceedErr: svcErr}

	_, api := humatest.New(t)
	RegisterDesign(api, svc)

	resp := api.Post(
		"/api/v1/organizations/acme/projects/my-project/design/save",
		emptyBody,
	)
	if resp.Code != 409 {
		t.Fatalf("want HTTP 409 for ErrUnresolvedDependency, got %d; body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "unresolved") {
		t.Errorf("want gate message in response body, got: %s", resp.Body.String())
	}
}

// TestSaveAndProceedHandler_SpecNotApproved_Returns409 asserts that the
// existing ErrSpecNotApproved mapping still returns 409 (regression guard).
func TestSaveAndProceedHandler_SpecNotApproved_Returns409(t *testing.T) {
	humakit.SetGateMode(tenant.GateModeLog)
	t.Cleanup(func() { humakit.SetGateMode(tenant.GateModeEnforce) })

	svc := &stubDesignService{saveAndProceedErr: ErrSpecNotApproved}

	_, api := humatest.New(t)
	RegisterDesign(api, svc)

	resp := api.Post(
		"/api/v1/organizations/acme/projects/my-project/design/save",
		emptyBody,
	)
	if resp.Code != 409 {
		t.Fatalf("want HTTP 409 for ErrSpecNotApproved, got %d; body=%s", resp.Code, resp.Body.String())
	}
}
