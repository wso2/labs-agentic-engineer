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

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
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
	// Secret repair — re-pushes through the in-process SecretRefWriter; keeps an
	// extra explicit opt-in (LOCAL_OPENBAO_REPAIR) on top of the dev gate.
	if p.Config.LocalOpenBaoRepairEnabled {
		mux.HandleFunc("POST /_dev/v1/secret-ref-resync", devResyncHandler(p))
	}
}

// devResyncHandler walks per-org credential rows and re-pushes secrets through
// the in-process SecretRefWriter / secrets provider. No decrypted plaintext
// leaves the process — the HTTP response carries only status counts/errors.
//
// ouId claims are injected from organizations.thunder_org_uuid so
// SecretRefWriter.resolveVaultKey can stamp paths without a user JWT (this
// endpoint is unauthenticated by design).
func devResyncHandler(params AppParams) http.HandlerFunc {
	type orgResult struct {
		OcOrgID        string `json:"ocOrgId"`
		Written        int    `json:"written"`
		AnthropicError string `json:"anthropicError,omitempty"`
		GitHubPATError string `json:"githubPatError,omitempty"`
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
			slog.ErrorContext(ctx, "secret resync: org list failed", "error", err)
			writeErrorEnvelope(w, http.StatusInternalServerError, CodeInternal, "org list failed", nil)
			return
		}

		out := response{Orgs: make([]orgResult, 0, len(orgIDs))}
		for _, ocOrgID := range orgIDs {
			res := orgResult{OcOrgID: ocOrgID}
			ouID, ouErr := lookupThunderOrgUUID(ctx, params.DB, ocOrgID)
			if ouErr != nil {
				res.AnthropicError = ouErr.Error()
				res.GitHubPATError = ouErr.Error()
				out.Orgs = append(out.Orgs, res)
				continue
			}
			if ouID == "" {
				msg := "no thunder_org_uuid for org — cannot derive vault path"
				res.AnthropicError = msg
				res.GitHubPATError = msg
				out.Orgs = append(out.Orgs, res)
				continue
			}
			orgCtx := jwtassertion.ContextWithTokenClaims(ctx, &jwtassertion.TokenClaims{OuId: ouID})

			if wrote, err := params.AnthropicCredService.ResyncSecretRef(orgCtx, ocOrgID); err != nil {
				res.AnthropicError = err.Error()
			} else if wrote {
				res.Written++
			}
			if wrote, err := params.CredService.ResyncSecretRef(orgCtx, ocOrgID); err != nil {
				res.GitHubPATError = err.Error()
			} else if wrote {
				res.Written++
			}
			out.Orgs = append(out.Orgs, res)
			slog.InfoContext(ctx, "secret resync: org",
				"ocOrgId", ocOrgID,
				"written", res.Written)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// collectResyncOrgs returns the unique set of ocOrgIDs that have either an
// org_credentials or org_anthropic_credentials row with a secret-ref triplet
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
		  WHERE secret_ref_name IS NOT NULL`,
	).Scan(&patOrgs).Error; err != nil {
		return nil, err
	}
	add(patOrgs)
	var anthropicOrgs []string
	if err := db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_anthropic_credentials
		  WHERE secret_ref_name IS NOT NULL`,
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

func lookupThunderOrgUUID(ctx context.Context, db *gorm.DB, ocOrgID string) (string, error) {
	var ouID *string
	err := db.WithContext(ctx).Raw(
		`SELECT thunder_org_uuid::text FROM organizations WHERE name = ?`, ocOrgID,
	).Scan(&ouID).Error
	if err != nil {
		return "", err
	}
	if ouID == nil {
		return "", nil
	}
	return *ouID, nil
}
