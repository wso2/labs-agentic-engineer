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

package secrets

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	vault "github.com/hashicorp/vault/api"
)

// DeliveryKV is the vault/OpenBao KV-v2 helper used by the openbao secrets
// provider to push user-app delivery secrets. It accepts an already-built
// logical path (e.g. user-app-secrets/{orgNS}/{secretRefName}) and writes
// under {mount}/data/{path}.
//
// Confined to this package by the OpenBao/Vault import fence — callers outside
// platform/secrets must not import the vault SDK.
//
// Never log secret values or tokens.
type DeliveryKV struct {
	client *vault.Client
	mount  string
}

// NewDeliveryKV constructs a DeliveryKV against addr with the given token and
// KV-v2 mount (defaults to "secret").
func NewDeliveryKV(addr, token, mount string) (*DeliveryKV, error) {
	if addr == "" {
		return nil, errors.New("delivery-kv: addr is required")
	}
	if token == "" {
		return nil, errors.New("delivery-kv: token is required")
	}
	if mount == "" {
		mount = "secret"
	}

	cfg := vault.DefaultConfig()
	cfg.Address = addr
	cfg.Timeout = 10 * time.Second

	client, err := vault.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("delivery-kv: new client: %w", err)
	}
	client.SetToken(token)

	return &DeliveryKV{client: client, mount: mount}, nil
}

func (k *DeliveryKV) validateSecretPath(secretPath string) error {
	if secretPath == "" || strings.HasPrefix(secretPath, "/") {
		return fmt.Errorf("delivery-kv: path must be non-empty and not start with '/'")
	}
	return nil
}

func (k *DeliveryKV) dataPath(secretPath string) (string, error) {
	if err := k.validateSecretPath(secretPath); err != nil {
		return "", err
	}
	return path.Join(k.mount, "data", secretPath), nil
}

func (k *DeliveryKV) metadataPath(secretPath string) (string, error) {
	if err := k.validateSecretPath(secretPath); err != nil {
		return "", err
	}
	return path.Join(k.mount, "metadata", secretPath), nil
}

// Put writes data at the KV-v2 path {mount}/data/{secretPath}. Each map key
// becomes a distinct field (ESO remoteRef.property). Errors never include
// secret values or the auth token.
func (k *DeliveryKV) Put(ctx context.Context, secretPath string, data map[string]string) error {
	p, err := k.dataPath(secretPath)
	if err != nil {
		return err
	}
	payload := make(map[string]interface{}, len(data))
	for key, val := range data {
		payload[key] = val
	}
	if _, err := k.client.Logical().WriteWithContext(ctx, p, map[string]interface{}{
		"data": payload,
	}); err != nil {
		// Do not wrap vault's raw error body — it may echo request material.
		// Status-only message keeps values and tokens out of logs/errors.
		return fmt.Errorf("delivery-kv: write %s failed: %s", redactDeliveryPath(p), vaultStatus(err))
	}
	return nil
}

// Get reads the KV-v2 secret at {mount}/data/{secretPath} and returns its
// fields. A path with no secret is (nil, nil): absence is a legitimate answer a
// caller branches on, not a failure.
//
// It is the ONE read on this helper, and it exists for the environment-tier
// Thunder binding: the admin credential for an environment's identity provider
// is written to OpenBao by deployments/scripts/setup-environment-thunder.sh
// because aep-api runs outside the cluster and can read neither of the binding's
// two in-cluster copies. Errors never include the value or the token.
func (k *DeliveryKV) Get(ctx context.Context, secretPath string) (map[string]string, error) {
	p, err := k.dataPath(secretPath)
	if err != nil {
		return nil, err
	}
	secret, err := k.client.Logical().ReadWithContext(ctx, p)
	if err != nil {
		if isVaultNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("delivery-kv: read %s failed: %s", redactDeliveryPath(p), vaultStatus(err))
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	// KV-v2 nests the fields under "data"; a v1 mount would not, and reading one
	// as the other silently yields an empty map, so the shape is checked rather
	// than assumed.
	raw, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("delivery-kv: read %s: not a KV-v2 secret", redactDeliveryPath(p))
	}
	out := make(map[string]string, len(raw))
	for key, val := range raw {
		s, ok := val.(string)
		if !ok {
			// Every writer here puts strings in; anything else is a value this
			// helper has no way to hand back, and skipping it silently would
			// surface as a missing credential field with no cause.
			return nil, fmt.Errorf("delivery-kv: read %s: field %q is not a string", redactDeliveryPath(p), key)
		}
		out[key] = s
	}
	return out, nil
}

// Delete permanently removes the secret at {mount}/metadata/{secretPath}
// (KV-v2 metadata delete, matching SM-API stub behaviour). Idempotent: a
// missing path is success.
func (k *DeliveryKV) Delete(ctx context.Context, secretPath string) error {
	p, err := k.metadataPath(secretPath)
	if err != nil {
		return err
	}
	if _, err := k.client.Logical().DeleteWithContext(ctx, p); err != nil {
		if isVaultNotFound(err) {
			return nil
		}
		return fmt.Errorf("delivery-kv: delete %s failed: %s", redactDeliveryPath(p), vaultStatus(err))
	}
	return nil
}

func isVaultNotFound(err error) bool {
	var respErr *vault.ResponseError
	if errors.As(err, &respErr) && respErr.StatusCode == httpStatusNotFound {
		return true
	}
	return false
}

// httpStatusNotFound avoids importing net/http solely for the constant.
const httpStatusNotFound = 404

func vaultStatus(err error) string {
	var respErr *vault.ResponseError
	if errors.As(err, &respErr) {
		return fmt.Sprintf("status %d", respErr.StatusCode)
	}
	return "request failed"
}

// redactDeliveryPath keeps mount + first path segment for diagnostics without
// echoing the full secret-ref leaf (which is not secret material, but keeps
// error strings stable and short).
func redactDeliveryPath(p string) string {
	parts := strings.Split(p, "/")
	// secret/data/user-app-secrets/... or secret/metadata/user-app-secrets/...
	if len(parts) >= 3 {
		return strings.Join(parts[:3], "/") + "/<redacted>"
	}
	return "<redacted>"
}
