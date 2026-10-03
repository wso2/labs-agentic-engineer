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

package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

var validKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

// validConfig returns a Config that passes Validate — every required field set.
// Each test starts from it and mutates the one field it exercises.
func validConfig() Config {
	return Config{
		CredentialEncryptionKey: validKey,
		GitProvider:             "github",
		JWKSURL:                 "https://thunder.example/oauth2/jwks",
		TaskTokenSigningKey:     "-----BEGIN KEY-----\nx\n-----END KEY-----",
	}
}

func TestConfigValidate_CredentialEncryptionKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid 32-byte key", validKey, false},
		{"empty", "", true},
		{"not base64", "!!!not-base64!!!", true},
		{"16 bytes (too short)", base64.StdEncoding.EncodeToString(make([]byte, 16)), true},
		{"64 bytes (too long)", base64.StdEncoding.EncodeToString(make([]byte, 64)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.CredentialEncryptionKey = tt.key
			if err := c.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigValidate_GitProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{"github", "github", false},
		{"empty", "", true},
		{"gitlab (unsupported)", "gitlab", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.GitProvider = tt.provider
			if err := c.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigValidate_RequiredFields pins the fail-fast contract: an empty JWKSURL
// or TaskTokenSigningKey is a boot error, not a soft-warn that surfaces later.
func TestConfigValidate_RequiredFields(t *testing.T) {
	t.Run("both set is valid", func(t *testing.T) {
		if err := validConfig().Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
	t.Run("missing JWKS_URL fails", func(t *testing.T) {
		c := validConfig()
		c.JWKSURL = ""
		if err := c.Validate(); err == nil {
			t.Fatal("Validate() = nil, want error for empty JWKS_URL")
		}
	})
	t.Run("missing task signing key fails", func(t *testing.T) {
		c := validConfig()
		c.TaskTokenSigningKey = ""
		if err := c.Validate(); err == nil {
			t.Fatal("Validate() = nil, want error for empty TaskTokenSigningKey")
		}
	})
}

// setRequiredLoadEnv sets the env vars Load() needs to reach cfg.Validate()
// without a "configuration errors" failure, so a test can isolate one
// optional field's wiring.
func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PLATFORM_API_SERVICE_BASE_URL", "https://platform-api.example")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/aep")
	t.Setenv("JWKS_URL", "https://thunder.example/oauth2/jwks")
	t.Setenv("BFF_TASK_SIGNING_KEY", "-----BEGIN KEY-----\nx\n-----END KEY-----")
}

// TestLoad_SREHandoff pins the handoff's env wiring: a key is enabled, none
// is disabled, and a short key is refused.
func TestLoad_SREHandoff(t *testing.T) {
	key := strings.Repeat("a", 64)

	t.Run("a key is enabled", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_HANDOFF_TOKEN", key)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if !cfg.SREHandoff.Enabled() || cfg.SREHandoff.Token != key {
			t.Fatalf("SREHandoff = %+v, want enabled", cfg.SREHandoff)
		}
	})

	t.Run("no key is disabled", func(t *testing.T) {
		setRequiredLoadEnv(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.SREHandoff.Enabled() {
			t.Fatal("SREHandoff.Enabled() = true with nothing set")
		}
	})

	t.Run("a short key is refused", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_HANDOFF_TOKEN", "too-short")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SRE_HANDOFF_TOKEN") {
			t.Fatalf("Load() error = %v, want an SRE_HANDOFF_TOKEN error", err)
		}
	})
}
