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

// org_github_controller.go — phase 2's org-scoped GitHub integration surface.
//
// Routes:
//
//	POST   /api/v1/github/connect/start  — start App connect (OAuth-driven)
//	GET    /api/v1/org/credentials/github/connect/callback?...     — App OAuth + post-install callback (unscoped)
//	POST   /api/v1/github/pat            — PAT-mode connect / replace
//	GET    /api/v1/github                — projection (no token)
//	DELETE /api/v1/github                — disconnect cascade
//
// Architecture: connect is fully binding-centric. Every App-mode connect
// goes through GitHub OAuth first; the callback intersects /user/installations
// with our App's installs and binds only what the requesting user actually
// administers. There is no platform-wide "discover unbound installs"
// surface — that would leak install metadata across tenants.
package organization

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/httpkit"
)

// OrgGitHubController handles the per-org GitHub connect callback surface.
// The connect/start, pat, status, and disconnect operations are now
// code-first Huma (orggithub_huma.go); only the unscoped OAuth/post-install
// callback remains a raw handler here.
type OrgGitHubController interface {
	HandleConnectCallback(w http.ResponseWriter, r *http.Request)
}

type orgGitHubController struct {
	credentialSvc *CredentialService
	disconnectSv  *OrgDisconnectService
	bearerSvc     *BearerService
	appSlug       string
	publicURL     string // for the post-callback redirect
	// appClientID is the GitHub App's OAuth client_id used to build the
	// authorize URL. Empty disables the App-mode connect path (StartConnect 503).
	appClientID string
}

// NewOrgGitHubController constructs the controller. publicURL is the
// user-facing base URL (BFF_PUBLIC_URL): the console's origin, which
// reverse-proxies the API, so both the GitHub callback and the settings page
// it returns to are built on it. appClientID is the GitHub App's OAuth
// client_id; empty disables App-mode connect.
func NewOrgGitHubController(
	credentialSvc *CredentialService,
	disconnectSv *OrgDisconnectService,
	bearerSvc *BearerService,
	appSlug, publicURL, appClientID string,
) OrgGitHubController {
	if appSlug == "" {
		appSlug = "aep-platform"
	}
	if publicURL == "" {
		publicURL = "http://localhost:8090"
	}
	return &orgGitHubController{
		credentialSvc: credentialSvc,
		disconnectSv:  disconnectSv,
		bearerSvc:     bearerSvc,
		appSlug:       appSlug,
		publicURL:     publicURL,
		appClientID:   appClientID,
	}
}

// consoleCredentialsPath is the console's Settings → Credentials page
// (apps/console/src/routes/settings.credentials.tsx), where the callback lands
// the user with its outcome in the query (`error`, `connected`, `candidates`).
// The console's routes carry no org: it reads the org from the user's token.
const consoleCredentialsPath = "/settings/credentials"

// ConnectCallbackPath is the single GitHub-side callback URL configured
// in both the App's "Setup URL" and "Callback URL" fields. Constant so
// the OAuth authorize URL and the redirect_uri in the code exchange
// always match (GitHub enforces exact equality).
//
// It is deliberately NOT renamed by the org-config consolidation: the callback
// is authed by a signed connect-state JWT on the OUTER mux (not the user-JWT
// /config surface), so it stays put while the org-scoped routes drop /org. The
// new POST /config/git-provider/connect-sessions handler references it so the
// authorize URL's redirect_uri still points here (org-config-consolidation.md
// §2). Exported so that handler, in the sibling orgconfig package, can build
// the same redirect_uri.
const ConnectCallbackPath = httpkit.APIV1 + "/org/credentials/github/connect/callback"

// HandleConnectCallback is the single callback for every App-mode connect
// roundtrip. Three shapes arrive at this endpoint:
//
//   - ?code present → OAuth callback. Resolve user's installs, then either
//     bind directly (1 candidate), redirect to install flow (0 candidates),
//     or send to the picker (2+ candidates). When the state JWT pinned an
//     installationID (picker re-OAuth), verify it's in the candidates and
//     bind it.
//   - ?installation_id present → post-install callback. The user just
//     installed the App on a GitHub org via the install flow; bind directly
//     (GitHub enforces installer-is-admin).
//   - neither → invalid; redirect with an error.
func (c *orgGitHubController) HandleConnectCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	claims, err := c.bearerSvc.VerifyConnectState(state)
	if err != nil {
		slog.WarnContext(r.Context(), "connect callback: invalid state", "error", err)
		http.Error(w, "invalid state: "+err.Error(), http.StatusBadRequest)
		return
	}
	settingsURL := c.publicURL + consoleCredentialsPath

	if code := q.Get("code"); code != "" {
		c.handleOAuthCallback(w, r, claims, code, settingsURL)
		return
	}
	if idStr := q.Get("installation_id"); idStr != "" {
		c.handlePostInstallCallback(w, r, claims, idStr, settingsURL)
		return
	}
	http.Redirect(w, r, settingsURL+"?error=callback_invalid", http.StatusSeeOther)
}

// handleOAuthCallback resolves the user's installs and routes by candidate
// count. When claims.InstallationID is non-zero (picker re-OAuth), verifies
// the pinned install is in the candidates before binding.
func (c *orgGitHubController) handleOAuthCallback(w http.ResponseWriter, r *http.Request, claims *ConnectStateClaims, code, settingsURL string) {
	redirectURI := c.publicURL + ConnectCallbackPath
	candidates, err := c.credentialSvc.ResolveUserInstallations(r.Context(), claims.OcOrgID, code, redirectURI)
	if err != nil {
		if errors.Is(err, ErrAppBindNotConfigured) {
			http.Redirect(w, r, settingsURL+"?error=app_bind_not_configured", http.StatusSeeOther)
			return
		}
		slog.ErrorContext(r.Context(), "connect callback: resolve user installations failed",
			"error", err, "ocOrgId", claims.OcOrgID)
		http.Redirect(w, r, settingsURL+"?error=connect_failed", http.StatusSeeOther)
		return
	}

	// Picker re-OAuth — state pinned a specific install. Verify it's in
	// the candidates (i.e. user actually administers it) and bind.
	if claims.InstallationID > 0 {
		for _, cand := range candidates {
			if cand.InstallationID == claims.InstallationID {
				c.bindAndRedirect(w, r, claims, claims.InstallationID, settingsURL)
				return
			}
		}
		slog.InfoContext(r.Context(), "connect callback: pinned install not in user's installations",
			"ocOrgId", claims.OcOrgID, "installationId", claims.InstallationID, "actor", claims.Actor)
		http.Redirect(w, r, settingsURL+"?error=oauth_unauthorized", http.StatusSeeOther)
		return
	}

	switch len(candidates) {
	case 0:
		// User has no install of our App they admin. Send them to the
		// install flow with a fresh state JWT (installationID still 0).
		state, err := c.bearerSvc.IssueConnectState(claims.OcOrgID, claims.Actor, 0, 15*time.Minute)
		if err != nil {
			slog.ErrorContext(r.Context(), "connect callback: re-issue state failed", "error", err)
			http.Redirect(w, r, settingsURL+"?error=connect_failed", http.StatusSeeOther)
			return
		}
		installURL := "https://github.com/apps/" + c.appSlug + "/installations/new?state=" + url.QueryEscape(state)
		http.Redirect(w, r, installURL, http.StatusSeeOther)
	case 1:
		c.bindAndRedirect(w, r, claims, candidates[0].InstallationID, settingsURL)
	default:
		// Picker. Encode the candidates in the URL so the credentials page can
		// offer them without another round-trip; the user picks one and
		// re-enters StartConnect with installationId pinned.
		raw, err := json.Marshal(candidates)
		if err != nil {
			slog.ErrorContext(r.Context(), "connect callback: marshal candidates failed", "error", err)
			http.Redirect(w, r, settingsURL+"?error=connect_failed", http.StatusSeeOther)
			return
		}
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		http.Redirect(w, r, settingsURL+"?candidates="+encoded, http.StatusSeeOther)
	}
}

// handlePostInstallCallback handles the redirect after the user installed
// the App via the github.com install flow. We trust GitHub's enforcement
// that "only admins can install Apps" and bind directly without an OAuth
// re-check. Same trust assumption the original install-callback path used.
func (c *orgGitHubController) handlePostInstallCallback(w http.ResponseWriter, r *http.Request, claims *ConnectStateClaims, idStr, settingsURL string) {
	installID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Redirect(w, r, settingsURL+"?error=callback_invalid", http.StatusSeeOther)
		return
	}
	c.bindAndRedirect(w, r, claims, installID, settingsURL)
}

// bindAndRedirect calls CreateOrReplaceCredential to insert the platform
// row for the installation, then 302s to the settings page.
func (c *orgGitHubController) bindAndRedirect(w http.ResponseWriter, r *http.Request, claims *ConnectStateClaims, installID int64, settingsURL string) {
	_, err := c.credentialSvc.Connect(r.Context(), claims.OcOrgID, ConnectRequest{
		Kind:           "app-installation",
		InstallationID: installID,
	})
	if err != nil {
		var ce *ConflictError
		var ve *ValidationError
		switch {
		case errors.As(err, &ce):
			http.Redirect(w, r, settingsURL+"?error=cross_mode", http.StatusSeeOther)
		case errors.As(err, &ve) && ve.Code != "":
			// Validation failure with a structured code — pass the code
			// through so the console can map it to a friendly message.
			slog.InfoContext(r.Context(), "connect callback: bind refused",
				"ocOrgId", claims.OcOrgID, "installationId", installID, "code", ve.Code, "msg", ve.Error())
			http.Redirect(w, r, settingsURL+"?error="+url.QueryEscape(ve.Code), http.StatusSeeOther)
		default:
			slog.ErrorContext(r.Context(), "connect callback: bind failed",
				"ocOrgId", claims.OcOrgID, "installationId", installID, "error", err)
			http.Redirect(w, r, settingsURL+"?error=connect_failed", http.StatusSeeOther)
		}
		return
	}
	slog.InfoContext(r.Context(), "connect callback: connected",
		"ocOrgId", claims.OcOrgID, "installationId", installID, "actor", claims.Actor)
	http.Redirect(w, r, settingsURL+"?connected=app", http.StatusSeeOther)
}
