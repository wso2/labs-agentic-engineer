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
)

// appInstallationCred is the App-mode Credential. Each row in the
// org_credentials table with kind='app-installation' is materialised by
// orgResolver into one of these.
//
// Token() delegates to AppTokenMinter — the only consumer of the App's
// private key. Identity() returns the App's bot identity (loaded once
// at first connect via GET /app, then cached on the minter).
// WebhookStrategy() returns WebhookPlatform: App-mode delivers events
// via the App-wide configured callback URL, not per-repo hooks.
type appInstallationCred struct {
	installationID int64
	accountLogin   string
	identity       Identity
	minter         *AppTokenMinter
}

// Token returns a freshly-minted-or-cached installation token.
func (c *appInstallationCred) Token(ctx context.Context) (string, time.Time, error) {
	if c.minter == nil {
		return "", time.Time{}, ErrAppNotConfigured
	}
	return c.minter.MintForInstallation(ctx, c.installationID)
}

// Identity returns the App's bot identity (e.g. asdlc-bot[bot]).
func (c *appInstallationCred) Identity() Identity { return c.identity }

// RepoOwner returns the GitHub account login the App is installed on
// (resolved at INSERT time via GET /app/installations/{id}).
func (c *appInstallationCred) RepoOwner() string { return c.accountLogin }

// WebhookStrategy returns WebhookPlatform — App mode never registers
// per-repo webhooks; events flow via the App's single configured callback.
func (c *appInstallationCred) WebhookStrategy() WebhookStrategy { return WebhookPlatform }
