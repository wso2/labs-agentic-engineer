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
	"errors"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ValidatorProbes is the production secrets.ValidatorProbes: the org's
// GitHub identity read through its AE Studio pod, and the
// CredentialService's identity-update helpers.
type ValidatorProbes struct {
	credSvc  *CredentialService
	identity sourcecontrol.IdentityOps
}

// NewValidatorProbes constructs the probes adapter. Both dependencies must
// be non-nil.
func NewValidatorProbes(credSvc *CredentialService, identity sourcecontrol.IdentityOps) *ValidatorProbes {
	return &ValidatorProbes{credSvc: credSvc, identity: identity}
}

// ListActiveRows projects org_credentials rows into the validator's
// schema-free shape.
func (p *ValidatorProbes) ListActiveRows(ctx context.Context) ([]secrets.ActiveRow, error) {
	rows, err := p.credSvc.ListActiveRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]secrets.ActiveRow, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, secrets.ActiveRow{
			OcOrgID:       r.OcOrgID,
			Kind:          r.Kind,
			GitHubLogin:   r.GitHubLogin,
			IdentityLogin: r.IdentityLogin,
			Status:        r.Status,
		})
	}
	return out, nil
}

// ProbePAT reads the GitHub user the org's gitpat belongs to through the
// org's pod (get-github-identity) and translates the answer into the
// validator's signal vocabulary: GitHub refusing the token (401/403/404)
// is ErrCredentialUnauthorized and triggers the cascade; anything else —
// the pod absent, unavailable or refusing aep-api's AE-only token, a rate
// limit, a GitHub 5xx — is ErrCredentialTransient and skips the tick. The
// AE-only token is not the user's PAT, so a refusal of it says nothing
// about the PAT (C3).
func (p *ValidatorProbes) ProbePAT(ctx context.Context, row secrets.ActiveRow) (login, name, email string, err error) {
	user, err := p.identity.GitHubIdentity(ctx, row.OcOrgID)
	if err != nil {
		if gitHubRefusedToken(err) {
			return "", "", "", secrets.ErrCredentialUnauthorized
		}
		return "", "", "", errors.Join(secrets.ErrCredentialTransient, err)
	}
	if user.Email == "" {
		user.Email = user.Login + "@users.noreply.github.com"
	}
	if user.Name == "" {
		user.Name = user.Login
	}
	return user.Login, user.Name, user.Email, nil
}

// gitHubRefusedToken reports whether GitHub itself refused the org's token
// (the pod's github_error with GitHub's status).
func gitHubRefusedToken(err error) bool {
	return sourcecontrol.IsHTTPStatus(err, 401) || sourcecontrol.IsHTTPStatus(err, 403) || sourcecontrol.IsHTTPStatus(err, 404)
}

// RecordIdentityFromGitHub delegates to the credential service so the
// drift columns are written under the row's database connection.
func (p *ValidatorProbes) RecordIdentityFromGitHub(ctx context.Context, ocOrgID, login, name, email string) (bool, error) {
	return p.credSvc.RecordIdentityFromGitHub(ctx, ocOrgID, login, name, email)
}
