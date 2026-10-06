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

// client_ensure.go — EnsureClient (06 §5): the org's two Thunder clients,
// aep-publisher-<org> and ae-studio-<org>, and their secrets in the org's
// vault references. Thunder hands a client secret out once, on create, so
// the reference is the only copy AE keeps:
//
//	app missing          → create; store the secret Thunder returns
//	app present, no row  → heal: new secret → vault (new reference) → Thunder
//	                       PUT → row → profile → delete the old reference
//	app present, row set → nothing
//
// On heal the Thunder PUT runs inside the write's repoint, after the vault
// write: a PUT failure rolls the new reference back, so Thunder never holds
// a secret AE failed to store, and a vault failure never reaches Thunder.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// ClientKind names one of the org's two Thunder clients.
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

// clientSecretBytes is the entropy of a client secret AE generates on heal.
const clientSecretBytes = 32

// errOrgOUUnknown means the org has no Thunder OU recorded yet, so its
// studio client cannot be placed.
var errOrgOUUnknown = errors.New("org Thunder OU unknown (the org row has no thunder_org_uuid)")

// errOrgOUMismatch means the request's ouId and the org row's Thunder OU
// disagree, so the vault path and the client's OU would name different orgs.
var errOrgOUMismatch = errors.New("the request's ouId does not match the org's Thunder OU")

// vaultOUOf is the OU every vault path of a request is derived from: the
// request's ouId claim, because the Secret Manager API derives the vault
// namespace from the JWT it authenticated (resolveVaultKey), in its
// canonical UUID form (orgUUIDForSecretLocation). The Thunder OU
// a client is registered under comes from the org row (lookupOrgOUID);
// EnsureClient refuses when the two disagree.
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

// orgClient is one ensured app and how to store a secret for it.
type orgClient struct {
	app thundersvc.OrgApp
	// write stores clientSecret; beforeStamp runs inside the repoint, after
	// the vault write and before the profile is stamped (nil = nothing).
	write func(clientSecret string, beforeStamp func() error) error
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

// EnsureClient is the only writer of the org clients and their secrets; the
// gitpat submit runs it, with the user's ouId claim on ctx (the vault path).
//
// The whole ensure (the Thunder ensure, the row check, the write and on heal
// the Thunder PUT) holds the client secret's lock. Otherwise a found-app
// heal could store and PUT a secret between another caller's create and its
// write of the created secret, leaving the reference on a secret Thunder no
// longer holds while the row looks present. Thunder calls and profile
// updates take no advisory lock, so the lock order holds.
func (s *idpService) EnsureClient(ctx context.Context, orgID string, kind ClientKind) error {
	if orgID == "" {
		return fmt.Errorf("orgID required")
	}
	secret, err := clientSecretOf(kind)
	if err != nil {
		return err
	}
	if s.thunder == nil {
		return ErrIDPThunderUnavailable
	}
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return fmt.Errorf("ensure %s client: secrets delivery is off (no SecretsProvider)", kind)
	}
	vaultOU, err := vaultOUOf(ctx)
	if err != nil {
		return fmt.Errorf("ensure %s client: %w", kind, err)
	}
	thunderOU := s.lookupOrgOUID(ctx, orgID)
	if thunderOU != "" && !sameOU(thunderOU, vaultOU) {
		return fmt.Errorf("ensure %s client: %w", kind, errOrgOUMismatch)
	}
	if thunderOU == "" && kind == ClientStudio {
		return fmt.Errorf("ensure %s client: %w", kind, errOrgOUUnknown)
	}
	return s.secretRefWriter.withOrgClientLock(ctx, orgID, secret, func(l *OrgSecretLocked) error {
		var client orgClient
		var err error
		if kind == ClientPublisher {
			client, err = s.publisherClient(ctx, l, orgID, thunderOU, vaultOU)
		} else {
			client, err = s.studioClient(ctx, l, orgID, thunderOU, vaultOU)
		}
		if err != nil {
			return fmt.Errorf("ensure %s client: %w", kind, err)
		}
		action, err := s.storeClientSecret(ctx, l, client)
		if err != nil {
			return fmt.Errorf("ensure %s client: %w", kind, err)
		}
		slog.InfoContext(ctx, "ae_studio.client_ensured", "org", orgID, "kind", string(kind), "action", action)
		return nil
	})
}

// storeClientSecret applies the table above to an ensured app, under the
// held lock l, and reports what it did: "created", "healed" or "present".
func (s *idpService) storeClientSecret(ctx context.Context, l *OrgSecretLocked, c orgClient) (string, error) {
	if c.app.Created {
		if c.app.Secret == "" {
			return "", errors.New("thunder created the app without returning its secret")
		}
		return "created", c.write(c.app.Secret, nil)
	}
	if l == nil {
		return "", errors.New("org secret writer not configured")
	}
	row, err := l.Ref(ctx)
	if err != nil {
		return "", err
	}
	if row != nil {
		return "present", nil
	}
	secret, err := generateRandomHex(clientSecretBytes)
	if err != nil {
		return "", fmt.Errorf("generate client secret: %w", err)
	}
	return "healed", c.write(secret, func() error {
		return s.thunder.SetAppSecret(ctx, c.app.EntityID, secret)
	})
}

// publisherClient ensures aep-publisher-<org> under thunderOU (the default
// OU when the org row has none, as on every publisher path), recording its
// ids on the profile. The secret lives only in the reference: no column
// holds it.
func (s *idpService) publisherClient(ctx context.Context, l *OrgSecretLocked, orgID, thunderOU, vaultOU string) (orgClient, error) {
	app, err := s.ensurePublisherApp(ctx, orgID, ClientEnsureActor, thunderOU)
	if err != nil {
		return orgClient{}, err
	}
	return orgClient{
		app: app,
		write: func(clientSecret string, beforeStamp func() error) error {
			_, err := s.secretRefWriter.writeOrgClient(ctx, l, orgID, vaultOU, ClientPublisher, app.ClientID, clientSecret, beforeStamp, map[string]any{
				"publisher_client_id":      app.ClientID,
				"publisher_thunder_app_id": app.EntityID,
				"updated_at":               time.Now().UTC(),
			})
			return err
		},
	}, nil
}

// studioClient ensures ae-studio-<org> under the org's OU (one under another
// OU is thundersvc.ErrAppInForeignOU and is never touched). Its ids are
// recorded with the write; the secret lives only in the reference.
func (s *idpService) studioClient(ctx context.Context, l *OrgSecretLocked, orgID, thunderOU, vaultOU string) (orgClient, error) {
	profile, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return orgClient{}, err
	}
	app, err := s.thunder.EnsureOrgApp(ctx, thundersvc.OrgAppSpec{
		Name:     thundersvc.StudioAppName(orgID),
		OUID:     thunderOU,
		StoredID: profile.StudioThunderAppID,
	})
	if err != nil {
		return orgClient{}, err
	}
	ids := map[string]any{
		"studio_client_id":      app.ClientID,
		"studio_thunder_app_id": app.EntityID,
		"updated_at":            time.Now().UTC(),
	}
	if !app.Created && (profile.StudioClientID != app.ClientID || profile.StudioThunderAppID != app.EntityID) {
		// The app was found by a scan (no or a stale stored id): record its
		// ids so the next lookup is one request, whatever the secret's state.
		if err := s.repo.UpdateProfileColumns(ctx, profile, orgID, ids); err != nil {
			return orgClient{}, fmt.Errorf("record studio client ids: %w", err)
		}
	}
	return orgClient{
		app: app,
		write: func(clientSecret string, beforeStamp func() error) error {
			_, err := s.secretRefWriter.writeOrgClient(ctx, l, orgID, vaultOU, ClientStudio, app.ClientID, clientSecret, beforeStamp, ids)
			return err
		},
	}, nil
}
