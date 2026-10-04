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

package organization_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// probeCredSvc is the credential service the probes delegate their row
// writes to; ProbePAT itself reads nothing from it.
func probeCredSvc() *organization.CredentialService {
	return organization.NewCredentialService(nil, nil, nil, "")
}

func TestProbePAT_UsesIdentityAndSkipsWhenUnavailable(t *testing.T) {
	f := aestudiotest.New()
	f.SetIdentity("default", &sourcecontrol.GitHubUser{Login: "acme-bot", ID: 1})
	p := organization.NewValidatorProbes(probeCredSvc(), f)
	login, _, _, err := p.ProbePAT(context.Background(), secrets.ActiveRow{OcOrgID: "default"})
	if err != nil || login != "acme-bot" {
		t.Fatalf("login=%q err=%v", login, err)
	}
	f.FailOrg("default", sourcecontrol.ErrAEStudioUnavailable)
	if _, _, _, err := p.ProbePAT(context.Background(), secrets.ActiveRow{OcOrgID: "default"}); !errors.Is(err, secrets.ErrCredentialTransient) {
		t.Fatalf("err = %v, want transient", err)
	}
}

// The pod's answer decides the tick: GitHub refusing the gitpat
// (github_error 401/403/404) cascades, the pod being absent, down or
// misconfigured skips the tick (the AE-only token is not the user's PAT),
// and the identity defaults fill name and email.
func TestProbePAT_MapsThePodsAnswer(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"github 401", &sourcecontrol.HTTPStatusError{StatusCode: 401}, secrets.ErrCredentialUnauthorized},
		{"github 403", &sourcecontrol.HTTPStatusError{StatusCode: 403}, secrets.ErrCredentialUnauthorized},
		{"github 404", &sourcecontrol.HTTPStatusError{StatusCode: 404}, secrets.ErrCredentialUnauthorized},
		{"github 502", &sourcecontrol.HTTPStatusError{StatusCode: 502}, secrets.ErrCredentialTransient},
		{"rate limited", &sourcecontrol.RateLimitedError{}, secrets.ErrCredentialTransient},
		{"absent", sourcecontrol.ErrAEStudioAbsent, secrets.ErrCredentialTransient},
		{"unavailable", sourcecontrol.ErrAEStudioUnavailable, secrets.ErrCredentialTransient},
		{"misconfigured", sourcecontrol.ErrAEStudioMisconfigured, secrets.ErrCredentialTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := aestudiotest.New()
			f.SetIdentity("default", &sourcecontrol.GitHubUser{Login: "acme-bot"})
			f.FailOp(aestudiotest.OpGitHubIdentity, tc.err)
			p := organization.NewValidatorProbes(probeCredSvc(), f)
			if _, _, _, err := p.ProbePAT(context.Background(), secrets.ActiveRow{OcOrgID: "default"}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	f := aestudiotest.New()
	f.SetIdentity("default", &sourcecontrol.GitHubUser{Login: "acme-bot"})
	login, name, email, err := organization.NewValidatorProbes(probeCredSvc(), f).ProbePAT(context.Background(), secrets.ActiveRow{OcOrgID: "default"})
	if err != nil || login != "acme-bot" || name != "acme-bot" || email != "acme-bot@users.noreply.github.com" {
		t.Fatalf("got %q %q %q err=%v", login, name, email, err)
	}
}
