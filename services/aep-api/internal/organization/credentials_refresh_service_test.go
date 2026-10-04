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

package organization

import (
	"context"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// fakeResolver dispatches a fixed Credential or returns a fixed error.
type fakeResolver struct {
	cred secrets.Credential
	err  error
}

func (f *fakeResolver) Resolve(ctx context.Context, ocOrgID string) (secrets.Credential, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cred, nil
}

// fakeCred returns a constant token + expiry.
type fakeCred struct {
	token string
	exp   time.Time
	err   error
}

func (c *fakeCred) Token(context.Context) (string, time.Time, error) {
	return c.token, c.exp, c.err
}
func (c *fakeCred) Identity() secrets.Identity { return secrets.Identity{} }
func (c *fakeCred) RepoOwner() string          { return "" }
func (c *fakeCred) WebhookStrategy() secrets.WebhookStrategy {
	return secrets.WebhookPerRepo
}

func TestRefresh_Happy(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	res := &fakeResolver{cred: &fakeCred{token: "ghs_abc", exp: expiry}}

	svc := NewCredentialsRefreshService(res)
	resp, err := svc.Refresh(context.Background(), "task-1", "default")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if resp.Token != "ghs_abc" {
		t.Errorf("token = %q; want ghs_abc", resp.Token)
	}
	if resp.TaskID != "task-1" {
		t.Errorf("taskId echo = %q; want task-1", resp.TaskID)
	}
}

// Ensure fakeCred matches the secrets.Credential interface.
var _ secrets.Credential = (*fakeCred)(nil)
