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
//     upsertSecretReference / DeleteSecret need.
//
// Per ADR-0002 this package does NOT ship an openbao provider — local is
// served by the SM-API binary backed by local OpenBao, cloud is served by
// the cloud SM-API.
package secretmanagersvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// no SecretReference exists for (orgNS, name). Distinct from
// ErrSecretNotFound (which is about the KV value itself).
var ErrNotFound = errors.New("not found")

// ErrConflict is returned by Create when a SecretReference with the same
// (orgNS, name) already exists, signalling a race with another writer.
var ErrConflict = errors.New("conflict")

// OpenChoreoSecretReferenceClient is the small slice of the OC client
// that this package needs. It covers SecretReference CRUD only; the
// GitSecret CRUD lives elsewhere. Implementations are expected to map
// ErrNotFound / ErrConflict on the namespaced errors they return.
type OpenChoreoSecretReferenceClient interface {
	GetSecretReference(ctx context.Context, orgNS, name string) (*SecretReference, error)
	CreateSecretReference(ctx context.Context, orgNS string, req CreateSecretReferenceRequest) (*SecretReference, error)
	UpdateSecretReference(ctx context.Context, orgNS, name string, req CreateSecretReferenceRequest) (*SecretReference, error)
	DeleteSecretReference(ctx context.Context, orgNS, name string) error
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
}

// SecretManagementClient is the high-level secret-management interface.
//   - CreateSecret REPLACES the whole record.
//   - PatchSecret merges (server-side merge-patch).
//   - DeleteSecret is idempotent and managed-by-fenced.
type SecretManagementClient interface {
	CreateSecret(ctx context.Context, location SecretLocation, data map[string]string) (string, error)
	PatchSecret(ctx context.Context, location SecretLocation, data map[string]string, keysToDelete []string) (string, error)
	DeleteSecret(ctx context.Context, location SecretLocation, secretRefName string) error
	GetSecret(ctx context.Context, kvPath string) (*SecretInfo, error)
	GetSecretWithValue(ctx context.Context, kvPath string) (map[string]string, error)
}

type secretManagementClient struct {
	lowLevelClient  SecretsClient
	managedBy       string
	ocClient        OpenChoreoSecretReferenceClient
	refreshInterval string
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

// NewSecretManagementClient constructs a client with the default
// managed-by tag and no OCClient — appropriate for the SM-API provider
// in both local and cloud.
func NewSecretManagementClient(cfg *StoreConfig, provider Provider) (SecretManagementClient, error) {
	return NewSecretManagementClientWithConfig(SecretManagementClientConfig{
		StoreConfig: cfg,
		Provider:    provider,
	})
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
		lowLevelClient:  lowLevelClient,
		managedBy:       DefaultManagedBy,
		ocClient:        cfg.OCClient,
		refreshInterval: cfg.RefreshInterval,
	}, nil
}

func (c *secretManagementClient) upsertSecretReference(ctx context.Context, location SecretLocation, kvPath string, secretKeys []string) (string, error) {
	name := location.SecretRefName()
	req := CreateSecretReferenceRequest{
		Namespace:       location.OrgName,
		Name:            name,
		ProjectName:     location.ProjectName,
		ComponentName:   location.EntityName,
		KVPath:          kvPath,
		SecretKeys:      secretKeys,
		RefreshInterval: c.refreshInterval,
	}
	_, getErr := c.ocClient.GetSecretReference(ctx, location.OrgName, name)
	if getErr != nil {
		if !errors.Is(getErr, ErrNotFound) {
			return "", fmt.Errorf("check SecretReference: %w", getErr)
		}
		if _, createErr := c.ocClient.CreateSecretReference(ctx, location.OrgName, req); createErr != nil {
			if errors.Is(createErr, ErrConflict) {
				if _, updateErr := c.ocClient.UpdateSecretReference(ctx, location.OrgName, name, req); updateErr != nil {
					return "", fmt.Errorf("update after create conflict: %w", updateErr)
				}
			} else {
				return "", fmt.Errorf("create SecretReference: %w", createErr)
			}
		}
	} else {
		if _, updateErr := c.ocClient.UpdateSecretReference(ctx, location.OrgName, name, req); updateErr != nil {
			return "", fmt.Errorf("update SecretReference: %w", updateErr)
		}
	}
	return name, nil
}

func (c *secretManagementClient) CreateSecret(ctx context.Context, location SecretLocation, data map[string]string) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal secret data: %w", err)
	}
	metadata := &SecretMetadata{ManagedBy: c.managedBy}
	secretRef, err := c.lowLevelClient.PushSecret(ctx, location, raw, metadata)
	if err != nil {
		return "", fmt.Errorf("upsert secret: %w", err)
	}
	if c.ocClient != nil {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		return c.upsertSecretReference(ctx, location, secretRef, keys)
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
	metadata := &SecretMetadata{ManagedBy: c.managedBy}
	secretRef, err := c.lowLevelClient.PatchSecret(ctx, location, raw, metadata)
	if err != nil {
		return "", fmt.Errorf("patch secret: %w", err)
	}
	if c.ocClient != nil {
		info, err := c.lowLevelClient.GetSecret(ctx, location)
		if err != nil {
			return "", fmt.Errorf("get secret keys after patch: %w", err)
		}
		return c.upsertSecretReference(ctx, location, secretRef, info.Keys)
	}
	return secretRef, nil
}

func (c *secretManagementClient) DeleteSecret(ctx context.Context, location SecretLocation, secretRefName string) error {
	metadata := &SecretMetadata{ManagedBy: c.managedBy}
	if err := c.lowLevelClient.DeleteSecret(ctx, location, metadata); err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	if c.ocClient != nil {
		if err := c.ocClient.DeleteSecretReference(ctx, location.OrgName, secretRefName); err != nil {
			if !errors.Is(err, ErrNotFound) {
				return fmt.Errorf("delete SecretReference: %w", err)
			}
		}
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
