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

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// vaultPathPrefix is the KV mount prefix SM-API writes user-app
// secrets under (matches SM-API's VAULT_PATH_PREFIX env, default
// "user-app-secrets" — see wso2cloud/backend/secret-manager-api/
// internal/vault/eso.go::VaultPath). Hardcoded here because the BFF
// must reconstruct the actual Vault path of a reference it wrote (see
// vaultKeyFor). If SM-API's mount changes, both sides must change together.
const vaultPathPrefix = "user-app-secrets"

// SecretRefWriter writes the org's secrets to the vault through the injected
// secrets provider: the org secrets (the GitHub PAT, the model keys, the two
// org clients) as a new reference per write (OrgSecretWriter), and the
// platform-issued per-agent values (WriteAMPModelKey and friends). The vault
// write is the write: a failure is returned, and no value is kept anywhere
// else. The one row it stamps besides the org secret rows (the org clients'
// ids on organization_idp_profiles) is reached through its owning
// repository, so the writer holds no ORM/DB handle of its own.
type SecretRefWriter struct {
	client  secretmanagersvc.SecretManagementClient
	idpRepo IDPRepository
	// orgSecrets writes the org secrets (the GitHub PAT, the two org
	// clients) as a new reference per write; see OrgSecretWriter.
	orgSecrets *OrgSecretWriter
	// modelKeyConsumers are what read the connection key by its vault path
	// (ModelKeyConsumers); nil: none to repoint.
	modelKeyConsumers ModelKeyConsumers
}

// ModelKeyConsumers repoints what reads the connection key by a vault path
// it was handed once, rather than through the triplet at each dispatch: the
// org's ai-agent-model-access SecretReference behind direct (ungoverned)
// ai-agent components, which ESO refreshes from that path. Each Default key
// write is a new reference, so these must move onto it before the previous
// one is retired. A consumer that does not exist (no direct agent deployed)
// is a no-op. Implemented by projects.ModelAccessRepointer.
type ModelKeyConsumers interface {
	RepointModelKey(ctx context.Context, ocOrgID string, ref SecretRefTriplet) error
}

// WithModelKeyConsumers attaches the consumers a Default key write repoints;
// chainable.
func (w *SecretRefWriter) WithModelKeyConsumers(c ModelKeyConsumers) *SecretRefWriter {
	w.modelKeyConsumers = c
	return w
}

// repointModelKeyConsumers moves the connection key's path consumers onto
// ref; nothing to do without any.
func (w *SecretRefWriter) repointModelKeyConsumers(ctx context.Context, ocOrgID string, ref SecretRefTriplet) error {
	if w.modelKeyConsumers == nil {
		return nil
	}
	return w.modelKeyConsumers.RepointModelKey(ctx, ocOrgID, ref)
}

// NewSecretRefWriter returns a no-op writer when client is nil (matches the
// composition-root behavior when SecretsProvider is nil).
func NewSecretRefWriter(client secretmanagersvc.SecretManagementClient, idpRepo IDPRepository) *SecretRefWriter {
	return &SecretRefWriter{client: client, idpRepo: idpRepo}
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
// Callers branch on it to refuse a secret save (secrets_delivery_unavailable)
// when no provider is configured, since the value has nowhere else to live.
func (w *SecretRefWriter) Enabled() bool {
	return w != nil && w.client != nil
}

// apiKeyProperty is the property the default-key and coding-agent-key
// references store their value under (orgSecretKeys).
const apiKeyProperty = secretmanagersvc.SecretKeyAPIKey

// WriteModelKey stores the org's model connection key, taken from the save
// that carries it, as a new default-key reference and records it in the
// secret's row; repoint (nil = nothing) receives the new reference's triplet
// while the secret's lock is held: the caller commits its own transaction
// there, and a repoint error undoes the write. The previous reference stays
// until the caller runs Retire on the returned write, after its transaction
// commits. ctx must carry the user's ouId claim (the vault path).
func (w *SecretRefWriter) WriteModelKey(ctx context.Context, ocOrgID, apiKey string, repoint func(SecretRefTriplet) error) (OrgSecretWrite, error) {
	return w.writeAPIKey(ctx, ocOrgID, OrgSecretDefaultKey, apiKey, repoint)
}

// WriteAnthropic is WriteModelKey for one role's Claude subscription token,
// as the role's coding-agent-key reference.
func (w *SecretRefWriter) WriteAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole, token string, repoint func(SecretRefTriplet) error) (OrgSecretWrite, error) {
	s, err := role.orgSecret()
	if err != nil {
		return OrgSecretWrite{}, err
	}
	return w.writeAPIKey(ctx, ocOrgID, s, token, repoint)
}

func (w *SecretRefWriter) writeAPIKey(ctx context.Context, ocOrgID string, s OrgSecret, apiKey string, repoint func(SecretRefTriplet) error) (OrgSecretWrite, error) {
	if !w.Enabled() {
		return OrgSecretWrite{}, errors.New("secret-ref writer: not configured")
	}
	if strings.TrimSpace(ocOrgID) == "" {
		return OrgSecretWrite{}, errors.New("secret-ref writer: ocOrgID required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return OrgSecretWrite{}, errors.New("secret-ref writer: apiKey required")
	}
	orgSecrets, err := w.orgSecretWriter()
	if err != nil {
		return OrgSecretWrite{}, err
	}
	ouID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return OrgSecretWrite{}, fmt.Errorf("secret-ref writer: %s upload: %w", s, err)
	}
	written, err := orgSecrets.Write(ctx, ocOrgID, ouID, s, map[string]string{apiKeyProperty: apiKey}, func(name string) error {
		if repoint == nil {
			return nil
		}
		return repoint(SecretRefTriplet{Name: name, KVPath: vaultKeyFor(ouID, name), Property: apiKeyProperty})
	})
	if err != nil {
		return OrgSecretWrite{}, fmt.Errorf("secret-ref writer: %s upload: %w", s, err)
	}
	return written, nil
}

// ForgetModelKey removes a deleted connection key's default-key row and then
// its reference, under the secret's lock. Unset is a no-op.
func (w *SecretRefWriter) ForgetModelKey(ctx context.Context, ocOrgID string) error {
	return w.forgetAPIKey(ctx, ocOrgID, OrgSecretDefaultKey)
}

// ForgetAnthropic is ForgetModelKey for one role's subscription token.
func (w *SecretRefWriter) ForgetAnthropic(ctx context.Context, ocOrgID string, role AnthropicRole) error {
	s, err := role.orgSecret()
	if err != nil {
		return err
	}
	return w.forgetAPIKey(ctx, ocOrgID, s)
}

func (w *SecretRefWriter) forgetAPIKey(ctx context.Context, ocOrgID string, s OrgSecret) error {
	if !w.Enabled() {
		return nil
	}
	orgSecrets, err := w.orgSecretWriter()
	if err != nil {
		return err
	}
	ouID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: forget %s: %w", s, err)
	}
	if err := orgSecrets.Remove(ctx, ocOrgID, ouID, s, nil); err != nil {
		return fmt.Errorf("secret-ref writer: forget %s: %w", s, err)
	}
	return nil
}

// orgSecret is the org secret holding role's credential.
func (r AnthropicRole) orgSecret() (OrgSecret, error) {
	if r == AnthropicRoleCoding {
		return OrgSecretCodingAgentKey, nil
	}
	return "", fmt.Errorf("secret-ref writer: no org secret for anthropic role %q", r)
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
// Unlike the org secrets it records no org_secrets row: the AMP key is not an
// org credential a user connected, it is a platform-issued credential whose only
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
// (keys token and password, one value) and records it in the secret's row,
// the only record of where the PAT lives. Then the row's previous reference
// is deleted. Errors are returned; ctx must carry the user's ouId claim (the
// vault path).
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
	name, err := orgSecrets.WriteAndRetire(ctx, ocOrgID, ouID, OrgSecretGitHubPAT, map[string]string{githubPATProperty: pat}, nil)
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: github-pat upload: %w", err)
	}
	slog.InfoContext(ctx, "secret-ref writer: github-pat uploaded", "ocOrgId", ocOrgID, "secretRefName", name)
	return name, nil
}

// RemoveGitHubSecrets removes the org's github-pat and github-webhook-secret
// (06 §9 gitpat disconnect): each one's row, then its reference by the stored
// name, under the secret's lock. An unset secret is a no-op, so a
// re-run finishes what a failed one left, and a reconnect writes both anew
// (the PAT on the submit, the webhook secret once more as a first submit).
// ctx must carry the user's ouId claim (the vault path).
func (w *SecretRefWriter) RemoveGitHubSecrets(ctx context.Context, ocOrgID string) error {
	if !w.Enabled() || w.orgSecrets == nil {
		return nil
	}
	ouID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: remove github secrets: %w", err)
	}
	if err := w.orgSecrets.Remove(ctx, ocOrgID, ouID, OrgSecretGitHubPAT, nil); err != nil {
		return fmt.Errorf("secret-ref writer: remove github-pat: %w", err)
	}
	if err := w.orgSecrets.Remove(ctx, ocOrgID, ouID, OrgSecretGitHubWebhookSecret, nil); err != nil {
		return fmt.Errorf("secret-ref writer: remove github-webhook-secret: %w", err)
	}
	return nil
}

// WriteExternalResourceSecret uploads the secret fields of an external
// resource's per-(project, env) value bundle to SM-API and returns the Vault
// KV path the rendered ExternalSecret reads (the secretStorePath) plus the
// secretRefName. Unlike WriteAnthropic/WriteGitHubPAT there is NO org_secrets
// row to record — the vault path is carried on the per-env OC
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
// ReleaseBinding collect can find them. A UUID ouId is returned in its
// canonical form: the hash is case-sensitive, so one OU must not land under
// two namespaces depending on how a token spelled it.
func orgUUIDForSecretLocation(ctx context.Context) (string, error) {
	claims := jwtassertion.GetTokenClaims(ctx)
	if claims == nil || strings.TrimSpace(claims.OuId) == "" {
		return "", errors.New("no ouId claim in JWT context")
	}
	if id, err := uuid.Parse(claims.OuId); err == nil {
		return id.String(), nil
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

// writeOrgClient stores one org client's credentials (client_id,
// client_secret) as a new reference of its secret (ae-publisher-client or
// ae-studio-client) under the held lock l, vault path under ouID. Inside the
// repoint it runs beforeStamp (nil = nothing), then writes cols (the client's
// ids) onto the profile, before the previous reference is deleted. The
// secret lives only in the reference: no column holds it. Coding dispatch
// mounts the publisher's two keys by the name its row records.
func (w *SecretRefWriter) writeOrgClient(ctx context.Context, l *OrgSecretLocked, ocOrgID, ouID string, kind ClientKind, clientID, clientSecret string, beforeStamp func() error, cols map[string]any) (string, error) {
	if err := requireOrgClient(ocOrgID, clientID, clientSecret); err != nil {
		return "", err
	}
	if l == nil {
		return "", errors.New("secret-ref writer: org secret writer not configured")
	}
	name, err := l.WriteAndRetire(ctx, ouID, map[string]string{
		PublisherSecretFieldClientID:     clientID,
		PublisherSecretFieldClientSecret: clientSecret,
	}, func(string) error {
		if beforeStamp != nil {
			if err := beforeStamp(); err != nil {
				return err
			}
		}
		return w.idpRepo.UpdateProfileColumns(ctx, &OrganizationIDPProfile{}, ocOrgID, cols)
	})
	if err != nil {
		return "", fmt.Errorf("secret-ref writer: %s client upload: %w", kind, err)
	}
	slog.InfoContext(ctx, "secret-ref writer: org client uploaded", "ocOrgId", ocOrgID, "kind", string(kind), "secretRefName", name)
	return name, nil
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

// DeletePublisher removes the org's ae-publisher-client reference: its row,
// then the reference by its stored name, under the secret's lock. Unset is a
// no-op. Called when the publisher's Thunder app is deleted
// (idp_service.forgetPublisherSecret). ctx must carry the user's ouId claim
// (the vault path).
func (w *SecretRefWriter) DeletePublisher(ctx context.Context, ocOrgID string) error {
	if !w.Enabled() || w.orgSecrets == nil {
		return nil
	}
	ouID, err := orgUUIDForSecretLocation(ctx)
	if err != nil {
		return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
	}
	if err := w.orgSecrets.Remove(ctx, ocOrgID, ouID, OrgSecretPublisherClient, nil); err != nil {
		return fmt.Errorf("secret-ref writer: delete publisher secret: %w", err)
	}
	return nil
}
