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
	"maps"
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
// The triplet columns live on four tables — org_credentials (GitHub PAT),
// org_model_connections (the model connection's key),
// org_anthropic_credentials (the Claude subscription), and
// organization_idp_profiles (Thunder publisher). Each is reached through its
// owning repository so the writer holds no ORM/DB handle of its own.
type SecretRefWriter struct {
	client        secretmanagersvc.SecretManagementClient
	orgCredRepo   OrgCredentialRepository
	anthropicRepo OrgAnthropicRepository
	idpRepo       IDPRepository
	modelConnRepo OrgModelConnectionRepository
	// orgSecrets writes the org secrets (the GitHub PAT, the two org
	// clients) as a new reference per write; see OrgSecretWriter.
	orgSecrets *OrgSecretWriter
}

// NewSecretRefWriter returns a no-op writer when client is nil (matches the
// composition-root behavior when SecretsProvider is nil).
func NewSecretRefWriter(
	client secretmanagersvc.SecretManagementClient,
	orgCredRepo OrgCredentialRepository,
	anthropicRepo OrgAnthropicRepository,
	idpRepo IDPRepository,
	modelConnRepo OrgModelConnectionRepository,
) *SecretRefWriter {
	return &SecretRefWriter{
		client:        client,
		orgCredRepo:   orgCredRepo,
		anthropicRepo: anthropicRepo,
		idpRepo:       idpRepo,
		modelConnRepo: modelConnRepo,
	}
}

// WithOrgSecretWriter attaches the writer the org secrets go through. It is
// built over the same secrets client; an enabled SecretRefWriter without one
// refuses the GitHub PAT and org client writes.
func (w *SecretRefWriter) WithOrgSecretWriter(o *OrgSecretWriter) *SecretRefWriter {
	w.orgSecrets = o
	return w
}

// orgSecretWriter returns the attached OrgSecretWriter, or an error naming
// the wiring gap.
func (w *SecretRefWriter) orgSecretWriter() (*OrgSecretWriter, error) {
	if w.orgSecrets == nil {
		return nil, errors.New("secret-ref writer: org secret writer not configured")
	}
	return w.orgSecrets, nil
}

// Enabled reports whether the writer is wired to a real secrets client.
// Callers should branch on this to avoid no-op DB updates when the
// provider isn't configured.
func (w *SecretRefWriter) Enabled() bool {
	return w != nil && w.client != nil
}

// WriteAnthropic uploads one role's per-org Anthropic credential (the Claude
// subscription) to SM-API and stamps the triplet onto that role's
// `org_anthropic_credentials` row. ctx must carry the inbound user JWT — the
// card's save runs on that ctx (the SM-API provider reads it via the
// jwtassertion middleware context helper).
//
// Returns the secretRefName for caller convenience; the DB has already
// been updated when the call returns nil.
func (w *SecretRefWriter) WriteAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole, apiKey string) (string, error) {
	return w.writeAPIKey(ctx, ocOrgID, role.SecretRefEntity(), apiKey, func(cols map[string]any) error {
		return w.anthropicRepo.UpdateColumns(ctx, ocOrgID, role, cols)
	})
}

// WriteModelKey uploads the org's model connection key to SM-API and stamps
// the triplet onto its `org_model_connections` row. Same contract as
// WriteAnthropic.
func (w *SecretRefWriter) WriteModelKey(ctx context.Context, ocOrgID, apiKey string) (string, error) {
	return w.writeAPIKey(ctx, ocOrgID, modelKeySecretEntity, apiKey, func(cols map[string]any) error {
		return w.modelConnRepo.UpdateColumns(ctx, ocOrgID, cols)
	})
}

// UploadModelKey uploads the org's model connection key to SM-API under the
// connection's entity and returns the reference, stamping nothing: for a
// caller that records it on the row inside its own transaction (the card's
// copies, the rename). ctx must carry an ouId claim.
func (w *SecretRefWriter) UploadModelKey(ctx context.Context, ocOrgID, apiKey string) (SecretRefTriplet, error) {
	if !w.Enabled() {
		return SecretRefTriplet{}, errors.New("secret-ref writer: not configured")
	}
	return w.uploadAPIKey(ctx, ocOrgID, modelKeySecretEntity, apiKey)
}

// UploadAnthropic is UploadModelKey for one role's Claude subscription token.
func (w *SecretRefWriter) UploadAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole, apiKey string) (SecretRefTriplet, error) {
	if !w.Enabled() {
		return SecretRefTriplet{}, errors.New("secret-ref writer: not configured")
	}
	return w.uploadAPIKey(ctx, ocOrgID, role.SecretRefEntity(), apiKey)
}

func (w *SecretRefWriter) writeAPIKey(ctx context.Context, ocOrgID, entity, apiKey string, stamp func(map[string]any) error) (string, error) {
	if !w.Enabled() {
		return "", nil
	}
	ref, err := w.uploadAPIKey(ctx, ocOrgID, entity, apiKey)
	if err != nil {
		return ref.Name, err
	}
	if err := stamp(stampSecretRefTriplet(ref.Name, ref.KVPath, ref.Property)); err != nil {
		return ref.Name, fmt.Errorf("secret-ref writer: stamp %s triplet: %w", entity, err)
	}
	slog.InfoContext(ctx, "secret-ref writer: api key uploaded",
		"ocOrgId", ocOrgID,
		"entity", entity,
		"secretRefName", ref.Name,
		"vaultKey", ref.KVPath)
	return ref.Name, nil
}

// uploadAPIKey uploads one API-key-shaped secret under entity and resolves
// where it landed. On a vault-key failure the returned triplet carries the
// name the upload produced.
func (w *SecretRefWriter) uploadAPIKey(ctx context.Context, ocOrgID, entity, apiKey string) (SecretRefTriplet, error) {
	if strings.TrimSpace(ocOrgID) == "" {
		return SecretRefTriplet{}, errors.New("secret-ref writer: ocOrgID required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return SecretRefTriplet{}, errors.New("secret-ref writer: apiKey required")
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return SecretRefTriplet{}, fmt.Errorf("secret-ref writer: %s upload: %w", entity, err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            entity,
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	secretRefName, err := w.client.CreateSecret(ctx, loc, map[string]string{
		secretmanagersvc.SecretKeyAPIKey: apiKey,
	})
	if err != nil {
		return SecretRefTriplet{}, fmt.Errorf("secret-ref writer: %s upload: %w", entity, err)
	}
	vaultKey, err := w.resolveVaultKey(ctx, secretRefName)
	if err != nil {
		return SecretRefTriplet{Name: secretRefName}, fmt.Errorf("secret-ref writer: resolve %s vault key: %w", entity, err)
	}
	return SecretRefTriplet{Name: secretRefName, KVPath: vaultKey, Property: secretmanagersvc.SecretKeyAPIKey}, nil
}

// WriteAMPModelKey stores one agent's Agent-Manager-issued model key and
// returns the SecretReference name and vault property a ReleaseBinding can
// secretKeyRef.
//
// PER (AGENT, ENVIRONMENT), not per org. Agent Manager issues one key per model
// config per environment, and the point of routing an agent's model traffic
// through the AI gateway is that each agent holds a credential that can be
// revoked on its own. Two agents sharing an entity name would share one key and
// give that up.
//
// Unlike WriteAnthropic this stamps no DB columns: the AMP key is not an org
// credential a user connected, it is a platform-issued credential whose only
// consumer is the ReleaseBinding composed moments later. Its coordinates are
// derived from (org, component, environment) by every reader, so there is
// nothing to record.
//
// The caller must supply a context carrying an ouId claim. In a request that is
// the user's JWT; the govern stage has no user, so it mints a system token and
// attaches its claims before calling this.
func (w *SecretRefWriter) WriteAMPModelKey(ctx context.Context, ocOrgID, component, environment, apiKey, proxyURL string) (string, string, error) {
	if !w.Enabled() {
		return "", "", nil
	}
	for _, required := range []struct{ name, value string }{
		{"ocOrgID", ocOrgID},
		{"component", component},
		{"environment", environment},
		{"apiKey", apiKey},
	} {
		if strings.TrimSpace(required.value) == "" {
			return "", "", fmt.Errorf("secret-ref writer: amp model key: %s required", required.name)
		}
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", "", fmt.Errorf("secret-ref writer: amp model key: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            ampModelKeyEntity(component, environment),
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	// The URL rides in the same secret as the key. They are useless apart — the
	// key authenticates against that proxy alone — and one secret means the
	// deployment composes both from one SecretReference instead of needing a
	// second source of truth for an address Agent Manager generated.
	payload := map[string]string{secretmanagersvc.SecretKeyAPIKey: apiKey}
	if strings.TrimSpace(proxyURL) != "" {
		payload[AMPModelURLKey] = proxyURL
	}
	secretRefName, err := w.client.CreateSecret(ctx, loc, payload)
	if err != nil {
		return "", "", fmt.Errorf("secret-ref writer: amp model key upload: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: AMP model key stored",
		"ocOrgId", ocOrgID,
		"component", component,
		"environment", environment,
		"secretRefName", secretRefName)
	return secretRefName, secretmanagersvc.SecretKeyAPIKey, nil
}

// WriteAMPTracingToken stores one agent's OTLP credential — what it sends as
// `x-amp-api-key` when it POSTs spans to the gateway's /otel route.
//
// ITS OWN SecretReference, not a property beside the model key, and the reason
// is composition rather than tidiness. Minting this token is allowed to fail
// without stopping a deploy: an agent with no tracing token runs correctly and
// is merely unobserved. That is only true if the deployment can avoid
// REFERENCING a token that was never written — and OpenChoreo's secretKeyRef
// has no `optional` flag, so a reference to an absent key does not degrade to
// "no traces", it stops the container from starting. A separate reference
// turns the question composition must answer into one it can: does this
// SecretReference exist?
//
// No endpoint rides along, unlike WriteAMPModelKey. The OTLP address is the
// environment's gateway plus a fixed route — derivable by anything holding the
// AI gateway binding, and not a secret — so composing it there costs one less
// stored value that could go stale.
//
// Callers treat a failure here as non-fatal — see the governor's
// reconcileTracingToken — so this returns a plain error and stamps nothing.
func (w *SecretRefWriter) WriteAMPTracingToken(ctx context.Context, ocOrgID, component, environment, token string) error {
	if !w.Enabled() {
		return nil
	}
	for _, required := range []struct{ name, value string }{
		{"ocOrgID", ocOrgID},
		{"component", component},
		{"environment", environment},
		{"token", token},
	} {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("secret-ref writer: amp tracing token: %s required", required.name)
		}
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: amp tracing token: %w", err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            ampTracingTokenEntity(component, environment),
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	if _, err := w.client.CreateSecret(ctx, loc, map[string]string{AMPTracingTokenKey: token}); err != nil {
		return fmt.Errorf("secret-ref writer: amp tracing token upload: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: AMP tracing token stored",
		"ocOrgId", ocOrgID,
		"component", component,
		"environment", environment)
	return nil
}

// AMPTracingTokenKey is the property inside an agent's tracing secret holding
// the token — what AMP_AGENT_API_KEY is composed from.
const AMPTracingTokenKey = "tracingToken"

// AMPTracingTokenSecretRefName is the SecretReference a deployment
// secretKeyRefs to reach one agent's tracing token. Derived from (component,
// environment) by both sides, exactly as AMPModelKeySecretRefName is.
func AMPTracingTokenSecretRefName(component, environment string) string {
	return secretmanagersvc.SecretLocation{
		EntityName: ampTracingTokenEntity(component, environment),
	}.SecretRefName()
}

func ampTracingTokenEntity(component, environment string) string {
	return fmt.Sprintf("amp-tracing-%s-%s", component, environment)
}

// AMPModelURLKey is the property inside an agent's AMP secret holding the proxy
// URL its key authenticates against — the value MODEL_ENDPOINT is composed from.
const AMPModelURLKey = "url"

// AMPModelKeySecretRefName is the SecretReference a deployment secretKeyRefs to
// reach one agent's AMP model key.
//
// DERIVED, not recorded. SM-API names a SecretReference deterministically from
// its location (secretsprovider.SecretLocation.SecretRefName), so the writer and
// the deployment that consumes it can each compute the name from (component,
// environment) without a row to look it up in. Both sides go through this
// function so there is exactly one spelling: a second, divergent one would not
// error, it would simply never find the secret.
func AMPModelKeySecretRefName(component, environment string) string {
	return secretmanagersvc.SecretLocation{
		EntityName: ampModelKeyEntity(component, environment),
	}.SecretRefName()
}

func ampModelKeyEntity(component, environment string) string {
	return fmt.Sprintf("amp-model-%s-%s", component, environment)
}

// githubPATProperty is the property of the github-pat reference that tools
// and coding read (the build checkout reads its password twin).
const githubPATProperty = "token"

// WriteGitHubPAT stores the org's GitHub PAT as a new github-pat reference
// (keys token and password, one value) and records it in the secret's row.
// The triplet on org_credentials is stamped with the new name while the
// secret's lock is held (the repoint), and only then is the previous
// reference deleted: the row's, else the pre-phase-1 one the triplet names.
// Errors are returned; ctx must carry the user's ouId claim (the vault path).
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
	orgSecrets, err := w.orgSecretWriter()
	if err != nil {
		return "", err
	}
	ouID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: github-pat upload: %w", err)
	}
	legacy := ""
	if row, err := w.orgCredRepo.GetByOrg(ctx, ocOrgID); err != nil {
		return "", fmt.Errorf("secret-ref writer: load github row: %w", err)
	} else if row != nil {
		legacy = derefOrEmpty(row.SecretRefName)
	}
	name, err := orgSecrets.WriteAndRetire(ctx, ocOrgID, ouID, OrgSecretGitHubPAT, map[string]string{githubPATProperty: pat}, legacy, func(name string) error {
		cols := stampSecretRefTripletWithWrittenAt(name, vaultKeyFor(ouID, name), githubPATProperty, time.Now().UTC())
		return w.orgCredRepo.UpdateColumns(ctx, ocOrgID, cols)
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: github-pat upload: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: github-pat uploaded", "ocOrgId", ocOrgID, "secretRefName", name)
	return name, nil
}

// RestoreGitHubPAT rewrites the org's GitHub PAT under the github-pat
// reference its row already names (local repair after the vault lost its
// values): no new reference, nothing repointed. It reports false when the
// secret has no row. ctx must carry an ouId claim.
func (w *SecretRefWriter) RestoreGitHubPAT(ctx context.Context, ocOrgID, pat string) (bool, error) {
	if !w.Enabled() {
		return false, nil
	}
	orgSecrets, err := w.orgSecretWriter()
	if err != nil {
		return false, err
	}
	ouID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return false, fmt.Errorf("secret-ref writer: github-pat restore: %w", err)
	}
	return orgSecrets.Restore(ctx, ocOrgID, ouID, OrgSecretGitHubPAT, map[string]string{githubPATProperty: pat})
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

// CopyOrgCatalogSecret reads the secret fields at fromVaultKey — a project's
// own external-resource secret, whose vault key its per-environment binding
// carries as secretStorePath — and writes them as an org-catalog entity. The
// values move vault to vault; nothing is echoed to the caller.
func (w *SecretRefWriter) CopyOrgCatalogSecret(ctx context.Context, ocOrgID, fromVaultKey, entityName string) (string, error) {
	if !w.Enabled() {
		return "", nil
	}
	if strings.TrimSpace(fromVaultKey) == "" {
		return "", errors.New("secret-ref writer: no vault key to copy from")
	}
	data, err := w.client.GetSecretWithValue(ctx, fromVaultKey)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: read %s for the org catalog: %w", fromVaultKey, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("secret-ref writer: %s holds no secret data", fromVaultKey)
	}
	return w.WriteOrgCatalogSecret(ctx, ocOrgID, entityName, data)
}

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
	return vaultKeyFor(orgUUID, secretRefName), nil
}

// vaultKeyFor is the vault KV key of the reference secretRefName of the org
// whose Thunder OU is ouID (see resolveVaultKey).
func vaultKeyFor(ouID, secretRefName string) string {
	return vaultPathPrefix + "/" + tenant.OrgBaseNamespace(ouID) + "/" + secretRefName
}

// DeleteAnthropic best-effort removes one role's SM-API secret. The caller
// passes the secret-ref name it captured from the row before deleting it — the
// credential row is already gone by the time this runs (the delete commits
// first), so there is no triplet left to read or clear. Tolerates "already
// gone" responses (the underlying client returns nil on 404).
func (w *SecretRefWriter) DeleteAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole, secretRefName string) error {
	return w.deleteAPIKey(ctx, ocOrgID, role.SecretRefEntity(), secretRefName)
}

// DeleteModelKey best-effort removes a model connection key's SM-API copy,
// with DeleteAnthropic's contract. The entity is read off the reference name,
// so a row the rename has not moved yet deletes its Anthropic-era copy rather
// than the new name's vault path (model_key_rename.go).
func (w *SecretRefWriter) DeleteModelKey(ctx context.Context, ocOrgID, secretRefName string) error {
	return w.deleteAPIKey(ctx, ocOrgID, modelKeyEntityOf(secretRefName), secretRefName)
}

func (w *SecretRefWriter) deleteAPIKey(ctx context.Context, ocOrgID, entity, secretRefName string) error {
	if !w.Enabled() {
		return nil
	}
	orgUUID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: delete %s secret: %w", entity, err)
	}
	loc := secretmanagersvc.SecretLocation{
		OrgName:               orgUUID,
		ControlPlaneNamespace: ocOrgID,
		EntityName:            entity,
		SecretKey:             secretmanagersvc.SecretKeyAPIKey,
	}
	if err := w.client.DeleteSecret(ctx, loc, secretRefName); err != nil {
		return fmt.Errorf("secret-ref writer: delete %s secret: %w", entity, err)
	}
	return nil
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

// publisherTripletProperty is what the publisher triplet records as its
// property: the reference carries two (client_id and client_secret) and
// dispatch mounts both by name, so it is a label, not a key.
const publisherTripletProperty = "publisher"

// withOrgClientLock runs fn holding the lock of an org client secret, so a
// caller can decide what to write from Thunder's state and write it with no
// other write of that secret in between (see OrgSecretWriter.WithLock).
// Without an org secret writer, fn gets a nil handle: every write through it
// then fails as not configured.
func (w *SecretRefWriter) withOrgClientLock(ctx context.Context, ocOrgID string, s OrgSecret, fn func(l *OrgSecretLocked) error) error {
	if !w.Enabled() || w.orgSecrets == nil {
		return fn(nil)
	}
	return w.orgSecrets.WithLock(ctx, ocOrgID, s, fn)
}

// writePublisherClient stores the org's Thunder publisher cc credentials as
// a new ae-publisher-client reference (client_id, client_secret) under the
// held lock l, vault path under ouID. Inside the repoint it first runs
// beforeStamp (nil = nothing), then writes the triplet together with cols
// (extra profile columns) in one update, before the previous reference is
// deleted (the row's, else the pre-phase-1 one the triplet names). Coding
// dispatch reads secret_ref_name to mount the two Workload secretEnv entries
// that hand the runner pod its cc credentials.
func (w *SecretRefWriter) writePublisherClient(ctx context.Context, l *OrgSecretLocked, ocOrgID, ouID, clientID, clientSecret string, beforeStamp func() error, cols map[string]any) (string, error) {
	if err := requireOrgClient(ocOrgID, clientID, clientSecret); err != nil {
		return "", err
	}
	legacy := ""
	if row, err := w.idpRepo.GetProfileByOrgID(ctx, ocOrgID); err != nil {
		return "", fmt.Errorf("secret-ref writer: load idp profile row: %w", err)
	} else if row != nil {
		legacy = derefOrEmpty(row.SecretRefName)
	}
	name, err := writeOrgClient(ctx, l, ouID, clientID, clientSecret, legacy, func(name string) error {
		if beforeStamp != nil {
			if err := beforeStamp(); err != nil {
				return err
			}
		}
		updates := stampSecretRefTripletWithWrittenAt(name, vaultKeyFor(ouID, name), publisherTripletProperty, time.Now().UTC())
		maps.Copy(updates, cols)
		return w.idpRepo.UpdateProfileColumns(ctx, &OrganizationIDPProfile{}, ocOrgID, updates)
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: publisher upload: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: publisher creds uploaded", "ocOrgId", ocOrgID, "secretRefName", name)
	return name, nil
}

// writeStudioClient stores the org's AE Studio client credentials as a new
// ae-studio-client reference under the held lock l. Inside the repoint it
// runs beforeStamp (nil = nothing), then writes cols onto the profile. The
// secret lives only in the reference: no column holds it.
func (w *SecretRefWriter) writeStudioClient(ctx context.Context, l *OrgSecretLocked, ocOrgID, ouID, clientID, clientSecret string, beforeStamp func() error, cols map[string]any) (string, error) {
	if err := requireOrgClient(ocOrgID, clientID, clientSecret); err != nil {
		return "", err
	}
	name, err := writeOrgClient(ctx, l, ouID, clientID, clientSecret, "", func(string) error {
		if beforeStamp != nil {
			if err := beforeStamp(); err != nil {
				return err
			}
		}
		return w.idpRepo.UpdateProfileColumns(ctx, &OrganizationIDPProfile{}, ocOrgID, cols)
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: studio client upload: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: studio client uploaded", "ocOrgId", ocOrgID, "secretRefName", name)
	return name, nil
}

// writeOrgClient writes one org client's credentials through the held lock.
func writeOrgClient(ctx context.Context, l *OrgSecretLocked, ouID, clientID, clientSecret, legacy string, repoint func(name string) error) (string, error) {
	if l == nil {
		return "", errors.New("secret-ref writer: org secret writer not configured")
	}
	return l.WriteAndRetire(ctx, ouID, map[string]string{
		PublisherSecretFieldClientID:     clientID,
		PublisherSecretFieldClientSecret: clientSecret,
	}, legacy, repoint)
}

// requireOrgClient rejects an org client write missing any of its parts.
func requireOrgClient(ocOrgID, clientID, clientSecret string) error {
	for _, f := range []struct{ name, value string }{
		{"ocOrgID", ocOrgID}, {"clientID", clientID}, {"clientSecret", clientSecret},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("secret-ref writer: %s required", f.name)
		}
	}
	return nil
}

// DeletePublisher best-effort removes the SM-API publisher secret + clears
// the triplet on `organization_idp_profiles`. Called by
// idp_service.RevokeOrgPublisher. Under the secret's lock, a reference the
// org secret writer recorded is removed through it (row, triplet, then the
// reference by its stored name); a pre-phase-1 one by the triplet's name.
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
	clearTriplet := func() error {
		return w.idpRepo.UpdateProfileColumns(ctx, &OrganizationIDPProfile{}, ocOrgID, clearSecretRefTripletWithWrittenAt())
	}
	return w.withOrgClientLock(ctx, ocOrgID, OrgSecretPublisherClient, func(l *OrgSecretLocked) error {
		if l != nil {
			ref, err := l.Ref(ctx)
			if err != nil {
				return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
			}
			if ref != nil {
				if err := l.Remove(ctx, orgUUID, clearTriplet); err != nil {
					return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
				}
				return nil
			}
		}
		loc := secretmanagersvc.SecretLocation{
			OrgName:               orgUUID,
			ControlPlaneNamespace: ocOrgID,
			EntityName:            "publisher",
		}
		if err := w.client.DeleteSecret(ctx, loc, derefOrEmpty(row.SecretRefName)); err != nil {
			return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
		}
		return clearTriplet()
	})
}
