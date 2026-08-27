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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// vaultPathPrefix is the KV mount prefix SM-API writes user-app
// secrets under (matches SM-API's VAULT_PATH_PREFIX env, default
// "user-app-secrets" — see wso2cloud/backend/secret-manager-api/
// internal/vault/eso.go::VaultPath). Hardcoded here because the BFF
// must reconstruct the actual Vault path it stamps into the credential
// row's secret_ref_kv_path column (read by the dispatcher's ExternalSecret).
// If SM-API's mount changes, both sides must change together.
const vaultPathPrefix = "user-app-secrets"

// SecretRefWriter is the small helper Connect flows call after the per-org
// credential row is upserted. It uploads the secret value through the
// injected secrets provider and stamps the resulting
// `{secretRefName, kvPath, property}` onto the row so dispatch can mint
// per-run ExternalSecrets without a label-lookup.
//
// Failures are logged but do not break the Connect transaction — the
// `org_secrets`-backed path keeps working. The "secret-ref row was upserted
// but the triplet is missing" state surfaces in the next Connect attempt
// (overwrites the row cleanly).
// The triplet columns live on three tables — org_credentials (GitHub PAT),
// org_anthropic_credentials (Anthropic key), and organization_idp_profiles
// (Thunder publisher). Each is reached through its owning repository so the
// writer holds no ORM/DB handle of its own.
type SecretRefWriter struct {
	client        secretmanagersvc.SecretManagementClient
	orgCredRepo   OrgCredentialRepository
	anthropicRepo OrgAnthropicRepository
	idpRepo       IDPRepository
}

// NewSecretRefWriter returns a no-op writer when client is nil (matches the
// composition-root behavior when SecretsProvider is nil).
func NewSecretRefWriter(
	client secretmanagersvc.SecretManagementClient,
	orgCredRepo OrgCredentialRepository,
	anthropicRepo OrgAnthropicRepository,
	idpRepo IDPRepository,
) *SecretRefWriter {
	return &SecretRefWriter{
		client:        client,
		orgCredRepo:   orgCredRepo,
		anthropicRepo: anthropicRepo,
		idpRepo:       idpRepo,
	}
}

// Enabled reports whether the writer is wired to a real secrets client.
// Callers should branch on this to avoid no-op DB updates when the
// provider isn't configured.
func (w *SecretRefWriter) Enabled() bool {
	return w != nil && w.client != nil
}

// WriteAnthropic uploads one role's per-org Anthropic API key to SM-API and
// stamps the triplet onto that role's `org_anthropic_credentials` row. ctx must
// carry the inbound user JWT — Connect and POST /build run on that ctx (the
// SM-API provider reads it via the jwtassertion middleware context helper).
//
// The role picks the SM-API EntityName, so the default and coding keys occupy
// separate vault paths and a rotation of one can never clobber the other.
//
// Returns the secretRefName for caller convenience; the DB has already
// been updated when the call returns nil.
func (w *SecretRefWriter) WriteAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole, apiKey string) (string, error) {
	if !w.Enabled() {
		return "", nil
	}
	if strings.TrimSpace(ocOrgID) == "" {
		return "", errors.New("secret-ref writer: ocOrgID required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return "", errors.New("secret-ref writer: apiKey required")
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: anthropic upload: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            role.SecretRefEntity(),
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	secretRefName, err := w.client.CreateSecret(ctx, loc, map[string]string{
		secretmanagersvc.SecretKeyAPIKey: apiKey,
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: anthropic upload: %w", err)
	}
	vaultKey, err := w.resolveVaultKey(ctx, secretRefName)
	if err != nil {
		return secretRefName, fmt.Errorf("secret-ref writer: resolve anthropic vault key: %w", err)
	}
	prop := secretmanagersvc.SecretKeyAPIKey
	if err := w.anthropicRepo.UpdateColumns(ctx, ocOrgID, role, stampSecretRefTriplet(secretRefName, vaultKey, prop)); err != nil {
		return secretRefName, fmt.Errorf("secret-ref writer: stamp anthropic triplet: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: anthropic key uploaded",
		"ocOrgId", ocOrgID,
		"role", role,
		"secretRefName", secretRefName,
		"vaultKey", vaultKey)
	return secretRefName, nil
}

// WriteGitHubPAT uploads a per-org GitHub PAT to SM-API and stamps the
// triplet (plus written_at) onto `org_credentials`. Same semantics as
// WriteAnthropic: errors are returned, ctx must carry the user JWT.
func (w *SecretRefWriter) WriteGitHubPAT(ctx context.Context, ocOrgID string, pat string) (string, error) {
	if !w.Enabled() {
		return "", nil
	}
	if strings.TrimSpace(ocOrgID) == "" {
		return "", errors.New("secret-ref writer: ocOrgID required")
	}
	if strings.TrimSpace(pat) == "" {
		return "", errors.New("secret-ref writer: pat required")
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: github-pat upload: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            "github-pat",
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	secretRefName, err := w.client.CreateSecret(ctx, loc, map[string]string{
		secretmanagersvc.SecretKeyAPIKey: pat,
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: github-pat upload: %w", err)
	}
	vaultKey, err := w.resolveVaultKey(ctx, secretRefName)
	if err != nil {
		return secretRefName, fmt.Errorf("secret-ref writer: resolve github-pat vault key: %w", err)
	}
	prop := secretmanagersvc.SecretKeyAPIKey
	now := time.Now().UTC()
	if err := w.orgCredRepo.UpdateColumns(ctx, ocOrgID, stampSecretRefTripletWithWrittenAt(secretRefName, vaultKey, prop, now)); err != nil {
		return secretRefName, fmt.Errorf("secret-ref writer: stamp github-pat triplet: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: github-pat uploaded",
		"ocOrgId", ocOrgID,
		"secretRefName", secretRefName,
		"vaultKey", vaultKey)
	return secretRefName, nil
}

// WriteExternalResourceSecret uploads the secret fields of an external
// resource's per-(project, env) value bundle to SM-API and returns the Vault
// KV path the rendered ExternalSecret reads (the secretStorePath) plus the
// secretRefName. Unlike WriteAnthropic/WriteGitHubPAT there is NO DB triplet
// to stamp — the vault path is carried on the per-env OC
// ResourceReleaseBinding instead (pinned by the external-resource
// provisioner). Same semantics otherwise: errors are returned, ctx must carry
// the user JWT (resolveVaultKey reads the ouId claim).
func (w *SecretRefWriter) WriteExternalResourceSecret(ctx context.Context, ocOrgID, projectName, entityName string, data map[string]string) (vaultKey, secretRefName string, err error) {
	if !w.Enabled() {
		return "", "", nil
	}
	if strings.TrimSpace(ocOrgID) == "" || strings.TrimSpace(projectName) == "" || strings.TrimSpace(entityName) == "" {
		return "", "", errors.New("secret-ref writer: ocOrgID, projectName, entityName required")
	}
	if len(data) == 0 {
		return "", "", errors.New("secret-ref writer: no external-resource secret data to write")
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", "", fmt.Errorf("secret-ref writer: external-resource secret upload (%s): %w", entityName, err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		ProjectName:           projectName,
		EntityName:            entityName,
	}
	secretRefName, err = w.client.CreateSecret(ctx, loc, data)
	if err != nil {
		return "", "", fmt.Errorf("secret-ref writer: external-resource secret upload (%s): %w", entityName, err)
	}
	vaultKey, err = w.resolveVaultKey(ctx, secretRefName)
	if err != nil {
		return "", secretRefName, fmt.Errorf("secret-ref writer: resolve external-resource vault key (%s): %w", entityName, err)
	}
	slog.InfoContext(ctx, "secret-ref writer: external-resource secret uploaded",
		"ocOrgId", ocOrgID, "project", projectName, "entity", entityName,
		"secretRefName", secretRefName, "vaultKey", vaultKey)
	return vaultKey, secretRefName, nil
}

// orgCatalogProjectName is the SM-API project sentinel for Registered External
// org-catalog secrets. It is not a real project; the vault layout is the same
// WriteExternalResourceSecret path.
const orgCatalogProjectName = "org-catalog"

// WriteOrgCatalogSecret uploads Registered External secret fields using the
// existing vault layout with projectName "org-catalog" and returns the vault
// key the ResourceType CEL reads from the binding (secretStorePath).
func (w *SecretRefWriter) WriteOrgCatalogSecret(ctx context.Context, ocOrgID, entityName string, data map[string]string) (string, error) {
	vaultKey, _, err := w.WriteExternalResourceSecret(ctx, ocOrgID, orgCatalogProjectName, entityName, data)
	return vaultKey, err
}

// OrgCatalogVaultKey reconstructs the org-catalog vault path for an already-
// written Registered External secret (entityName is `<name>-<env>`) without
// writing. Used after aep-api restart when the process-local value plane is
// empty but OpenBao still holds the org-catalog record.
func (w *SecretRefWriter) OrgCatalogVaultKey(ctx context.Context, ocOrgID, entityName string) (string, error) {
	if w == nil || !w.Enabled() {
		return "", nil
	}
	if strings.TrimSpace(ocOrgID) == "" || strings.TrimSpace(entityName) == "" {
		return "", errors.New("secret-ref writer: ocOrgID and entityName required")
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: org-catalog vault key (%s): %w", entityName, err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		ProjectName:           orgCatalogProjectName,
		EntityName:            entityName,
	}
	return w.resolveVaultKey(ctx, loc.SecretRefName())
}

// orgUUIDForSecretLocation returns the Thunder ouId that must populate
// SecretLocation.OrgName. The vault KV path hashes OrgName via
// tenant.OrgBaseNamespace; SecretReference CRs are authored into
// ControlPlaneNamespace (the OC org handle, e.g. "default") so
// ReleaseBinding collect can find them.
func orgUUIDForSecretLocation(ctx context.Context) (string, error) {
	claims := jwtassertion.GetTokenClaims(ctx)
	if claims == nil || strings.TrimSpace(claims.OuId) == "" {
		return "", errors.New("no ouId claim in JWT context")
	}
	return claims.OuId, nil
}

// resolveVaultKey reconstructs the actual Vault KV key from the
// JWT's `ouId` claim — matches the shape SM-API derives server-side
// via vault.VaultPath() and stamps onto the SecretReference CR's
// spec.data[].remoteRef.key. The dispatcher pipes this verbatim into
// the per-run ExternalSecret.
//
// Pulling orgUUID from the JWT (not the DB) is deliberate: SM-API
// derives the NS from the JWT it just authenticated, so the BFF must
// use the same source-of-truth to compute a matching path. The BFF's
// local `organizations.uuid` is a random local PK and would diverge.
// Connect and POST /build always run in a request context with a verified user JWT.
func (w *SecretRefWriter) resolveVaultKey(ctx context.Context, secretRefName string) (string, error) {
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", err
	}
	ns := tenant.OrgBaseNamespace(orgUUID)
	vaultKey := vaultPathPrefix + "/" + ns + "/" + secretRefName
	return vaultKey, nil
}

// DeleteAnthropic best-effort removes one role's SM-API secret + clears the
// triplet on that role's `org_anthropic_credentials` row. Tolerates "already
// gone" responses (the underlying client returns nil on 404).
func (w *SecretRefWriter) DeleteAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole) error {
	if !w.Enabled() {
		return nil
	}
	row, err := w.anthropicRepo.GetByOrg(ctx, ocOrgID, role)
	if err != nil {
		return fmt.Errorf("secret-ref writer: load anthropic row: %w", err)
	}
	if row == nil {
		return nil
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: delete anthropic secret: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            role.SecretRefEntity(),
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	refName := derefOrEmpty(row.SecretRefName)
	if err := w.client.DeleteSecret(ctx, loc, refName); err != nil {
		return fmt.Errorf("secret-ref writer: delete anthropic secret: %w", err)
	}
	return w.anthropicRepo.UpdateColumns(ctx, ocOrgID, role, clearSecretRefTriplet())
}

// PublisherSecretFieldClientID and PublisherSecretFieldClientSecret are the
// JSON field names inside the SM-API "publisher" secret. The dispatcher
// materialises both into the per-run Job as PUBLISHER_CLIENT_ID and
// PUBLISHER_CLIENT_SECRET via two Workload secretEnv entries on the same
// SecretReference (PUBLISHER_CLIENT_ID ← client_id, PUBLISHER_CLIENT_SECRET ←
// client_secret). Token URL is non-secret plain Job env derived from
// PLATFORM_IDP_JWKS_URL (/oauth2/jwks → /oauth2/token).
const (
	PublisherSecretFieldClientID     = "client_id"
	PublisherSecretFieldClientSecret = "client_secret"
)

// WritePublisher uploads the per-org Thunder publisher cc credentials to
// SM-API as a single 2-field secret and stamps the triplet onto
// `organization_idp_profiles`. Called from idp_service.EnsureOrgPublisher
// (on create), RegenerateClientSecret (on rotation), and
// ProvisionPublisherForBuild (POST /build). Coding dispatch reads
// secret_ref_name to mount the two Workload secretEnv entries that hand the
// runner pod its cc credentials.
//
// Same semantics as WriteAnthropic: best-effort, errors returned, ctx
// must carry the user JWT (Connect and POST /build).
func (w *SecretRefWriter) WritePublisher(ctx context.Context, ocOrgID, clientID, clientSecret string) (string, error) {
	if !w.Enabled() {
		return "", nil
	}
	if strings.TrimSpace(ocOrgID) == "" {
		return "", errors.New("secret-ref writer: ocOrgID required")
	}
	if strings.TrimSpace(clientID) == "" {
		return "", errors.New("secret-ref writer: clientID required")
	}
	if strings.TrimSpace(clientSecret) == "" {
		return "", errors.New("secret-ref writer: clientSecret required")
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: publisher upload: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            "publisher",
	}
	secretRefName, err := w.client.CreateSecret(ctx, loc, map[string]string{
		PublisherSecretFieldClientID:     clientID,
		PublisherSecretFieldClientSecret: clientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: publisher upload: %w", err)
	}
	vaultKey, err := w.resolveVaultKey(ctx, secretRefName)
	if err != nil {
		return secretRefName, fmt.Errorf("secret-ref writer: resolve publisher vault key: %w", err)
	}
	now := time.Now().UTC()
	if err := w.idpRepo.UpdateProfileColumns(ctx, &OrganizationIDPProfile{}, ocOrgID, stampSecretRefTripletWithWrittenAt(secretRefName, vaultKey, "publisher", now)); err != nil {
		return secretRefName, fmt.Errorf("secret-ref writer: stamp publisher triplet: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: publisher creds uploaded",
		"ocOrgId", ocOrgID,
		"secretRefName", secretRefName,
		"vaultKey", vaultKey)
	return secretRefName, nil
}

// DeletePublisher best-effort removes the SM-API publisher secret + clears
// the triplet on `organization_idp_profiles`. Called by
// idp_service.RevokeOrgPublisher.
func (w *SecretRefWriter) DeletePublisher(ctx context.Context, ocOrgID string) error {
	if !w.Enabled() {
		return nil
	}
	row, err := w.idpRepo.GetProfileByOrgID(ctx, ocOrgID)
	if err != nil {
		return fmt.Errorf("secret-ref writer: load idp profile row: %w", err)
	}
	if row == nil {
		return nil
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            "publisher",
	}
	refName := derefOrEmpty(row.SecretRefName)
	if err := w.client.DeleteSecret(ctx, loc, refName); err != nil {
		return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
	}
	return w.idpRepo.UpdateProfileColumns(ctx, &OrganizationIDPProfile{}, ocOrgID, clearSecretRefTripletWithWrittenAt())
}

// DeleteGitHubPAT mirrors DeleteAnthropic on the GitHub side.
func (w *SecretRefWriter) DeleteGitHubPAT(ctx context.Context, ocOrgID string) error {
	if !w.Enabled() {
		return nil
	}
	row, err := w.orgCredRepo.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return fmt.Errorf("secret-ref writer: load github row: %w", err)
	}
	if row == nil {
		return nil
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: delete github-pat secret: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            "github-pat",
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	refName := derefOrEmpty(row.SecretRefName)
	if err := w.client.DeleteSecret(ctx, loc, refName); err != nil {
		return fmt.Errorf("secret-ref writer: delete github-pat secret: %w", err)
	}
	return w.orgCredRepo.UpdateColumns(ctx, ocOrgID, clearSecretRefTripletWithWrittenAt())
}
