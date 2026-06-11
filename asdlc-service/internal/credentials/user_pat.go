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

package credentials

import (
	"context"
	"time"

	"golang.org/x/sync/singleflight"
)

// userPATCred is the User-PAT-mode Credential. Each row in
// org_credentials with kind='user-pat' is materialised by orgResolver
// into one of these.
//
// Token() reads from OpenBao at secret/asdlc/{ocOrgID}/github/pat with
// singleflight collapsing concurrent reads. There is NO plaintext cache
// — phase2.md §1.13 / §6.2 explicitly drops the 30-min cache the
// evolution doc speculated about. The security trade (process-memory
// retention window) was undesirable; OpenBao reads are sub-10ms and
// bursts are absorbed by singleflight. Reachability is now an
// architectural property via the startup gate, not a runbook discipline.
type userPATCred struct {
	ocOrgID     string
	githubLogin string
	identity    Identity
	store       OpenBaoStore
	flight      *singleflight.Group
}

// Token returns the stored PAT. ExpiresAt is zero (long-lived) — the
// workspace credential helper treats zero as "no refresh needed".
func (c *userPATCred) Token(ctx context.Context) (string, time.Time, error) {
	v, err, _ := c.flight.Do(c.ocOrgID, func() (interface{}, error) {
		return c.store.Get(ctx, c.ocOrgID, "github/pat")
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return string(v.([]byte)), time.Time{}, nil
}

// Identity returns the PAT owner's identity (resolved via GET /user at
// connect time, refreshed on PAT replace).
func (c *userPATCred) Identity() Identity { return c.identity }

// RepoOwner returns the GitHub org/user login chosen at connect time.
// Decoupled from ocOrgID — the OC org slug and the GitHub org slug can
// differ.
func (c *userPATCred) RepoOwner() string { return c.githubLogin }

// WebhookStrategy returns WebhookPerRepo — PAT mode registers a webhook
// on each repo at provision time using the org's webhook secret list.
func (c *userPATCred) WebhookStrategy() WebhookStrategy { return WebhookPerRepo }
