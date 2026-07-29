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

// Package openbao is the OpenBao-direct secrets provider.
//
// It writes user-app delivery secrets via platform/secrets.DeliveryKV
// (import fence: vault SDK stays in platform/secrets). Capabilities are
// WriteOnly; ManagesSecretReferences is false — the high-level client must
// author SecretReference CRs via OpenChoreo.
package openbao

import (
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/secretsprovider"
)

// ProviderName is the registry key for this provider.
const ProviderName = "openbao"

// vaultPathPrefix matches SM-API's VAULT_PATH_PREFIX / resolveVaultKey shape.
const vaultPathPrefix = "user-app-secrets"

// Compile-time interface assertions.
var (
	_ secretsprovider.Provider               = (*Provider)(nil)
	_ secretsprovider.SecretReferenceManager = (*Provider)(nil)
	_ secretsprovider.SecretsClient          = (*Client)(nil)
)

// Provider implements secretsprovider.Provider for direct OpenBao KV writes.
type Provider struct {
	kv *secrets.DeliveryKV
}

// NewProvider builds an OpenBao provider from StoreConfig.OpenBao fields.
// Never logs the token.
func NewProvider(cfg *secretsprovider.OpenBaoConfig) (*Provider, error) {
	if cfg == nil {
		return nil, errors.New("openbao: config is required")
	}
	if cfg.Server == "" {
		return nil, errors.New("openbao: server is required")
	}
	if cfg.Auth == nil || cfg.Auth.Token == "" {
		return nil, errors.New("openbao: auth token is required")
	}
	mount := cfg.Path
	if mount == "" {
		mount = "secret"
	}
	kv, err := secrets.NewDeliveryKV(cfg.Server, cfg.Auth.Token, mount)
	if err != nil {
		return nil, fmt.Errorf("openbao: %w", err)
	}
	return &Provider{kv: kv}, nil
}

// Capabilities is WriteOnly — values are never returned.
func (p *Provider) Capabilities() secretsprovider.StoreCapabilities {
	return secretsprovider.StoreCapabilityWriteOnly
}

// ManagesSecretReferences is false — OC adapter must author SecretReferences.
func (p *Provider) ManagesSecretReferences() bool { return false }

// NewClient returns a SecretsClient backed by DeliveryKV.
func (p *Provider) NewClient(_ *secretsprovider.StoreConfig) (secretsprovider.SecretsClient, error) {
	if p == nil || p.kv == nil {
		return nil, errors.New("openbao: provider not initialized")
	}
	return &Client{kv: p.kv}, nil
}

// ValidateConfig checks OpenBao fields on the store config (or accepts nil
// when the provider was already constructed with a validated config).
func (p *Provider) ValidateConfig(config *secretsprovider.StoreConfig) error {
	if p == nil || p.kv == nil {
		return errors.New("openbao: provider not initialized")
	}
	if config == nil {
		return nil
	}
	if config.OpenBao == nil {
		return nil
	}
	ob := config.OpenBao
	if ob.Server == "" {
		return errors.New("openbao: server is required")
	}
	if ob.Auth == nil || ob.Auth.Token == "" {
		return errors.New("openbao: auth token is required")
	}
	return nil
}
