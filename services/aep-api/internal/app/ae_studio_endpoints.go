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

package app

// ae_studio_endpoints.go — aestudiotools.Endpoints over the org's AE Studio
// status (R13, R18 port): a ready studio's tools URL and the OU id its pod
// pins.

import (
	"context"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/organization"
)

// aeStudioEndpoints resolves an org's ae-studio-tools Target. The status
// reader reads OpenChoreo as aep-api's own identity (aeStudioOCConfig) and
// may start an idempotent converge on drift.
type aeStudioEndpoints struct {
	status organization.AEStudioStatusReader
}

var _ aestudiotools.Endpoints = aeStudioEndpoints{}

// Resolve maps absent to ErrAEStudioAbsent; provisioning, failed (which an
// install without AE_STUDIO_* answers), a ready status without a tools URL
// or OU id, and a failed read to ErrAEStudioUnavailable.
func (e aeStudioEndpoints) Resolve(ctx context.Context, org string) (aestudiotools.Target, error) {
	st, err := e.status.Status(ctx, org)
	if err != nil {
		return aestudiotools.Target{}, fmt.Errorf("%w: read the AE Studio status: %w", aestudiotools.ErrAEStudioUnavailable, err)
	}
	switch st.State {
	case organization.AEStudioAbsent:
		return aestudiotools.Target{}, aestudiotools.ErrAEStudioAbsent
	case organization.AEStudioReady:
	default:
		return aestudiotools.Target{}, fmt.Errorf("%w: AE Studio is %s", aestudiotools.ErrAEStudioUnavailable, st.State)
	}
	if st.URLs == nil || st.URLs.Tools == "" || st.OUID == "" {
		return aestudiotools.Target{}, fmt.Errorf("%w: a ready AE Studio without a tools URL or OU id", aestudiotools.ErrAEStudioUnavailable)
	}
	return aestudiotools.Target{
		BaseURL:        strings.TrimSuffix(st.URLs.Tools, "/") + aestudiotools.InternalAPIPath,
		ImpersonateOrg: st.OUID,
	}, nil
}
