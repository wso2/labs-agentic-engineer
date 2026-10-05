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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/bootstrap"
	"github.com/wso2/aep/aectl/internal/ui"
)

// webhookRelaySeedPath keys every org's AE Studio relay channel (aep-api's
// AE_STUDIO_WEBHOOK_RELAY_SEED). Seeded with the install's secrets but not in
// requiredOpenBaoPaths: an install that predates the relay is topped up by
// `platform update` (seedWebhookRelaySeedIfMissing), it is not a wiped store.
const webhookRelaySeedPath = "aep/webhook-relay-seed"

// webhookRelaySeed is 32 random bytes as hex text: `platform secret import`
// trims values, so the seed is stored and used (as the HMAC key) as text.
func webhookRelaySeed() (string, error) { return bootstrap.GenerateHex(32) }

// legacyRelayNotice makes the legacy-key notice print once per run.
var legacyRelayNotice = &sync.Once{}

// webhookRelayEnabled is the per-org webhook relay switch. An install made
// before the relay persisted webhook.local_smee.enabled (the install-wide smee
// client the relay replaces); while ae_studio.webhook_relay.enabled is absent
// that legacy key stands in, so an upgrade keeps webhook delivery.
func webhookRelayEnabled() bool {
	const key, legacyKey = "ae_studio.webhook_relay.enabled", "webhook.local_smee.enabled"
	if viper.IsSet(key) || !viper.IsSet(legacyKey) {
		return viper.GetBool(key)
	}
	enabled := viper.GetBool(legacyKey)
	legacyRelayNotice.Do(func() {
		ui.Warn(fmt.Sprintf("%s is not set; using the legacy %s=%t. Set %s in the aectl config to silence this.",
			key, legacyKey, enabled, key))
	})
	return enabled
}

// seedWebhookRelaySeedIfMissing writes a fresh relay seed when the store has
// none and leaves an existing one untouched: put must refuse to overwrite
// (errSecretExists), so a seed created between the check and the write
// survives. It reports whether it wrote one; the value is never returned or
// printed (a new seed only changes channel URLs nobody has registered yet).
func seedWebhookRelaySeedIfMissing(exists func(path string) (bool, error), put func(path, value string) error) (bool, error) {
	ok, err := exists(webhookRelaySeedPath)
	if err != nil {
		return false, fmt.Errorf("check secret %s: %w", webhookRelaySeedPath, err)
	}
	if ok {
		return false, nil
	}
	seed, err := webhookRelaySeed()
	if err != nil {
		return false, fmt.Errorf("generate webhook relay seed: %w", err)
	}
	if err := put(webhookRelaySeedPath, seed); err != nil {
		if errors.Is(err, errSecretExists) {
			return false, nil
		}
		return false, fmt.Errorf("write %s: %w", webhookRelaySeedPath, err)
	}
	return true, nil
}

// ensureWebhookRelaySeed tops up aep/webhook-relay-seed in OpenBao for an
// install that predates the relay (create-only), before the chart's
// ExternalSecret for it is rendered.
func ensureWebhookRelaySeed(ctx context.Context) error {
	s, err := openOpenBaoSession(ctx)
	if err != nil {
		return fmt.Errorf("connect to OpenBao: %w", err)
	}
	defer s.stop()
	created, err := seedWebhookRelaySeedIfMissing(
		func(path string) (bool, error) { return s.pathExists(ctx, path) },
		func(path, value string) error { return s.putCreateOnly(ctx, path, value) })
	if err != nil {
		return err
	}
	if created {
		ui.Success("Seeded " + webhookRelaySeedPath)
	}
	return nil
}
