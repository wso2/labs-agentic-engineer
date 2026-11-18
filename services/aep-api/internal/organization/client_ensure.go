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

// client_ensure.go — EnsureClient: the org's two clients on the platform IdP,
// aep-publisher-<org> and ae-studio-<org>, and their secrets in the org's
// vault references. Both clients are created before aep-api runs (OrgClients),
// so an ensure calls no IdP: it writes the client's secret as a new reference
// and records the client's ids on the profile.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// ClientKind names one of the org's two clients.
type ClientKind string

const (
	// ClientPublisher is aep-publisher-<org>, the coding and validation
	// runners' client (secret ae-publisher-client).
	ClientPublisher ClientKind = "publisher"
	// ClientStudio is ae-studio-<org>, the AE Studio pod's own client
	// (secret ae-studio-client).
	ClientStudio ClientKind = "studio"
)

// ClientEnsureActor is the audit actor of an EnsureClient run.
const ClientEnsureActor = "ae-studio-ensure"

// errOrgOUUnknown means the org has no IdP org id recorded yet, so its AE
// Studio pod could not be told which org's tokens to accept.
var errOrgOUUnknown = errors.New("org Thunder OU unknown (the org row has no thunder_org_uuid)")

// errOrgOUMismatch means the request's ouId and the org row's IdP org id
// disagree, so the vault path and the org's record would name different orgs.
var errOrgOUMismatch = errors.New("the request's ouId does not match the org's Thunder OU")

// vaultOUOf is the OU every vault path of a request is derived from: the
// request's ouId claim, because the Secret Manager API derives the vault
// namespace from the JWT it authenticated (resolveVaultKey), in its
// canonical UUID form (orgUUIDForSecretLocation). The org's own IdP org id
// comes from the org row (lookupOrgOUID); EnsureClient refuses when the two
// disagree.
func vaultOUOf(ctx context.Context) (string, error) {
	return orgUUIDForSecretLocation(ctx)
}

// sameOU reports whether two OU ids name one OU. UUIDs compare by value, so
// a claim in another case or form than the org row's canonical one matches;
// anything that is not a UUID compares as a string.
func sameOU(a, b string) bool {
	ua, errA := uuid.Parse(a)
	ub, errB := uuid.Parse(b)
	if errA == nil && errB == nil {
		return ua == ub
	}
	return a == b
}

// clientSecretOf is the org secret holding kind's credentials.
func clientSecretOf(kind ClientKind) (OrgSecret, error) {
	switch kind {
	case ClientPublisher:
		return OrgSecretPublisherClient, nil
	case ClientStudio:
		return OrgSecretStudioClient, nil
	default:
		return "", fmt.Errorf("ensure client: unknown kind %q", kind)
	}
}

// EnsureClient is the only writer of the org clients' secrets and ids; the
// gitpat submit runs it, with the user's ouId claim on ctx (the vault path).
// It holds the client secret's lock for the write.
func (s *idpService) EnsureClient(ctx context.Context, orgID string, kind ClientKind) error {
	if orgID == "" {
		return fmt.Errorf("orgID required")
	}
	secret, err := clientSecretOf(kind)
	if err != nil {
		return err
	}
	if s.clients == nil {
		return fmt.Errorf("ensure %s client: the org's clients are not configured", kind)
	}
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return fmt.Errorf("ensure %s client: secrets delivery is off (no SecretsProvider)", kind)
	}
	vaultOU, err := vaultOUOf(ctx)
	if err != nil {
		return fmt.Errorf("ensure %s client: %w", kind, err)
	}
	orgOU := s.lookupOrgOUID(ctx, orgID)
	if orgOU != "" && !sameOU(orgOU, vaultOU) {
		return fmt.Errorf("ensure %s client: %w", kind, errOrgOUMismatch)
	}
	if orgOU == "" && kind == ClientStudio {
		return fmt.Errorf("ensure %s client: %w", kind, errOrgOUUnknown)
	}
	return s.secretRefWriter.withOrgClientLock(ctx, orgID, secret, func(l *OrgSecretLocked) error {
		var err error
		if kind == ClientPublisher {
			err = s.storePublisherClient(ctx, l, orgID, vaultOU)
		} else {
			err = s.storeStudioClient(ctx, l, orgID, vaultOU)
		}
		if err != nil {
			return fmt.Errorf("ensure %s client: %w", kind, err)
		}
		slog.InfoContext(ctx, "ae_studio.client_ensured", "org", orgID, "kind", string(kind))
		return nil
	})
}

// storePublisherClient writes aep-publisher-<org>'s secret as the org's
// ae-publisher-client reference and records its client id on the profile. The
// secret lives only in the reference: no column holds it.
func (s *idpService) storePublisherClient(ctx context.Context, l *OrgSecretLocked, orgID, vaultOU string) error {
	client, err := s.recordPublisherClient(ctx, orgID, ClientEnsureActor)
	if err != nil {
		return err
	}
	_, err = s.secretRefWriter.writeOrgClient(ctx, l, orgID, vaultOU, ClientPublisher, client.ClientID, client.Secret, map[string]any{
		"publisher_client_id": client.ClientID,
		"updated_at":          time.Now().UTC(),
	})
	return err
}

// storeStudioClient writes ae-studio-<org>'s secret as the org's
// ae-studio-client reference and records its client id on the profile.
func (s *idpService) storeStudioClient(ctx context.Context, l *OrgSecretLocked, orgID, vaultOU string) error {
	// The write stamps the profile row, so the row has to exist.
	if _, err := s.GetOrCreateProfile(ctx, orgID); err != nil {
		return err
	}
	client, err := s.clients.Studio(orgID)
	if err != nil {
		return err
	}
	_, err = s.secretRefWriter.writeOrgClient(ctx, l, orgID, vaultOU, ClientStudio, client.ClientID, client.Secret, map[string]any{
		"studio_client_id": client.ClientID,
		"updated_at":       time.Now().UTC(),
	})
	return err
}
