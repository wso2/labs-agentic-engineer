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

// gitpat_submit.go — what a gitpat submit (PATCH /config {gitProvider})
// does after Connect: write the PAT's reference, generate the webhook secret
// once, ensure both org clients, and start the AE Studio converge. Nothing
// here waits for the pod: the console polls GET /ae-studio for that.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// StudioConverger starts a converge of the org's AE Studio (ticket 08) and
// returns at once; the converge runs detached from the caller's request.
// Implemented by aestudio.Service.
type StudioConverger interface {
	Trigger(ctx context.Context, org string)
}

// webhookSecretBytes is the entropy of the org's GitHub webhook secret.
const webhookSecretBytes = 32

// WithAEStudio attaches what the gitpat submit needs beyond Connect: the org
// secret writer (the webhook secret) and the AE Studio converger. Either
// may be nil: without the writer (secrets delivery off) the submit stops
// after the PAT, without the converger it ensures the clients and logs
// ae_studio_not_configured instead of converging.
func (s *Service) WithAEStudio(orgSecrets *OrgSecretWriter, converger StudioConverger) *Service {
	s.orgSecrets = orgSecrets
	s.converger = converger
	return s
}

// submitGitPAT runs the gitpat submit after Connect committed (and after the
// patch's other sections). Every vault path here derives from the request's
// ouId (vaultOUOf); the clients' Thunder OU is the org row's. Each step
// needs the one before it, so the first failure fails the section; a
// resubmit repeats the sequence, and every step is idempotent (the webhook
// secret is kept, present clients are left alone).
func (s *Service) submitGitPAT(ctx context.Context, org, pat string) error {
	if err := s.credentialSvc.WritePATRef(ctx, org, pat); err != nil {
		return submitFailure(ctx, org, "github-pat", err)
	}
	if s.orgSecrets == nil || s.idpSvc == nil {
		slog.ErrorContext(ctx, "ae_studio_not_configured", "org", org, "reason", "secrets delivery is off")
		return nil
	}
	if err := s.ensureWebhookSecret(ctx, org); err != nil {
		return submitFailure(ctx, org, "github-webhook-secret", err)
	}
	for _, kind := range []ClientKind{ClientPublisher, ClientStudio} {
		err := s.idpSvc.EnsureClient(ctx, org, kind)
		if errors.Is(err, ErrIDPThunderUnavailable) {
			slog.ErrorContext(ctx, "ae_studio_not_configured", "org", org, "reason", "thunder admin client not configured")
			return nil
		}
		if err != nil {
			return submitFailure(ctx, org, "client:"+string(kind), err)
		}
	}
	if s.converger == nil {
		slog.ErrorContext(ctx, "ae_studio_not_configured", "org", org, "reason", "no AE Studio converger")
		return nil
	}
	s.converger.Trigger(ctx, org)
	return nil
}

// ensureWebhookSecret generates the org's GitHub webhook secret (32 random
// bytes, hex) at the first submit and keeps it on every later one (06 §6).
// "First" is decided under the secret's lock, so two concurrent first
// submits store one secret.
func (s *Service) ensureWebhookSecret(ctx context.Context, org string) error {
	ouID, err := vaultOUOf(ctx)
	if err != nil {
		return err
	}
	value, err := generateRandomHex(webhookSecretBytes)
	if err != nil {
		return err
	}
	_, err = s.orgSecrets.WriteIfUnset(ctx, org, ouID, OrgSecretGitHubWebhookSecret, map[string]string{"secret": value})
	return err
}

// AEStudioSetupIncompleteCode is the gitProvider section error code of a
// submit whose GitHub connection was saved but whose AE Studio setup (the
// token's reference, the webhook secret, the org clients) did not finish.
// Saving the token again retries the setup, except after an OU error (the
// org's Thunder OU missing, or not the caller's), which an operator must
// fix first; that message says so (submitFailure).
const AEStudioSetupIncompleteCode = "ae_studio_setup_incomplete"

// submitFailure logs a failed setup step (value-free) and returns the
// gitProvider section error the console shows. Connect has committed by
// then, so every message says the connection was saved. A concurrent write
// of the same secret is a retryable 409. A foreign-OU client and an org
// whose Thunder OU is missing or disagrees with the caller's are 409s that a
// retry cannot fix, so their messages name the operator action instead. Any
// other failure is a 502.
func submitFailure(ctx context.Context, org, step string, err error) error {
	slog.ErrorContext(ctx, "ae_studio.gitpat_submit_failed", "org", org, "step", step, "error", err)
	const saved = "The GitHub connection was saved, but AE Studio setup didn't finish: "
	switch {
	case errors.Is(err, ErrOrgSecretConflict):
		return &SectionError{Section: "gitProvider", Status: http.StatusConflict, Code: AEStudioSetupIncompleteCode,
			Message: saved + "another save of this organization's secrets was in progress. Save the token again to retry."}
	case errors.Is(err, thundersvc.ErrAppInForeignOU):
		return &SectionError{Section: "gitProvider", Status: http.StatusConflict, Code: AEStudioSetupIncompleteCode,
			Message: saved + "an AE Studio client with this organization's name belongs to another organization; an operator must remove it."}
	case errors.Is(err, errOrgOUUnknown):
		return &SectionError{Section: "gitProvider", Status: http.StatusConflict, Code: AEStudioSetupIncompleteCode,
			Message: saved + "this organization has no identity-provider organization unit recorded; an operator must link it before AE Studio can be set up."}
	case errors.Is(err, errOrgOUMismatch):
		return &SectionError{Section: "gitProvider", Status: http.StatusConflict, Code: AEStudioSetupIncompleteCode,
			Message: saved + "your sign-in belongs to a different identity-provider organization unit than this organization's record; an operator must reconcile them."}
	default:
		return &SectionError{Section: "gitProvider", Status: http.StatusBadGateway, Code: AEStudioSetupIncompleteCode,
			Message: saved + "save the token again to retry."}
	}
}
