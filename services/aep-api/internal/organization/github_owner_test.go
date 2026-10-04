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

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ownerRows answers GetByOrg from a map; every other method is unused here.
type ownerRows struct {
	organization.OrgCredentialRepository
	rows map[string]*organization.OrgCredential
	err  error
}

func (r ownerRows) GetByOrg(_ context.Context, org string) (*organization.OrgCredential, error) {
	return r.rows[org], r.err
}

func TestGitHubOwner_IsTheConnectedLogin(t *testing.T) {
	rows := ownerRows{rows: map[string]*organization.OrgCredential{"acme": {OcOrgID: "acme", GitHubLogin: "acme-gh"}}}
	svc := organization.NewCredentialService(rows, nil, nil)
	if owner, err := svc.GitHubOwner(context.Background(), "acme"); err != nil || owner != "acme-gh" {
		t.Fatalf("owner=%q err=%v", owner, err)
	}
	if _, err := svc.GitHubOwner(context.Background(), "globex"); !errors.Is(err, sourcecontrol.ErrAEStudioAbsent) {
		t.Fatalf("no row: err = %v, want ErrAEStudioAbsent", err)
	}
	boom := errors.New("db down")
	failing := organization.NewCredentialService(ownerRows{err: boom}, nil, nil)
	if _, err := failing.GitHubOwner(context.Background(), "acme"); !errors.Is(err, boom) || errors.Is(err, sourcecontrol.ErrAEStudioAbsent) {
		t.Fatalf("read failure: err = %v", err)
	}
}
