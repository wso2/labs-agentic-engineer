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

package openbao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/secretsprovider"
)

// Client implements secretsprovider.SecretsClient against OpenBao via DeliveryKV.
type Client struct {
	kv *secrets.DeliveryKV
}

// vaultPath builds user-app-secrets/{OrgBaseNamespace(orgUUID)}/{SecretRefName}.
// location.OrgName is the org UUID (vault path only; CR namespace is ControlPlaneNamespace).
func vaultPath(location secretsprovider.SecretLocation) string {
	ns := tenant.OrgBaseNamespace(location.OrgName)
	name := location.SecretRefName()
	return vaultPathPrefix + "/" + ns + "/" + name
}

// PushSecret writes the JSON object value to OpenBao and returns the full
// vault path (stable with SecretRefWriter.resolveVaultKey).
func (c *Client) PushSecret(ctx context.Context, location secretsprovider.SecretLocation, value []byte, _ *secretsprovider.SecretMetadata) (string, error) {
	if location.OrgName == "" {
		return "", fmt.Errorf("openbao: OrgName is required")
	}
	if location.EntityName == "" {
		return "", fmt.Errorf("openbao: EntityName is required")
	}
	var data map[string]string
	if err := json.Unmarshal(value, &data); err != nil {
		return "", fmt.Errorf("openbao: unmarshal secret data: %w", err)
	}
	path := vaultPath(location)
	if err := c.kv.Put(ctx, path, data); err != nil {
		return "", err
	}
	return path, nil
}

// PatchSecret replaces secret data entirely (v1: no server-side merge).
// Same write path as PushSecret.
func (c *Client) PatchSecret(ctx context.Context, location secretsprovider.SecretLocation, value []byte, metadata *secretsprovider.SecretMetadata) (string, error) {
	return c.PushSecret(ctx, location, value, metadata)
}

// DeleteSecret removes the vault path. Idempotent (missing path is success).
func (c *Client) DeleteSecret(ctx context.Context, location secretsprovider.SecretLocation, _ *secretsprovider.SecretMetadata) error {
	if location.OrgName == "" {
		return fmt.Errorf("openbao: OrgName is required")
	}
	return c.kv.Delete(ctx, vaultPath(location))
}

// GetSecret is not supported — provider is WriteOnly.
func (c *Client) GetSecret(context.Context, secretsprovider.SecretLocation) (*secretsprovider.SecretInfo, error) {
	return nil, secretsprovider.ErrNotSupported
}

// GetSecretWithValue is not supported — provider is WriteOnly.
func (c *Client) GetSecretWithValue(context.Context, secretsprovider.SecretLocation) ([]byte, error) {
	return nil, secretsprovider.ErrNotSupported
}

// Close is a no-op.
func (c *Client) Close(context.Context) error { return nil }
