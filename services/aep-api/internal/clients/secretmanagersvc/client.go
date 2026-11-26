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

// Package secretmanagersvc is the secret-management client for the
// aep-service workflows plane. Two shape choices drive the rest of the
// package:
//
//  1. SecretLocation is keyed by `{org, project, task, entity, secretKey}`.
//     A coding-agent task is the smallest ownership unit; aep has
//     no agent or env-set concept at the secret layer, and per-env scoping
//     happens at the OC SecretReference level, not in the store key.
//
//  2. The provider owns the SecretReference: it stores the value and creates
//     the reference that points at it (OpenChoreo's Secret API locally,
//     Cloud's SM-API). This client never authors a SecretReference itself.
//
// OpenChoreoSecretReferenceClient is a minimal local interface over OC's
// SecretReference API, for the callers that read references or author their
// own alias of one (the ai-agent model-access reference).
package secretmanagersvc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/secretsprovider"
)

const (
	// DefaultManagedBy is the default ownership tag stamped onto every
	// secret written through this client. Cloud SM-API uses it to refuse
	// cross-tenant deletes.
	DefaultManagedBy = "aep-aep"

	// SecretKeyAPIKey is the conventional key name for single-value
	// secrets stored as `{api-key: "..."}`. Anthropic key + GitHub PAT
	// both use this shape.
	SecretKeyAPIKey = "api-key"
)

// ErrNotFound is returned by OpenChoreoSecretReferenceClient lookups when
// no SecretReference exists for (cpNS, name). Distinct from
// ErrSecretNotFound (which is about the stored value itself).
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a secret or SecretReference with the same
// (cpNS, name) already exists, signalling a race with another writer.
var ErrConflict = errors.New("conflict")

// OpenChoreoSecretReferenceClient is the small slice of the OC client that
// reads SecretReferences or authors one aliasing an existing store key. It
// covers SecretReference CRUD only; the GitSecret CRUD lives elsewhere.
// Implementations are expected to map ErrNotFound / ErrConflict on the
// namespaced errors they return. cpNS is the Workload/ReleaseBinding
// control-plane namespace.
type OpenChoreoSecretReferenceClient interface {
	GetSecretReference(ctx context.Context, cpNS, name string) (*SecretReference, error)
	CreateSecretReference(ctx context.Context, cpNS string, req CreateSecretReferenceRequest) (*SecretReference, error)
	UpdateSecretReference(ctx context.Context, cpNS, name string, req CreateSecretReferenceRequest) (*SecretReference, error)
	DeleteSecretReference(ctx context.Context, cpNS, name string) error
}

// CreateSecretReferenceRequest mirrors the field set the cluster-gateway
// proxy accepts on POST /apis/openchoreo.dev/v1alpha1/.../secretreferences.
// Kept minimal — the rest (refreshInterval, target ClusterSecretStore) is
// filled in from per-call context.
type CreateSecretReferenceRequest struct {
	Namespace       string
	Name            string
	ProjectName     string
	ComponentName   string
	KVPath          string
	SecretKeys      []string
	RefreshInterval string
}

// SecretReference is the projection of the OC SecretReference CR this
// package needs back from the server.
type SecretReference struct {
	Namespace string
	Name      string
	// Data is spec.data[]: names and keys only, never values.
	Data []SecretReferenceData
}

// SecretReferenceData is one spec.data[] entry of a SecretReference: the
// Kubernetes Secret key it fills and the store entry (RemoteKey) and
// property it reads.
type SecretReferenceData struct {
	SecretKey string
	RemoteKey string
	Property  string
}

// SecretManagementClient is the high-level secret-management interface.
//   - CreateSecret REPLACES the whole record.
//   - PatchSecret merges (server-side merge-patch).
//   - DeleteSecret is idempotent and managed-by-fenced.
//   - CreateSecretRef always makes a NEW SecretReference and returns its
//     name; DeleteSecretRef removes exactly the named one. A caller that
//     rotates a value stores the new name, repoints its readers, then
//     deletes the old reference by the name it stored.
type SecretManagementClient interface {
	CreateSecret(ctx context.Context, location SecretLocation, data map[string]string) (string, error)
	PatchSecret(ctx context.Context, location SecretLocation, data map[string]string, keysToDelete []string) (string, error)
	DeleteSecret(ctx context.Context, location SecretLocation, secretRefName string) error
	// CreateSecretRef writes data as a new SecretReference: one stored
	// property per data key, and one spec.data entry per key whose
	// property is the key itself (no aliasing). It never updates an
	// existing reference: a name clash is ErrConflict. On error nothing
	// new is left behind. location.ControlPlaneNamespace is required.
	CreateSecretRef(ctx context.Context, location SecretLocation, data map[string]string) (string, error)
	// DeleteSecretRef removes the SecretReference called name and its
	// stored value. name is the one CreateSecretRef returned; it is never
	// derived from location. A reference already gone is success.
	DeleteSecretRef(ctx context.Context, location SecretLocation, name string) error
	GetSecret(ctx context.Context, kvPath string) (*SecretInfo, error)
	// GetSecretWithValue reads the values of the secret at location.
	GetSecretWithValue(ctx context.Context, location SecretLocation) (map[string]string, error)
	// StoredKey is the store key a consumer reads the secret at location by,
	// as the backend chose it. ErrNotSupported when the provider cannot
	// report it (the caller then derives the key itself); ErrSecretNotFound
	// when no such secret exists.
	StoredKey(ctx context.Context, location SecretLocation) (string, error)
}

type secretManagementClient struct {
	lowLevelClient SecretsClient
	managedBy      string
	// mintName mints new SecretReference names (mintRefName; a seam so
	// tests can force a name clash).
	mintName func(cpNS, entity string) (string, error)
}

// SecretManagementClientConfig configures NewSecretManagementClientWithConfig.
type SecretManagementClientConfig struct {
	StoreConfig *StoreConfig
	// Provider must create the SecretReference for every value it stores
	// (SecretReferenceManager with ManagesSecretReferences() true).
	Provider Provider
}

// NewSecretManagementClientWithConfig is the full-control constructor. It
// refuses a provider that does not create SecretReferences: this client
// never authors one, so such a provider would store values nothing points at.
func NewSecretManagementClientWithConfig(cfg SecretManagementClientConfig) (SecretManagementClient, error) {
	if cfg.StoreConfig == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if m, ok := cfg.Provider.(SecretReferenceManager); !ok || !m.ManagesSecretReferences() {
		return nil, fmt.Errorf("provider must create SecretReferences (SecretReferenceManager)")
	}
	lowLevelClient, err := cfg.Provider.NewClient(cfg.StoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create secrets client: %w", err)
	}
	return &secretManagementClient{
		lowLevelClient: lowLevelClient,
		managedBy:      DefaultManagedBy,
		mintName:       mintRefName,
	}, nil
}

func (c *secretManagementClient) CreateSecret(ctx context.Context, location SecretLocation, data map[string]string) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal secret data: %w", err)
	}
	secretRef, err := c.lowLevelClient.PushSecret(ctx, location, raw, &SecretMetadata{ManagedBy: c.managedBy})
	if err != nil {
		return "", fmt.Errorf("upsert secret: %w", err)
	}
	return secretRef, nil
}

func (c *secretManagementClient) PatchSecret(ctx context.Context, location SecretLocation, data map[string]string, keysToDelete []string) (string, error) {
	patch := make(map[string]any, len(data)+len(keysToDelete))
	for k, v := range data {
		patch[k] = v
	}
	for _, k := range keysToDelete {
		patch[k] = nil
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return "", fmt.Errorf("marshal patch data: %w", err)
	}
	secretRef, err := c.lowLevelClient.PatchSecret(ctx, location, raw, &SecretMetadata{ManagedBy: c.managedBy})
	if err != nil {
		return "", fmt.Errorf("patch secret: %w", err)
	}
	return secretRef, nil
}

func (c *secretManagementClient) DeleteSecret(ctx context.Context, location SecretLocation, _ string) error {
	if err := c.lowLevelClient.DeleteSecret(ctx, location, &SecretMetadata{ManagedBy: c.managedBy}); err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	return nil
}

// refNameSuffixBytes is the random suffix length of a minted reference
// name: 4 bytes, 8 hex characters.
const refNameSuffixBytes = 4

// maxK8sNameLen is the DNS-label limit a SecretReference name must fit.
const maxK8sNameLen = 63

// mintRefName returns a fresh SecretReference name
// `<cpNS>-<entity>-<8 hex>`, a DNS label of at most 63 characters. A long
// cpNS is trimmed first so the entity stays readable; the entity is
// trimmed only when it alone does not fit, and the random suffix never is.
func mintRefName(cpNS, entity string) (string, error) {
	buf := make([]byte, refNameSuffixBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint SecretReference name: %w", err)
	}
	suffix := hex.EncodeToString(buf)
	budget := maxK8sNameLen - len(suffix) - 1 // room for "<prefix>"
	ent := secretsprovider.SanitizeForK8sName(entity)
	ns := secretsprovider.SanitizeForK8sName(cpNS)
	if len(ent) > budget {
		ent = strings.TrimRight(ent[:budget], "-")
	}
	prefix := ent
	if room := budget - len(ent) - 1; room > 0 && ns != "" {
		if len(ns) > room {
			ns = strings.TrimRight(ns[:room], "-")
		}
		if ns != "" {
			prefix = ns + "-" + ent
		}
	}
	if prefix == "" {
		return "", fmt.Errorf("mint SecretReference name: empty prefix")
	}
	return prefix + "-" + suffix, nil
}

// CreateSecretRef stores data under a freshly minted reference name. The
// provider creates the reference itself and its returned name is the
// result. A minted name already in use is retried once with a new one
// (pushUnderNewName).
func (c *secretManagementClient) CreateSecretRef(ctx context.Context, location SecretLocation, data map[string]string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("secret data is required")
	}
	if strings.TrimSpace(location.EntityName) == "" {
		return "", fmt.Errorf("SecretLocation.EntityName is required")
	}
	cpNS, err := location.CPNamespace()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal secret data: %w", err)
	}
	return c.pushUnderNewName(ctx, cpNS, location, raw, &SecretMetadata{ManagedBy: c.managedBy})
}

// refNameAttempts bounds how many minted names CreateSecretRef tries
// before reporting a name clash: the first plus one re-mint.
const refNameAttempts = 2

// pushUnderNewName writes raw under a freshly minted name. Each attempt
// mints a fresh name at the top of the loop; a provider reports a name
// already in use as ErrConflict, and only that is retried (once, under the
// next minted name). Any other error, or a second clash, is returned, so an
// existing reference is never overwritten.
func (c *secretManagementClient) pushUnderNewName(ctx context.Context, cpNS string, location SecretLocation, raw []byte, metadata *SecretMetadata) (string, error) {
	var err error
	for attempt := 0; attempt < refNameAttempts; attempt++ {
		if location.RefName, err = c.mintName(cpNS, location.EntityName); err != nil {
			return "", err
		}
		var name string
		name, err = c.lowLevelClient.PushSecret(ctx, location, raw, metadata)
		if err == nil {
			if name == "" {
				return "", fmt.Errorf("write secret: provider returned no reference name")
			}
			return name, nil
		}
		if !errors.Is(err, ErrConflict) {
			break
		}
	}
	return "", fmt.Errorf("write secret: %w", err)
}

// DeleteSecretRef removes the SecretReference called name and its stored
// value; the provider deletes both.
func (c *secretManagementClient) DeleteSecretRef(ctx context.Context, location SecretLocation, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("SecretReference name is required")
	}
	location.RefName = name
	if err := c.lowLevelClient.DeleteSecret(ctx, location, &SecretMetadata{ManagedBy: c.managedBy}); err != nil {
		return fmt.Errorf("delete secret %s: %w", name, err)
	}
	return nil
}

func (c *secretManagementClient) GetSecret(ctx context.Context, kvPath string) (*SecretInfo, error) {
	location, err := ParseKVPath(kvPath)
	if err != nil {
		return nil, fmt.Errorf("parse KV path %q: %w", kvPath, err)
	}
	info, err := c.lowLevelClient.GetSecret(ctx, location)
	if err != nil {
		return nil, fmt.Errorf("get secret info at %q: %w", kvPath, err)
	}
	return info, nil
}

func (c *secretManagementClient) GetSecretWithValue(ctx context.Context, location SecretLocation) (map[string]string, error) {
	raw, err := c.lowLevelClient.GetSecretWithValue(ctx, location)
	if err != nil {
		return nil, fmt.Errorf("get secret %s: %w", location.SecretRefName(), err)
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal secret data: %w", err)
	}
	return out, nil
}

func (c *secretManagementClient) StoredKey(ctx context.Context, location SecretLocation) (string, error) {
	resolver, ok := c.lowLevelClient.(StoredKeyResolver)
	if !ok {
		return "", ErrNotSupported
	}
	return resolver.StoredKey(ctx, location)
}
