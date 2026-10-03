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

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
)

type studioStatusStub struct {
	status organization.AEStudioStatus
	err    error
	orgs   []string
}

func (s *studioStatusStub) Status(_ context.Context, org string) (organization.AEStudioStatus, error) {
	s.orgs = append(s.orgs, org)
	return s.status, s.err
}

func TestAEStudioEndpoints_Resolve(t *testing.T) {
	ready := organization.AEStudioStatus{
		State: organization.AEStudioReady,
		URLs:  &organization.AEStudioURLs{Tools: "https://tools.acme.example/"},
		OUID:  "ou-123",
	}
	t.Run("ready answers the internal API root and the OU id", func(t *testing.T) {
		s := &studioStatusStub{status: ready}
		got, err := aeStudioEndpoints{status: s}.Resolve(context.Background(), "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != (aestudiotools.Target{BaseURL: "https://tools.acme.example/internal/v1", ImpersonateOrg: "ou-123"}) {
			t.Fatalf("target = %+v", got)
		}
		if len(s.orgs) != 1 || s.orgs[0] != "default" {
			t.Fatalf("status read for %v, want [default]", s.orgs)
		}
	})
	for _, tc := range []struct {
		name   string
		status organization.AEStudioStatus
		err    error
		want   error
	}{
		{name: "absent", status: organization.AEStudioStatus{State: organization.AEStudioAbsent}, want: aestudiotools.ErrAEStudioAbsent},
		{name: "provisioning", status: organization.AEStudioStatus{State: organization.AEStudioProvisioning}, want: aestudiotools.ErrAEStudioUnavailable},
		{name: "failed", status: organization.AEStudioStatus{State: organization.AEStudioFailed}, want: aestudiotools.ErrAEStudioUnavailable},
		{name: "status read fails", err: errors.New("openchoreo down"), want: aestudiotools.ErrAEStudioUnavailable},
		{name: "ready without a tools URL", status: organization.AEStudioStatus{State: organization.AEStudioReady, URLs: &organization.AEStudioURLs{}, OUID: "ou-123"}, want: aestudiotools.ErrAEStudioUnavailable},
		{name: "ready without URLs", status: organization.AEStudioStatus{State: organization.AEStudioReady, OUID: "ou-123"}, want: aestudiotools.ErrAEStudioUnavailable},
		{name: "ready without an OU id", status: organization.AEStudioStatus{State: organization.AEStudioReady, URLs: ready.URLs}, want: aestudiotools.ErrAEStudioUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := aeStudioEndpoints{status: &studioStatusStub{status: tc.status, err: tc.err}}.Resolve(context.Background(), "default")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// C5: aep-api boots without AE_STUDIO_INTERNAL_CLIENT_SECRET (the
// ae-studio-tools degradation names it); the first call to a ready pod fails
// as misconfigured and never leaves aep-api.
func TestAEStudioTools_WithoutTheInternalClientSecretIsMisconfigured(t *testing.T) {
	pod := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no call may reach the pod") }))
	defer pod.Close()
	var cfg config.AEStudioConfig
	cfg.IDP.TokenURL, cfg.InternalClientID = pod.URL+"/oauth2/token", "ae-studio-internal-client"
	status := &studioStatusStub{status: organization.AEStudioStatus{
		State: organization.AEStudioReady, URLs: &organization.AEStudioURLs{Tools: pod.URL}, OUID: "ou-123",
	}}
	_, err := aeStudioTools(cfg, status).GitHubIdentity(context.Background(), "default")
	if !errors.Is(err, aestudiotools.ErrAEStudioMisconfigured) || !aestudiotools.IsPermanent(err) {
		t.Fatalf("err = %v, want a permanent ErrAEStudioMisconfigured", err)
	}
}
