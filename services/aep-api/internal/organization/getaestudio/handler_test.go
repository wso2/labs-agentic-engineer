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

package getaestudio

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

type fakeStatus struct {
	st  organization.AEStudioStatus
	err error
	org *string
}

func (f fakeStatus) Status(_ context.Context, org string) (organization.AEStudioStatus, error) {
	if f.org != nil {
		*f.org = org
	}
	return f.st, f.err
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGetAeStudio(t *testing.T) {
	cases := []struct {
		st   organization.AEStudioStatus
		want string
	}{
		{organization.AEStudioStatus{State: organization.AEStudioAbsent}, `{"state":"absent"}`},
		{organization.AEStudioStatus{State: organization.AEStudioProvisioning}, `{"state":"provisioning"}`},
		{organization.AEStudioStatus{State: organization.AEStudioReady, URLs: &organization.AEStudioURLs{DesignAgent: "http://d", Collab: "ws://c", Tools: "http://t"}},
			`{"state":"ready","urls":{"collab":"ws://c","designAgent":"http://d","tools":"http://t"}}`},
	}
	for _, c := range cases {
		var org string
		h := New(fakeStatus{st: c.st, org: &org})
		resp, err := h.GetAeStudio(tenant.WithBoundOrg(context.Background(), "default"), gen.GetAeStudioRequestObject{})
		if err != nil {
			t.Fatal(err)
		}
		if got := mustJSON(t, resp); got != c.want {
			t.Fatalf("got %s want %s", got, c.want)
		}
		if org != "default" {
			t.Fatalf("status read for org %q, want the bound org", org)
		}
	}
}

func TestGetAeStudio_NotConfiguredIsFailed(t *testing.T) {
	h := New(fakeStatus{st: organization.AEStudioStatus{State: organization.AEStudioFailed}})
	resp, err := h.GetAeStudio(tenant.WithBoundOrg(context.Background(), "default"), gen.GetAeStudioRequestObject{})
	if err != nil || mustJSON(t, resp) != `{"state":"failed"}` {
		t.Fatalf("%v %v", resp, err)
	}
}

func TestGetAeStudio_ReadErrorIs500WithFixedMessage(t *testing.T) {
	h := New(fakeStatus{err: errors.New("oc down: secret detail")})
	_, err := h.GetAeStudio(tenant.WithBoundOrg(context.Background(), "default"), gen.GetAeStudioRequestObject{})
	if err == nil || err.Error() != "failed to read AE Studio state" {
		t.Fatalf("got %v", err)
	}
}
