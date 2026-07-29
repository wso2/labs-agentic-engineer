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

package edge

import (
	"context"
	"log/slog"
	"net/http"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
)

// RegisterAllDev mounts the dev/test surface (/_dev/v1/*) — local-only tooling
// that is deliberately NOT authenticated and NOT in any OpenAPI spec. Its
// safety is structural, not a token: (1) this registration gate mounts nothing
// unless TestMode && DeploymentTier=="dev" (TEST_MODE defaults false, so the
// surface is ABSENT in every real env — fail-safe, not fail-open), and (2)
// /_dev is on no HTTPRoute, reachable only on the local/loopback interface the
// dev scripts use. Separating it here keeps the gate explicit and the handler
// bodies out of NewHandler.
func RegisterAllDev(mux *http.ServeMux, p AppParams) {
	if !(p.Config.TestMode && p.Config.DeploymentTier == "dev") {
		return // registration gate — the surface does not exist in real envs
	}
	// Secret repair — emits decrypted per-org plaintext, so it keeps an extra
	// explicit opt-in (LOCAL_OPENBAO_REPAIR) on top of the dev gate; the calling
	// script's kubectl-context check is the final net.
	if p.Config.LocalOpenBaoRepairEnabled {
		mux.HandleFunc("POST /_dev/v1/sm-api-resync", devResyncHandler(p))
	}
}

// devResyncHandler walks per-org credential rows and returns the
// {kvPath, property, value} tuples deployments/scripts/repair-secrets.sh needs
// to reseed OpenBao. Plaintext crosses the localhost boundary once per write —
// the repair script then runs `vault kv put` via `kubectl exec` against the
// in-cluster OpenBao to materialise the secret at the path the dispatcher will
// read on the next ExternalSecret sync.
//
// We don't call SM-API directly here because SM-API's auth requires a Thunder
// JWT with an `ouId` claim — only mintable from a user session. For a no-user
// repair flow the BFF would need a per-org impersonation token. The shell→vault
// path bypasses that entirely and matches how setup-aep.sh seeds other local
// secrets. Plaintext is never logged.
func devResyncHandler(params AppParams) http.HandlerFunc {
	type orgResult struct {
		OcOrgID        string                         `json:"ocOrgId"`
		Writes         []organization.SMAPISeedBundle `json:"writes"`
		AnthropicError string                         `json:"anthropicError,omitempty"`
		GitHubPATError string                         `json:"githubPatError,omitempty"`
	}
	type response struct {
		Orgs []orgResult `json:"orgs"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if params.DB == nil || params.CredService == nil || params.AnthropicCredService == nil {
			writeErrorEnvelope(w, http.StatusServiceUnavailable, CodeServiceUnavailable, "resync surface not wired", nil)
			return
		}

		orgIDs, err := collectResyncOrgs(ctx, params.DB, r.URL.Query().Get("org"))
		if err != nil {
			slog.ErrorContext(ctx, "sm-api resync: org list failed", "error", err)
			writeErrorEnvelope(w, http.StatusInternalServerError, CodeInternal, "org list failed", nil)
			return
		}

		out := response{Orgs: make([]orgResult, 0, len(orgIDs))}
		for _, ocOrgID := range orgIDs {
			res := orgResult{OcOrgID: ocOrgID}
			if bundle, err := params.AnthropicCredService.PrepareSMAPISeed(ctx, ocOrgID); err != nil {
				res.AnthropicError = err.Error()
			} else if bundle != nil {
				res.Writes = append(res.Writes, *bundle)
			}
			if bundle, err := params.CredService.PrepareSMAPISeed(ctx, ocOrgID); err != nil {
				res.GitHubPATError = err.Error()
			} else if bundle != nil {
				res.Writes = append(res.Writes, *bundle)
			}
			out.Orgs = append(out.Orgs, res)
			slog.InfoContext(ctx, "sm-api resync: org",
				"ocOrgId", ocOrgID,
				"writeCount", len(res.Writes))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// collectResyncOrgs returns the unique set of ocOrgIDs that have either an
// org_credentials or org_anthropic_credentials row with the SM-API triplet
// populated. When `only` is non-empty the set is filtered to that single id.
func collectResyncOrgs(ctx context.Context, db *gorm.DB, only string) ([]string, error) {
	seen := map[string]struct{}{}
	add := func(rows []string) {
		for _, id := range rows {
			if only != "" && id != only {
				continue
			}
			seen[id] = struct{}{}
		}
	}
	var patOrgs []string
	if err := db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_credentials
		  WHERE secret_ref_name IS NOT NULL OR sm_api_secret_ref_name IS NOT NULL`,
	).Scan(&patOrgs).Error; err != nil {
		return nil, err
	}
	add(patOrgs)
	var anthropicOrgs []string
	if err := db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_anthropic_credentials
		  WHERE secret_ref_name IS NOT NULL OR sm_api_secret_ref_name IS NOT NULL`,
	).Scan(&anthropicOrgs).Error; err != nil {
		return nil, err
	}
	add(anthropicOrgs)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}
