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

// TestLoad_SREAgentConfig pins the push-target wiring (Task 7): Enabled()
// is true only when all four SRE_AGENT_* vars are set, matching the
// TemporalConfig.Enabled() toggle pattern.
func TestLoad_SREAgentConfig(t *testing.T) {
	t.Run("all four set is enabled", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_AGENT_ORG", "default")
		t.Setenv("SRE_AGENT_NAMESPACE", "openchoreo-observability-plane")
		t.Setenv("SRE_AGENT_DEPLOYMENT", "sre-agent")
		t.Setenv("SRE_AGENT_SECRET", "sre-agent-aep")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if !cfg.SREAgent.Enabled() {
			t.Fatal("SREAgent.Enabled() = false, want true")
		}
		if cfg.SREAgent.Org != "default" {
			t.Errorf("Org = %q, want %q", cfg.SREAgent.Org, "default")
		}
		if cfg.SREAgent.Namespace != "openchoreo-observability-plane" {
			t.Errorf("Namespace = %q, want %q", cfg.SREAgent.Namespace, "openchoreo-observability-plane")
		}
		if cfg.SREAgent.Deployment != "sre-agent" {
			t.Errorf("Deployment = %q, want %q", cfg.SREAgent.Deployment, "sre-agent")
		}
		if cfg.SREAgent.Secret != "sre-agent-aep" {
			t.Errorf("Secret = %q, want %q", cfg.SREAgent.Secret, "sre-agent-aep")
		}
	})

	for _, missing := range []string{
		"SRE_AGENT_ORG", "SRE_AGENT_NAMESPACE", "SRE_AGENT_DEPLOYMENT", "SRE_AGENT_SECRET",
	} {
		t.Run("missing "+missing+" disables", func(t *testing.T) {
			setRequiredLoadEnv(t)
			vals := map[string]string{
				"SRE_AGENT_ORG":        "default",
				"SRE_AGENT_NAMESPACE":  "openchoreo-observability-plane",
				"SRE_AGENT_DEPLOYMENT": "sre-agent",
				"SRE_AGENT_SECRET":     "sre-agent-aep",
			}
			for k, v := range vals {
				if k == missing {
					continue
				}
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if cfg.SREAgent.Enabled() {
				t.Fatalf("SREAgent.Enabled() = true with %s unset, want false", missing)
			}
		})
	}
}

// TestLoad_SREAgentSeed pins the install-time seed's env wiring (Task A1):
// Present() is true only with both a key and a model, and the base URL
// defaults to the OpenAI endpoint only then — never as a bare default that
// would make an absent seed look present.
func TestLoad_SREAgentSeed(t *testing.T) {
	t.Run("key and model populate Seed with the default base URL", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_AGENT_SEED_API_KEY", "sk-seed-0123456789")
		t.Setenv("SRE_AGENT_SEED_MODEL", "gpt-4o-mini")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if !cfg.SREAgent.Seed.Present() {
			t.Fatal("Seed.Present() = false, want true")
		}
		if cfg.SREAgent.Seed.APIKey != "sk-seed-0123456789" {
			t.Errorf("APIKey = %q", cfg.SREAgent.Seed.APIKey)
		}
		if cfg.SREAgent.Seed.Model != "gpt-4o-mini" {
			t.Errorf("Model = %q", cfg.SREAgent.Seed.Model)
		}
		if cfg.SREAgent.Seed.BaseURL != "https://api.openai.com/v1" {
			t.Errorf("BaseURL = %q, want the default", cfg.SREAgent.Seed.BaseURL)
		}
	})

	t.Run("an explicit base URL overrides the default", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_AGENT_SEED_API_KEY", "sk-seed-0123456789")
		t.Setenv("SRE_AGENT_SEED_MODEL", "gpt-4o-mini")
		t.Setenv("SRE_AGENT_SEED_BASE_URL", "https://llm.internal/v1")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.SREAgent.Seed.BaseURL != "https://llm.internal/v1" {
			t.Errorf("BaseURL = %q, want the explicit value", cfg.SREAgent.Seed.BaseURL)
		}
	})

	for _, missing := range []string{"SRE_AGENT_SEED_API_KEY", "SRE_AGENT_SEED_MODEL"} {
		t.Run("missing "+missing+" is not Present", func(t *testing.T) {
			setRequiredLoadEnv(t)
			vals := map[string]string{"SRE_AGENT_SEED_API_KEY": "sk-seed-0123456789", "SRE_AGENT_SEED_MODEL": "gpt-4o-mini"}
			for k, v := range vals {
				if k == missing {
					continue
				}
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if cfg.SREAgent.Seed.Present() {
				t.Fatalf("Seed.Present() = true with %s unset, want false", missing)
			}
		})
	}

	t.Run("neither set is not Present and no default base URL leaks in", func(t *testing.T) {
		setRequiredLoadEnv(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.SREAgent.Seed.Present() {
			t.Fatal("Seed.Present() = true with neither env var set, want false")
		}
		if cfg.SREAgent.Seed.BaseURL != "" {
			t.Errorf("BaseURL = %q, want empty when the seed is not present", cfg.SREAgent.Seed.BaseURL)
		}
	})
}
