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
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type countingHooks struct{ calls int }

func (c *countingHooks) Register(context.Context, string, string) (*int64, error) {
	c.calls++
	id := int64(1)
	return &id, nil
}

type credStatus struct {
	p   *organization.Projection
	err error
}

func (c credStatus) Status(context.Context, string) (*organization.Projection, error) {
	return c.p, c.err
}

// The sweep's hook repair registers only for an org whose GitHub credential
// is active: a disconnected or never-connected org reads as absent (passed
// by this tick), so the repair never undoes a disconnect (I-1).
func TestActiveOrgHooks_RegistersOnlyForAnActiveCredential(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		creds credStatus
		want  error
		calls int
	}{
		{"active", credStatus{p: &organization.Projection{Status: organization.CredentialStatusActive}}, nil, 1},
		{"disconnected", credStatus{p: &organization.Projection{Status: "disconnected"}}, sourcecontrol.ErrAEStudioAbsent, 0},
		{"suspended", credStatus{p: &organization.Projection{Status: "suspended"}}, sourcecontrol.ErrAEStudioAbsent, 0},
		{"none", credStatus{err: &organization.NotFoundError{What: "org_credentials.acme"}}, sourcecontrol.ErrAEStudioAbsent, 0},
		{"unreadable", credStatus{err: errors.New("db down")}, sourcecontrol.ErrAEStudioUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hooks := &countingHooks{}
			_, err := activeOrgHooks{hooks: hooks, creds: tc.creds}.Register(context.Background(), "acme", "p")
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) || hooks.calls != tc.calls {
				t.Fatalf("err=%v calls=%d, want %v and %d", err, hooks.calls, tc.want, tc.calls)
			}
		})
	}
}
