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
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// sealPublisherSecret encrypts a publisher client secret for at-rest storage.
// Empty values stay empty (clearing the column).
func sealPublisherSecret(cipher *secrets.ColumnCipher, plain string) (string, error) {
	if plain == "" || cipher == nil {
		return plain, nil
	}
	return cipher.Seal([]byte(plain))
}

// openPublisherSecret decrypts a stored publisher secret; plaintext is
// accepted during the migration window (OpenTolerant).
func openPublisherSecret(cipher *secrets.ColumnCipher, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	pt, err := cipher.OpenTolerant(stored)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// sealWebhookSecrets encrypts each entry's Secret field. Nil/empty lists
// pass through unchanged (App-mode rows store NULL).
func sealWebhookSecrets(cipher *secrets.ColumnCipher, list WebhookSecrets) (WebhookSecrets, error) {
	if cipher == nil || len(list) == 0 {
		return list, nil
	}
	out := make(WebhookSecrets, len(list))
	for i, e := range list {
		out[i] = e
		if e.Secret == "" {
			continue
		}
		sealed, err := cipher.Seal([]byte(e.Secret))
		if err != nil {
			return nil, fmt.Errorf("seal webhook secret[%d]: %w", i, err)
		}
		out[i].Secret = sealed
	}
	return out, nil
}

// openWebhookSecrets decrypts each entry; plaintext entries are tolerated
// during the migration window.
func openWebhookSecrets(cipher *secrets.ColumnCipher, list WebhookSecrets) (WebhookSecrets, error) {
	if len(list) == 0 {
		return list, nil
	}
	out := make(WebhookSecrets, len(list))
	for i, e := range list {
		out[i] = e
		if e.Secret == "" {
			continue
		}
		pt, err := cipher.OpenTolerant(e.Secret)
		if err != nil {
			return nil, fmt.Errorf("open webhook secret[%d]: %w", i, err)
		}
		out[i].Secret = string(pt)
	}
	return out, nil
}

// sealWebhookUpdates seals webhook_secrets when present in an Updates map.
func sealWebhookUpdates(cipher *secrets.ColumnCipher, updates map[string]any) error {
	v, ok := updates["webhook_secrets"]
	if !ok || v == nil {
		return nil
	}
	switch list := v.(type) {
	case WebhookSecrets:
		sealed, err := sealWebhookSecrets(cipher, list)
		if err != nil {
			return err
		}
		updates["webhook_secrets"] = sealed
	case *WebhookSecrets:
		if list == nil {
			return nil
		}
		sealed, err := sealWebhookSecrets(cipher, *list)
		if err != nil {
			return err
		}
		updates["webhook_secrets"] = sealed
	default:
		return fmt.Errorf("seal webhook_secrets: unsupported type %T", v)
	}
	return nil
}
