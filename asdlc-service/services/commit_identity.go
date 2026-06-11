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

package services

import (
	"context"
	"log/slog"
)

// commitIdentity returns the (authorName, authorEmail) tuple to use for
// platform-driven git commits (spec save, design save, etc.) under
// orgID.
//
// Phase 2 PR B: identity now comes from the org's active credential
// record — App mode returns the bot identity (asdlc-platform[bot]);
// PAT mode returns the PAT owner. The fallback covers newly-connected
// orgs with empty cache and transient lookup hiccups.
func commitIdentity(ctx context.Context, credentialSvc *CredentialService, orgID string) (name, email string) {
	const fallbackName = "ASDLC Bot"
	const fallbackEmail = "bot@asdlc.dev"

	if credentialSvc == nil || orgID == "" {
		return fallbackName, fallbackEmail
	}
	ident, err := credentialSvc.IdentityFor(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "commit identity lookup failed; falling back to default",
			"orgId", orgID, "error", err)
		return fallbackName, fallbackEmail
	}
	if ident == nil || ident.Name == "" {
		return fallbackName, fallbackEmail
	}
	return ident.Name, ident.Email
}
