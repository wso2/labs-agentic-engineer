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

// devResyncRoute is the dev/test route group (/_dev/v1/*): local-only tooling
// that is deliberately NOT authenticated and NOT in any OpenAPI spec. Its
// safety is structural, not a token: (1) it returns nil, so nothing mounts,
// unless TestMode && DeploymentTier=="dev" (TEST_MODE defaults false, so the
// group is ABSENT in every real env: fail-safe, not fail-open), (2) secret
// repair keeps an extra explicit opt-in (LOCAL_OPENBAO_REPAIR), and (3) /_dev
// is on no HTTPRoute, reachable only on the loopback interface the dev scripts
// use.
func devResyncRoute(p AppParams) http.Handler {
	if !(p.Config.TestMode && p.Config.DeploymentTier == "dev" && p.Config.LocalOpenBaoRepairEnabled) {
		return nil
	}
	return devResyncHandler(p)
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
		ModelError     string `json:"modelError,omitempty"`
		GitHubPATError string `json:"githubPatError,omitempty"`
	}
	type response struct {
		Orgs []orgResult `json:"orgs"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if params.DB == nil || params.CredService == nil || params.AnthropicCredService == nil || params.ModelConnections == nil {
			writeErrorEnvelope(w, http.StatusServiceUnavailable, CodeServiceUnavailable, "resync route not wired", nil)
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
				res.ModelError = ouErr.Error()
				res.GitHubPATError = ouErr.Error()
				out.Orgs = append(out.Orgs, res)
				continue
			}
			if ouID == "" {
				msg := "no thunder_org_uuid for org — cannot derive vault path"
				res.ModelError = msg
				res.GitHubPATError = msg
				out.Orgs = append(out.Orgs, res)
				continue
			}
			orgCtx := jwtassertion.ContextWithTokenClaims(ctx, &jwtassertion.TokenClaims{OuId: ouID})

			// The connection key (under its entity, model-connection) and the
			// Claude subscription: a repair that restored only one would leave
			// a run mounting a path that no longer resolves.
			for _, resync := range []func(context.Context, string) (bool, error){
				params.ModelConnections.ResyncSecretRef, params.AnthropicCredService.ResyncSecretRef,
			} {
				if wrote, err := resync(orgCtx, ocOrgID); err != nil {
					res.ModelError = err.Error()
				} else if wrote {
					res.Written++
				}
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
// org_credentials, org_model_connections or org_anthropic_credentials row with a secret-ref triplet
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
	var modelOrgs []string
	if err := db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_model_connections WHERE secret_ref_name IS NOT NULL
		 UNION
		 SELECT oc_org_id FROM org_anthropic_credentials WHERE secret_ref_name IS NOT NULL`,
	).Scan(&modelOrgs).Error; err != nil {
		return nil, err
	}
	add(modelOrgs)
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
