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

	"gorm.io/gorm"
)

type dbStore struct {
	db     *gorm.DB
	cipher *ColumnCipher
}

// NewDBStore returns a CredentialStore backed by Postgres. key must be exactly
// 32 bytes (AES-256). Values are encrypted with AES-256-GCM before writing and
// decrypted on read. Generate a key with: openssl rand -base64 32
func NewDBStore(db *gorm.DB, key []byte) (CredentialStore, error) {
	c, err := NewColumnCipher(key)
	if err != nil {
		return nil, fmt.Errorf("credential store: %w", err)
	}
	return &dbStore{db: db, cipher: c}, nil
}

func (s *dbStore) Get(ctx context.Context, ocOrgID, key string) ([]byte, error) {
	if err := validateOrgID(ocOrgID); err != nil {
		return nil, err
	}
	var row struct{ Value string }
	err := s.db.WithContext(ctx).Table("org_secrets").
		Where("oc_org_id = ? AND key = ?", ocOrgID, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.cipher.Open(row.Value)
}

func (s *dbStore) Put(ctx context.Context, ocOrgID, key string, value []byte) error {
	if err := validateOrgID(ocOrgID); err != nil {
		return err
	}
	encrypted, err := s.cipher.Seal(value)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO org_secrets (oc_org_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT (oc_org_id, key) DO UPDATE
		  SET value = EXCLUDED.value, updated_at = now()`,
		ocOrgID, key, encrypted).Error
}

func (s *dbStore) Delete(ctx context.Context, ocOrgID, key string) error {
	if err := validateOrgID(ocOrgID); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Exec(
		`DELETE FROM org_secrets WHERE oc_org_id = ? AND key = ?`, ocOrgID, key).Error
}
