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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ColumnCipher seals and opens a column value with AES-256-GCM under
// credential-encryption-key (base64(nonce || ciphertext+tag)). Its one column
// is test_users.password_sealed: Thunder never returns a password, so the
// platform keeps the test users' generated passwords sealed. No org secret is
// in Postgres; those live only in the vault.
type ColumnCipher struct {
	gcm cipher.AEAD
}

// NewColumnCipher builds a cipher from a 32-byte AES-256 key.
func NewColumnCipher(key []byte) (*ColumnCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("column cipher: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("column cipher: cipher init: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("column cipher: gcm init: %w", err)
	}
	return &ColumnCipher{gcm: gcm}, nil
}

// Seal encrypts plaintext and returns base64(nonce || ciphertext+tag).
func (c *ColumnCipher) Seal(plaintext []byte) (string, error) {
	if c == nil {
		return string(plaintext), nil
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("encrypt: nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(c.gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

// Open decrypts a value previously produced by Seal.
func (c *ColumnCipher) Open(encoded string) ([]byte, error) {
	if c == nil {
		return []byte(encoded), nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decrypt: base64: %w", err)
	}
	ns := c.gcm.NonceSize()
	if len(data) < ns {
		return nil, errors.New("decrypt: ciphertext too short")
	}
	pt, err := c.gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: gcm: %w", err)
	}
	return pt, nil
}
