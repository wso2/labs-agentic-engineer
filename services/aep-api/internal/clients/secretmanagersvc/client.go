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
//     happens at the OC SecretReference level, not in the KV path.
//
//  2. OpenChoreoSecretReferenceClient is a minimal local interface rather
//     than the full OC client — it covers only the SecretReference surface
//     the replace-in-place writes (CreateSecret / PatchSecret / DeleteSecret)
//     and the new-reference-per-write pair (CreateSecretRef /
//     DeleteSecretRef) need.
//
// Per ADR-0002 this package does NOT ship an openbao provider — local is
// served by the SM-API binary backed by local OpenBao, cloud is served by
// the cloud SM-API.
package secretmanagersvc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/secretsprovider"
)

const (
	// DefaultManagedBy is the default ownership tag stamped onto every
	// secret written through this client. Cloud SM-API uses it to refuse
	// cross-tenant deletes; OpenBao providers use it for the same reason.
	DefaultManagedBy = "aep-aep"

	// SecretKeyAPIKey is the conventional key name for single-value
	// secrets stored as `{api-key: "..."}`. Anthropic key + GitHub PAT
	// both use this shape.
	SecretKeyAPIKey = "api-key"
)

// ErrNotFound is returned by OpenChoreoSecretReferenceClient lookups when
// no SecretReference exists for (cpNS, name). Distinct from
// ErrSecretNotFound (which is about the KV value itself).
var ErrNotFound = errors.New("not found")

// ErrConflict is returned by Create when a SecretReference with the same
// (cpNS, name) already exists, signalling a race with another writer.
var ErrConflict = errors.New("conflict")

// OpenChoreoSecretReferenceClient is the small slice of the OC client
// that this package needs. It covers SecretReference CRUD only; the
// GitSecret CRUD lives elsewhere. Implementations are expected to map
// ErrNotFound / ErrConflict on the namespaced errors they return.
// cpNS is the Workload/ReleaseBinding control-plane namespace, not the
// vault OrgBaseNamespace.
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
// Kubernetes Secret key it fills and the vault entry (RemoteKey) and
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
	// new is left behind, except after ErrConflict (see the method doc).
	// location.ControlPlaneNamespace is required on both installs.
	CreateSecretRef(ctx context.Context, location SecretLocation, data map[string]string) (string, error)
	// DeleteSecretRef removes the SecretReference called name and its
	// stored value. name is the one CreateSecretRef returned; it is never
	// derived from location. A reference already gone is success.
	DeleteSecretRef(ctx context.Context, location SecretLocation, name string) error
	GetSecret(ctx context.Context, kvPath string) (*SecretInfo, error)
	GetSecretWithValue(ctx context.Context, kvPath string) (map[string]string, error)
}

type secretManagementClient struct {
	provider        Provider
	lowLevelClient  SecretsClient
	managedBy       string
	ocClient        OpenChoreoSecretReferenceClient
	refreshInterval string
	// mintName mints new SecretReference names (mintRefName; a seam so
	// tests can force a name clash).
	mintName func(cpNS, entity string) (string, error)
}

// SecretManagementClientConfig configures NewSecretManagementClientWithConfig.
// OCClient nil → the underlying Provider must implement
// SecretReferenceManager and ManagesSecretReferences()==true (Secret
// Manager API does); otherwise SecretReferences are upserted via OCClient.
type SecretManagementClientConfig struct {
	StoreConfig     *StoreConfig
	Provider        Provider
	OCClient        OpenChoreoSecretReferenceClient
	RefreshInterval string
}

// NewSecretManagementClientWithConfig is the full-control constructor.
func NewSecretManagementClientWithConfig(cfg SecretManagementClientConfig) (SecretManagementClient, error) {
	if cfg.StoreConfig == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	lowLevelClient, err := cfg.Provider.NewClient(cfg.StoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create secrets client: %w", err)
	}
	return &secretManagementClient{
		provider:        cfg.Provider,
		lowLevelClient:  lowLevelClient,
		managedBy:       DefaultManagedBy,
		ocClient:        cfg.OCClient,
		refreshInterval: cfg.RefreshInterval,
		mintName:        mintRefName,
	}, nil
}

// managesRefs reports whether the underlying provider owns SecretReference
// CRUD (e.g. SM-API). When true the high-level client must not call OC.
func (c *secretManagementClient) managesRefs() bool {
	if m, ok := c.provider.(SecretReferenceManager); ok && m.ManagesSecretReferences() {
		return true
	}
	return false
}

func (c *secretManagementClient) requireOCClient() error {
	if c.ocClient == nil {
		return fmt.Errorf("OCClient required when provider does not manage SecretReferences")
	}
	return nil
}

func (c *secretManagementClient) upsertSecretReference(ctx context.Context, location SecretLocation, kvPath string, secretKeys []string) (string, error) {
	cpNS, err := location.CPNamespace()
	if err != nil {
		return "", err
	}
	name := location.SecretRefName()
	req := CreateSecretReferenceRequest{
		Namespace:       cpNS,
		Name:            name,
		ProjectName:     location.ProjectName,
		ComponentName:   location.EntityName,
		KVPath:          kvPath,
		SecretKeys:      secretKeys,
		RefreshInterval: c.refreshInterval,
	}
	_, getErr := c.ocClient.GetSecretReference(ctx, cpNS, name)
	if getErr != nil {
		if !errors.Is(getErr, ErrNotFound) {
			return "", fmt.Errorf("check SecretReference: %w", getErr)
		}
		if _, createErr := c.ocClient.CreateSecretReference(ctx, cpNS, req); createErr != nil {
			if errors.Is(createErr, ErrConflict) {
				if _, updateErr := c.ocClient.UpdateSecretReference(ctx, cpNS, name, req); updateErr != nil {
					return "", fmt.Errorf("update after create conflict: %w", updateErr)
				}
			} else {
				return "", fmt.Errorf("create SecretReference: %w", createErr)
			}
		}
	} else {
		if _, updateErr := c.ocClient.UpdateSecretReference(ctx, cpNS, name, req); updateErr != nil {
			return "", fmt.Errorf("update SecretReference: %w", updateErr)
		}
	}
	return name, nil
}

func (c *secretManagementClient) requireOpenBaoDirectRefs(location SecretLocation) error {
	if err := c.requireOCClient(); err != nil {
		return err
	}
	_, err := location.CPNamespace()
	return err
}

func (c *secretManagementClient) CreateSecret(ctx context.Context, location SecretLocation, data map[string]string) (string, error) {
	if !c.managesRefs() {
		if err := c.requireOpenBaoDirectRefs(location); err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal secret data: %w", err)
	}
	metadata := &SecretMetadata{ManagedBy: c.managedBy}
	secretRef, err := c.lowLevelClient.PushSecret(ctx, location, raw, metadata)
	if err != nil {
		return "", fmt.Errorf("upsert secret: %w", err)
	}
	if c.managesRefs() {
		return secretRef, nil
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return c.upsertSecretReference(ctx, location, secretRef, keys)
}

func (c *secretManagementClient) PatchSecret(ctx context.Context, location SecretLocation, data map[string]string, keysToDelete []string) (string, error) {
	if !c.managesRefs() {
		if err := c.requireOpenBaoDirectRefs(location); err != nil {
			return "", err
		}
	}
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
	metadata := &SecretMetadata{ManagedBy: c.managedBy}
	secretRef, err := c.lowLevelClient.PatchSecret(ctx, location, raw, metadata)
	if err != nil {
		return "", fmt.Errorf("patch secret: %w", err)
	}
	if c.managesRefs() {
		return secretRef, nil
	}
	info, err := c.lowLevelClient.GetSecret(ctx, location)
	if err != nil {
		return "", fmt.Errorf("get secret keys after patch: %w", err)
	}
	return c.upsertSecretReference(ctx, location, secretRef, info.Keys)
}

func (c *secretManagementClient) DeleteSecret(ctx context.Context, location SecretLocation, secretRefName string) error {
	if !c.managesRefs() {
		if err := c.requireOpenBaoDirectRefs(location); err != nil {
			return err
		}
	}
	metadata := &SecretMetadata{ManagedBy: c.managedBy}
	if err := c.lowLevelClient.DeleteSecret(ctx, location, metadata); err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	if !c.managesRefs() {
		cpNS, err := location.CPNamespace()
		if err != nil {
			return err
		}
		if err := c.deleteSecretReference(ctx, cpNS, secretRefName); err != nil {
			return fmt.Errorf("delete SecretReference: %w", err)
		}
		// Pre-fix OpenBao-direct authored CRs into OrgBaseNamespace.
		// Best-effort sweep so disconnect does not leave an inert CR.
		if oldNS := tenant.OrgBaseNamespace(location.OrgName); oldNS != "" && oldNS != cpNS {
			if err := c.deleteSecretReference(ctx, oldNS, secretRefName); err != nil {
				return fmt.Errorf("delete leftover SecretReference: %w", err)
			}
		}
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

// CreateSecretRef stores data under a freshly minted reference name.
//
// Local (provider does not manage references): the SecretReference is
// created through OC first, pointing at the vault path the provider will
// use for the minted name (SecretPathResolver), and only then is the value
// written. Creating the reference reserves the name, and OC does not read
// the vault when a reference is created, so a not-yet-written value is
// harmless: nothing consumes the reference until the caller repoints to
// it. A name clash (ErrConflict, a 1-in-2^32 event) is retried once with a
// new name and otherwise returned with nothing written, so an existing
// reference's value is never overwritten. If the value write fails, the new
// reference is deleted by its name.
//
// Cloud (provider manages references): the provider creates the
// reference itself and its returned name is the result; the minted name
// is passed only as a hint.
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
	metadata := &SecretMetadata{ManagedBy: c.managedBy}

	if c.managesRefs() {
		if location.RefName, err = c.mintName(cpNS, location.EntityName); err != nil {
			return "", err
		}
		name, err := c.lowLevelClient.PushSecret(ctx, location, raw, metadata)
		if err != nil {
			return "", fmt.Errorf("write secret: %w", err)
		}
		if name == "" {
			return "", fmt.Errorf("write secret: provider returned no reference name")
		}
		return name, nil
	}

	if err := c.requireOCClient(); err != nil {
		return "", err
	}
	resolver, ok := c.lowLevelClient.(SecretPathResolver)
	if !ok {
		return "", fmt.Errorf("provider must resolve secret paths (SecretPathResolver) when it does not manage SecretReferences")
	}
	location, kvPath, err := c.reserveSecretReference(ctx, cpNS, location, resolver, data)
	if err != nil {
		return "", err
	}
	stored, err := c.lowLevelClient.PushSecret(ctx, location, raw, metadata)
	if err == nil && stored != kvPath {
		err = fmt.Errorf("provider stored the value at a different path than it resolved")
		_ = c.lowLevelClient.DeleteSecret(ctx, location, metadata)
	}
	if err != nil {
		if delErr := c.deleteSecretReference(ctx, cpNS, location.RefName); delErr != nil {
			return "", fmt.Errorf("write secret for SecretReference %s: %w (deleting the new reference also failed: %v)", location.RefName, err, delErr)
		}
		return "", fmt.Errorf("write secret for SecretReference %s: %w", location.RefName, err)
	}
	return location.RefName, nil
}

// refNameAttempts bounds how many minted names CreateSecretRef tries
// before reporting a name clash: the first plus one re-mint.
const refNameAttempts = 2

// reserveSecretReference mints a name and creates its SecretReference,
// pointing at the vault path the value will be written to. It returns the
// location carrying the reserved RefName and that path. Nothing is written
// to the vault here.
func (c *secretManagementClient) reserveSecretReference(ctx context.Context, cpNS string, location SecretLocation, resolver SecretPathResolver, data map[string]string) (SecretLocation, string, error) {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var err error
	for attempt := 0; attempt < refNameAttempts; attempt++ {
		if location.RefName, err = c.mintName(cpNS, location.EntityName); err != nil {
			return location, "", err
		}
		kvPath, pathErr := resolver.SecretPath(location)
		if pathErr != nil {
			return location, "", fmt.Errorf("resolve secret path: %w", pathErr)
		}
		_, err = c.ocClient.CreateSecretReference(ctx, cpNS, CreateSecretReferenceRequest{
			Namespace:       cpNS,
			Name:            location.RefName,
			ProjectName:     location.ProjectName,
			ComponentName:   location.EntityName,
			KVPath:          kvPath,
			SecretKeys:      keys,
			RefreshInterval: c.refreshInterval,
		})
		if err == nil {
			return location, kvPath, nil
		}
		if !errors.Is(err, ErrConflict) {
			break
		}
	}
	return location, "", fmt.Errorf("create SecretReference %s: %w", location.RefName, err)
}

// DeleteSecretRef removes the stored value and the SecretReference called
// name. The value goes first: a retry after a partial failure still finds
// the reference by its name and finishes the job. The vault path is
// derived from location.OrgName and name, so location must carry the same
// OrgName the reference was created with.
func (c *secretManagementClient) DeleteSecretRef(ctx context.Context, location SecretLocation, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("SecretReference name is required")
	}
	managed := c.managesRefs()
	if !managed {
		if err := c.requireOpenBaoDirectRefs(location); err != nil {
			return err
		}
	}
	location.RefName = name
	if err := c.lowLevelClient.DeleteSecret(ctx, location, &SecretMetadata{ManagedBy: c.managedBy}); err != nil {
		return fmt.Errorf("delete secret %s: %w", name, err)
	}
	if managed {
		return nil
	}
	cpNS, err := location.CPNamespace()
	if err != nil {
		return err
	}
	if err := c.deleteSecretReference(ctx, cpNS, name); err != nil {
		return fmt.Errorf("delete SecretReference %s: %w", name, err)
	}
	return nil
}

func (c *secretManagementClient) deleteSecretReference(ctx context.Context, ns, name string) error {
	if err := c.ocClient.DeleteSecretReference(ctx, ns, name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
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

func (c *secretManagementClient) GetSecretWithValue(ctx context.Context, kvPath string) (map[string]string, error) {
	location, err := ParseKVPath(kvPath)
	if err != nil {
		return nil, fmt.Errorf("parse KV path %q: %w", kvPath, err)
	}
	raw, err := c.lowLevelClient.GetSecretWithValue(ctx, location)
	if err != nil {
		return nil, fmt.Errorf("get secret at %q: %w", kvPath, err)
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal secret data: %w", err)
	}
	return out, nil
}
