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

package seed

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// AppPlatformFromEnv formerly wrote the GitHub App's appID, clientID,
// private key, and webhook secret into an OpenBao _platform namespace when
// DeploymentTier=dev. That OpenBao-backed store was deleted (issue #263);
// the wired CredentialStore is Postgres and has no platform namespace.
//
// Kept as a no-op so Resolve's call site stays stable. App-mode connect
// remains unavailable until a platform-capable seed path exists. Validates
// the env shape in-dev so operators still get early feedback on a bad PEM.
func AppPlatformFromEnv(ctx context.Context, store secrets.CredentialStore, cfg config.Config) error {
	_ = ctx
	_ = store

	if cfg.DeploymentTier != "dev" {
		slog.Info("app-platform seed: skipped (DeploymentTier != dev)", "tier", cfg.DeploymentTier)
		return nil
	}
	if cfg.GitHubAppID == "" || cfg.GitHubAppPrivateKeyPath == "" {
		slog.Info("app-platform seed: skipped (no GITHUB_APP_ID or GITHUB_APP_PRIVATE_KEY_PATH set)",
			"appIdSet", cfg.GitHubAppID != "",
			"keyPathSet", cfg.GitHubAppPrivateKeyPath != "")
		return nil
	}

	pemBytes, err := os.ReadFile(cfg.GitHubAppPrivateKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("app-platform seed: private key file not found; App-mode connect will be unavailable",
				"path", cfg.GitHubAppPrivateKeyPath,
				"hint", "download from github.com/settings/apps/aep-platform → 'Generate a private key' and drop at this path")
			return nil
		}
		return fmt.Errorf("app-platform seed: read %s: %w", cfg.GitHubAppPrivateKeyPath, err)
	}
	if len(pemBytes) == 0 {
		slog.Warn("app-platform seed: private key file is empty; App-mode connect will be unavailable", "path", cfg.GitHubAppPrivateKeyPath)
		return nil
	}
	if !looksLikePEM(pemBytes) {
		return fmt.Errorf("app-platform seed: %s is %d bytes but does not contain a PEM-encoded RSA key (drop the .pem you downloaded from GitHub App settings → 'Generate a private key')", cfg.GitHubAppPrivateKeyPath, len(pemBytes))
	}

	slog.Warn("app-platform seed: CredentialStore has no platform namespace; skipping write",
		"appId", cfg.GitHubAppID,
		"hint", "Postgres CredentialStore cannot hold GitHub App platform material; App-mode remains unavailable")
	return nil
}

// looksLikePEM does a cheap shape check on the file before we try to
// write it. Catches the common "user copied wrong file" mistake during
// the connect-flow operator runbook.
func looksLikePEM(b []byte) bool {
	s := string(b)
	return strings.Contains(s, "-----BEGIN") && strings.Contains(s, "PRIVATE KEY")
}
