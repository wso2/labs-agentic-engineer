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

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
)

// ClientKind names one of the org's two Thunder clients.
type ClientKind string

const (
	// ClientPublisher is aep-publisher-<org>, the coding runner's and the
	// pod's publisher client (secret ae-publisher-client).
	ClientPublisher ClientKind = "publisher"
	// ClientStudio is ae-studio-<org>, the AE Studio pod's own client
	// (secret ae-studio-client).
	ClientStudio ClientKind = "studio"
)

// ClientEnsureActor is the audit actor of an EnsureClient run.
const ClientEnsureActor = "ae-studio-ensure"

// clientSecretBytes is the entropy of a client secret AE generates on heal.
const clientSecretBytes = 32

// errOrgOUUnknown means the org has no Thunder OU recorded yet, so neither
// its clients nor its vault path can be placed.
var errOrgOUUnknown = errors.New("org Thunder OU unknown (the org row has no thunder_org_uuid)")

// orgClient is one ensured app and how to store a secret for it.
type orgClient struct {
	secret OrgSecret
	app    thundersvc.OrgApp
	// write stores clientSecret; beforeStamp runs inside the repoint, after
	// the vault write and before the profile is stamped (nil = nothing).
	write func(clientSecret string, beforeStamp func() error) error
}

func (s *idpService) EnsureClient(ctx context.Context, orgID string, kind ClientKind) error {
	if orgID == "" {
		return fmt.Errorf("orgID required")
	}
	if s.thunder == nil {
		return ErrIDPThunderUnavailable
	}
	if s.secretRefWriter == nil || !s.secretRefWriter.Enabled() {
		return fmt.Errorf("ensure %s client: secrets delivery is off (no SecretsProvider)", kind)
	}
	ouID := s.lookupOrgOUID(ctx, orgID)
	if ouID == "" {
		return fmt.Errorf("ensure %s client: %w", kind, errOrgOUUnknown)
	}
	var (
		client orgClient
		err    error
	)
	switch kind {
	case ClientPublisher:
		client, err = s.publisherClient(ctx, orgID, ouID)
	case ClientStudio:
		client, err = s.studioClient(ctx, orgID, ouID)
	default:
		return fmt.Errorf("ensure client: unknown kind %q", kind)
	}
	if err != nil {
		return fmt.Errorf("ensure %s client: %w", kind, err)
	}
	action, err := s.storeClientSecret(ctx, orgID, client)
	if err != nil {
		return fmt.Errorf("ensure %s client: %w", kind, err)
	}
	slog.InfoContext(ctx, "ae_studio.client_ensured", "org", orgID, "kind", string(kind), "action", action)
	return nil
}

// storeClientSecret applies the table above to an ensured app and reports
// what it did: "created", "healed" or "present".
func (s *idpService) storeClientSecret(ctx context.Context, orgID string, c orgClient) (string, error) {
	if c.app.Created {
		if c.app.Secret == "" {
			return "", errors.New("thunder created the app without returning its secret")
		}
		return "created", c.write(c.app.Secret, nil)
	}
	row, err := s.secretRefWriter.orgClientRef(ctx, orgID, c.secret)
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

// publisherClient ensures aep-publisher-<org>, recording its ids (and, on
// create, the sealed secret) on the profile. Its write also keeps the dual
// path: the sealed publisher_client_secret column and the profile triplet.
func (s *idpService) publisherClient(ctx context.Context, orgID, ouID string) (orgClient, error) {
	app, err := s.ensurePublisherApp(ctx, orgID, ClientEnsureActor, ouID)
	if err != nil {
		return orgClient{}, err
	}
	return orgClient{
		secret: OrgSecretPublisherClient,
		app:    app,
		write: func(clientSecret string, beforeStamp func() error) error {
			_, err := s.secretRefWriter.writePublisherClient(ctx, orgID, ouID, app.ClientID, clientSecret, beforeStamp, map[string]any{
				"publisher_client_id":      app.ClientID,
				"publisher_thunder_app_id": app.EntityID,
				"publisher_client_secret":  clientSecret,
				"updated_at":               time.Now().UTC(),
			})
			return err
		},
	}, nil
}

// studioClient ensures ae-studio-<org> under the org's OU (one under another
// OU is thundersvc.ErrAppInForeignOU and is never touched). Its ids are
// recorded with the write; the secret lives only in the reference.
func (s *idpService) studioClient(ctx context.Context, orgID, ouID string) (orgClient, error) {
	profile, err := s.GetOrCreateProfile(ctx, orgID)
	if err != nil {
		return orgClient{}, err
	}
	app, err := s.thunder.EnsureOrgApp(ctx, thundersvc.OrgAppSpec{
		Name:     thundersvc.StudioAppName(orgID),
		OUID:     ouID,
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
		secret: OrgSecretStudioClient,
		app:    app,
		write: func(clientSecret string, beforeStamp func() error) error {
			_, err := s.secretRefWriter.writeStudioClient(ctx, orgID, ouID, app.ClientID, clientSecret, beforeStamp, ids)
			return err
		},
	}, nil
}

// forgetPublisherRef unsets the ae-publisher-client row after a rotation
// whose vault write failed, so EnsureClient heals the reference instead of
// finding one that holds an invalidated secret. Best-effort: logged.
func (s *idpService) forgetPublisherRef(ctx context.Context, orgID string) {
	if err := s.secretRefWriter.removeOrgClient(ctx, orgID, OrgSecretPublisherClient); err != nil {
		slog.ErrorContext(ctx, "idp_service: unset publisher reference after failed SM-API rewrite",
			"orgID", orgID, "error", err)
	}
}
