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

// TEMPORARY (phase 3 deletes): old agents joins the pod Room.

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
	"github.com/wso2/aep/aep-api/internal/spec"
)

type fakeStudioStatus struct {
	st  organization.AEStudioStatus
	err error
	org *string
}

func (f fakeStudioStatus) Status(_ context.Context, org string) (organization.AEStudioStatus, error) {
	if f.org != nil {
		*f.org = org
	}
	return f.st, f.err
}

func TestAEStudioRooms_ReadyAnswersThePublicRoomsURL(t *testing.T) {
	var asked string
	l := aeStudioRooms{status: fakeStudioStatus{org: &asked, st: organization.AEStudioStatus{
		State: organization.AEStudioReady,
		URLs:  &organization.AEStudioURLs{Collab: "ws://ae-collab-x.openchoreoapis.localhost:19080", DesignAgent: "d", Tools: "t"},
	}}}
	got, err := l.RoomURL(t.Context(), "acme")
	if err != nil || got != "ws://ae-collab-x.openchoreoapis.localhost:19080/v1/rooms" {
		t.Fatalf("RoomURL = %q, %v", got, err)
	}
	if asked != "acme" {
		t.Fatalf("status read for org %q, want acme", asked)
	}
}

func TestAEStudioRooms_NotReadyStatesMapToSentinels(t *testing.T) {
	cases := []struct {
		name string
		st   organization.AEStudioStatus
		err  error
		want error
	}{
		{"absent", organization.AEStudioStatus{State: organization.AEStudioAbsent}, nil, spec.ErrAEStudioAbsent},
		{"provisioning", organization.AEStudioStatus{State: organization.AEStudioProvisioning}, nil, spec.ErrAEStudioUnavailable},
		{"failed", organization.AEStudioStatus{State: organization.AEStudioFailed}, nil, spec.ErrAEStudioUnavailable},
		{"ready without urls", organization.AEStudioStatus{State: organization.AEStudioReady}, nil, spec.ErrAEStudioUnavailable},
		{"status read error", organization.AEStudioStatus{}, errors.New("oc down"), spec.ErrAEStudioUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := aeStudioRooms{status: fakeStudioStatus{st: c.st, err: c.err}}.RoomURL(t.Context(), "acme")
			if !errors.Is(err, c.want) || got != "" {
				t.Fatalf("RoomURL = %q, %v; want %v", got, err, c.want)
			}
		})
	}
}

// Q-47: an install without AE_STUDIO_* still boots, and the real Status then
// answers failed for an org with a GitHub token; the locator turns that into
// unavailable rather than panicking.
func TestAEStudioRooms_NotConfiguredIsUnavailable(t *testing.T) {
	svc := aestudio.New(aestudio.Deps{Config: config.AEStudioConfig{}, OrgSecrets: gitPATOnly{}})
	if _, err := (aeStudioRooms{status: svc}).RoomURL(t.Context(), "acme"); !errors.Is(err, spec.ErrAEStudioUnavailable) {
		t.Fatalf("RoomURL on an unconfigured install = %v, want ErrAEStudioUnavailable", err)
	}
}

// gitPATOnly lists one org secret: the GitHub token.
type gitPATOnly struct{}

func (gitPATOnly) List(context.Context, string) ([]organization.OrgSecretRef, error) {
	return []organization.OrgSecretRef{{Secret: organization.OrgSecretGitHubPAT, Name: "github-pat"}}, nil
}
